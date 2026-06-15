package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"sort"
	"strings"

	"github.com/dop251/goja"

	"mcphe/config"
	"mcphe/host"
)

// PluginAnnotations mirrors the MCP ToolAnnotations hints, settable per plugin.
type PluginAnnotations struct {
	ReadOnlyHint    *bool
	DestructiveHint *bool
	IdempotentHint  *bool
	OpenWorldHint   *bool
}

type Plugin struct {
	Name        string
	Description string
	Version     string
	Commit      string
	Tags        []string
	InputSchema interface{}
	Call        goja.Callable
	VM          *goja.Runtime
	Dir         string
	File        string
	Meta        interface{}
	Annotations *PluginAnnotations
}

type PluginManager struct {
	plugins []*Plugin
	byName  map[string]*Plugin
}

func LoadPlugins(cfg config.Config) (*PluginManager, error) {
	cfg.Logf(2, "Scanning plugin directory %s", cfg.PluginDir)
	dir := cfg.PluginDir
	if dir == "" {
		dir = "plugins"
	}
	if _, err := os.Stat(dir); err != nil {
		cfg.Logf(1, "Plugin directory %q missing: %v", dir, err)
		return nil, fmt.Errorf("plugin directory %q missing: %w", dir, err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.js"))
	if err != nil {
		cfg.Logf(1, "Failed to scan plugin directory: %v", err)
		return nil, fmt.Errorf("failed to scan plugin directory: %w", err)
	}
	if len(files) == 0 {
		cfg.Logf(1, "No JavaScript plugin files found in %q", dir)
		return nil, fmt.Errorf("no JavaScript plugins found in %q", dir)
	}

	plugins := make([]*Plugin, 0, len(files))
	byName := make(map[string]*Plugin, len(files))

	for _, path := range files {
		cfg.Logf(2, "Loading plugin %s", path)
		plugin, err := loadPlugin(path, cfg)
		if err != nil {
			return nil, fmt.Errorf("loading plugin %q: %w", path, err)
		}
		if !cfg.IsToolEnabled(plugin.Name) {
			cfg.Logf(2, "Plugin %s disabled by config", plugin.Name)
			continue
		}
		plugins = append(plugins, plugin)
		byName[plugin.Name] = plugin
	}

	sort.SliceStable(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })
	return &PluginManager{plugins: plugins, byName: byName}, nil
}

// valueToString safely converts a goja value to a string, handling undefined/nil cases
func valueToString(val goja.Value) string {
	if val == nil {
		return ""
	}
	exported := val.Export()
	if exported == nil {
		return ""
	}
	if str, ok := exported.(string); ok {
		return str
	}
	return val.String()

}

// safeExport safely exports a goja value, returning nil for undefined/null values.
// Any panic from Export() is recovered and logged rather than silently swallowed.
func safeExport(val goja.Value) (result interface{}) {
	if val == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			// Log the panic so it is visible during debugging.
			fmt.Fprintf(os.Stderr, "safeExport: recovered panic: %v\n", r)
			result = nil
		}
	}()
	return val.Export()
}

// parseBoolPtr extracts a named bool field from a JS object, returning a *bool
// or nil if the field is absent or not a bool.
func parseBoolPtr(obj *goja.Object, key string) *bool {
	val := obj.Get(key)
	if val == nil {
		return nil
	}
	exported := val.Export()
	if exported == nil {
		return nil
	}
	if b, ok := exported.(bool); ok {
		return &b
	}
	return nil
}

// parseAnnotations reads the optional `annotations` property from a plugin export.
func parseAnnotations(obj *goja.Object) *PluginAnnotations {
	annVal := obj.Get("annotations")
	if annVal == nil {
		return nil
	}
	annObj, ok := annVal.(*goja.Object)
	if !ok {
		return nil
	}
	ann := &PluginAnnotations{
		ReadOnlyHint:    parseBoolPtr(annObj, "readOnlyHint"),
		DestructiveHint: parseBoolPtr(annObj, "destructiveHint"),
		IdempotentHint:  parseBoolPtr(annObj, "idempotentHint"),
		OpenWorldHint:   parseBoolPtr(annObj, "openWorldHint"),
	}
	// Return nil if no hints were actually set
	if ann.ReadOnlyHint == nil && ann.DestructiveHint == nil && ann.IdempotentHint == nil && ann.OpenWorldHint == nil {
		return nil
	}
	return ann
}
func makeModuleLoader(vm *goja.Runtime, pluginDir string, cfg config.Config) func(modulePath string, visited map[string]bool) goja.Value {
	// internal recursive loader with cycle detection. Uses the provided VM so
	// values can be returned into the calling runtime (avoids illegal runtime
	// transitions when returning objects created in another Runtime).
	var load func(modulePath string, visited map[string]bool) goja.Value
	load = func(modulePath string, visited map[string]bool) goja.Value {
		// (no-op)
		// If the request appears to attempt path traversal and the requested
		// basename exists inside the plugin directory, treat this as an
		// explicit traversal attempt and reject it. Otherwise, reject
		// non-.js extensions with a clearer error.
		if strings.Contains(modulePath, "..") {
			base := filepath.Base(modulePath)
			if _, statErr := os.Stat(filepath.Join(pluginDir, base)); statErr == nil {
				if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", fmt.Sprintf("path traversal not allowed: %s", modulePath))); runErr != nil {
					panic(runErr)
				}
				return goja.Undefined()
			}
		}
		// Reject explicit non-.js extensions (unless the heuristic above matched)
		if ext := filepath.Ext(modulePath); ext != "" && ext != ".js" {
			if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", fmt.Sprintf("only .js modules may be required: %s", modulePath))); runErr != nil {
				panic(runErr)
			}
			return goja.Undefined()
		}
		// allow require("foo") to resolve to foo.js
		candidates := []string{modulePath}
		if !strings.HasSuffix(modulePath, ".js") {
			candidates = append(candidates, modulePath+".js")
		}

		var lastErr error
		for _, candidate := range candidates {
			resolved := filepath.Join(pluginDir, candidate)
			resolved = filepath.Clean(resolved)

			// (no-op)

			// prevent path traversal outside pluginDir
			base := pluginDir
			if !strings.HasSuffix(base, string(filepath.Separator)) {
				base = base + string(filepath.Separator)
			}
			if !strings.HasPrefix(resolved, base) {
				lastErr = fmt.Errorf("path traversal not allowed: %s", modulePath)
				continue
			}

			if visited[resolved] {
				// debug
				fmt.Fprintf(os.Stderr, "DEBUG visited hit: resolved=%s visitedKeys=%v\n", resolved, visited)
				cfg.Logf(1, "require: circular import detected: %s", modulePath)
				if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", fmt.Sprintf("circular import: %s", modulePath))); runErr != nil {
					panic(runErr)
				}
				return goja.Undefined()
			}

			content, err := os.ReadFile(resolved)
			if err != nil {
				lastErr = fmt.Errorf("cannot read %s: %w", modulePath, err)
				continue
			}

			visited[resolved] = true
			// ensure we remove from visited for other branches
			defer func(p string) { delete(visited, p) }(resolved)

			// provide a fresh module scope and run in the same VM so returned
			// values are compatible with the caller's runtime.
			script := fmt.Sprintf("var module={exports:{}};var exports=module.exports;\n%s\nmodule.exports", content)
			val, err := vm.RunString(script)
			if err != nil {
				cfg.Logf(1, "require: error executing %s: %v", modulePath, err)
				if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", fmt.Sprintf("error executing %s: %v", modulePath, err))); runErr != nil {
					panic(runErr)
				}
				return goja.Undefined()
			}
			return val
		}

		// If we reach here, no candidate succeeded — throw a JS-visible error.
		if lastErr != nil {
			if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", lastErr.Error())); runErr != nil {
				panic(runErr)
			}
			return goja.Undefined()
		}
		if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", fmt.Sprintf("only .js modules may be required: %s", modulePath))); runErr != nil {
			panic(runErr)
		}
		return goja.Undefined()
	}

	return load
}

// makeRequire returns a JS-callable require implementation that uses a fresh
// visited map per top-level require invocation. This is suitable for setting
// on the VM during plugin load-time; CallTool will overwrite require with a
// per-call variant that shares a visited map across nested requires.
func makeRequire(vm *goja.Runtime, pluginDir string, cfg config.Config) func(goja.FunctionCall) goja.Value {
	loader := makeModuleLoader(vm, pluginDir, cfg)
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			if _, runErr := vm.RunString(fmt.Sprintf("throw new Error(%q)", "require: module path required")); runErr != nil {
				panic(runErr)
			}
			return goja.Undefined()
		}
		modulePath := call.Arguments[0].String()
		return loader(modulePath, make(map[string]bool))
	}
}

func loadPlugin(path string, cfg config.Config) (*Plugin, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		cfg.Logf(1, "Failed to read plugin file %s: %v", path, err)
		return nil, err
	}

	vm := goja.New()

	// Inject the host API into the VM *before* running the plugin script so
	// that plugins which call host functions at module-load time (outside of
	// the call() function) see a properly initialised host object rather than
	// undefined.
	//
	// We use a temporary plugin name derived from the file path here; the real
	// name is extracted from the plugin's exports below and the host object is
	// refreshed again on every CallTool invocation with the correct context.
	tempName := filepath.Base(path)
	tempHost := host.MakeHostObject(cfg, context.Background(), tempName)
	if err := vm.Set("host", tempHost); err != nil {
		cfg.Logf(1, "Failed to set initial host API for plugin %s: %v", path, err)
		return nil, err
	}

	pluginDir := filepath.Dir(path)
	if err := vm.Set("require", makeRequire(vm, pluginDir, cfg)); err != nil {
		cfg.Logf(1, "Failed to set require for plugin %s: %v", path, err)
		return nil, err
	}

	script := fmt.Sprintf("var module = { exports: {} }; var exports = module.exports;\n%s\nmodule.exports", content)
	value, err := vm.RunString(script)
	if err != nil {
		cfg.Logf(1, "Error executing plugin %s: %v", path, err)
		return nil, err
	}

	obj := value.ToObject(vm)
	nameVal := obj.Get("name")

	// Guard against nil/undefined name before using it.
	name, ok := func() (string, bool) {
		if nameVal == nil {
			return "", false
		}
		s, ok := nameVal.Export().(string)
		return s, ok
	}()
	if !ok || name == "" {
		cfg.Logf(1, "Plugin %q missing name", path)
		return nil, fmt.Errorf("plugin %q missing name", path)
	}

	// Now that we know the real plugin name, refresh the host object so that
	// plugin-specific config (allowed paths, domains, etc.) is applied correctly.
	realHost := host.MakeHostObject(cfg, context.Background(), name)
	if err := vm.Set("host", realHost); err != nil {
		cfg.Logf(1, "Failed to set host API for plugin %s: %v", path, err)
		return nil, err
	}

	callVal := obj.Get("call")
	inputSchemaVal := obj.Get("inputSchema")
	if callVal == nil {
		cfg.Logf(1, "Plugin %q missing call function", name)
		return nil, fmt.Errorf("plugin %q missing call function", name)
	}
	callFunc, ok := goja.AssertFunction(callVal)
	if !ok {
		cfg.Logf(1, "Plugin %q call property is not a function", name)
		return nil, fmt.Errorf("plugin %q call property is not a function", name)
	}

	return &Plugin{
		Name:        name,
		Description: valueToString(obj.Get("description")),
		Version:     valueToString(obj.Get("version")),
		Meta:        safeExport(obj.Get("_meta")),
		Annotations: parseAnnotations(obj),
		Tags: func() []string {
			tagsVal := obj.Get("tags")
			if tagsVal == nil {
				return nil
			}
			exported := tagsVal.Export()
			if exported == nil {
				return nil
			}
			tagsArray, ok := exported.([]interface{})
			if !ok {
				return nil
			}
			tags := make([]string, 0, len(tagsArray))
			for _, t := range tagsArray {
				if str, ok := t.(string); ok {
					tags = append(tags, str)
				}
			}
			return tags
		}(),
		Commit:      valueToString(obj.Get("commit")),
		InputSchema: safeExport(inputSchemaVal),
		Call:        callFunc,
		VM:          vm,
		Dir:         pluginDir,
	}, nil
}

func (pm *PluginManager) ListTools(cfg config.Config) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		if !cfg.IsToolEnabled(plugin.Name) {
			continue
		}
		tools = append(tools, map[string]interface{}{
			"name":        plugin.Name,
			"description": plugin.Description,
			"tags":        plugin.Tags,
			"inputSchema": plugin.InputSchema,
			"version":     plugin.Version,
			"commit":      plugin.Commit,
			"_meta":       plugin.Meta,
			"annotations": plugin.Annotations,
		})
	}
	return tools
}

func (pm *PluginManager) CallTool(ctx context.Context, name string, rawArgs json.RawMessage, cfg config.Config) (interface{}, error) {
	cfg.Logf(2, "Calling tool %s with raw args %s", name, string(rawArgs))
	plugin, ok := pm.byName[name]
	if !ok {
		cfg.Logf(1, "Tool %s not found", name)
		return nil, fmt.Errorf("tool %q not found", name)
	}

	// runtime cycle detection is handled by the per-call 'require' loader

	var args interface{}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			cfg.Logf(1, "Failed to parse arguments for tool %s: %v", name, err)
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	} else {
		args = map[string]interface{}{}
	}

	if err := plugin.VM.Set("host", host.MakeHostObject(cfg, ctx, plugin.Name)); err != nil {
		cfg.Logf(1, "Failed to set host API for tool %s: %v", name, err)
		return nil, fmt.Errorf("failed to set host API: %w", err)
	}

	// Install a per-call `require` that shares a single `visited` map across
	// nested requires invoked during this plugin.Call invocation so circular
	// imports can be detected.
	loader := makeModuleLoader(plugin.VM, plugin.Dir, cfg)
	visited := make(map[string]bool)
	if plugin.File != "" {
		visited[filepath.Clean(plugin.File)] = true
	}
	if err := plugin.VM.Set("require", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			if _, runErr := plugin.VM.RunString(fmt.Sprintf("throw new Error(%q)", "require: module path required")); runErr != nil {
				panic(runErr)
			}
			return goja.Undefined()
		}
		return loader(call.Arguments[0].String(), visited)
	}); err != nil {
		cfg.Logf(1, "Failed to set require API for tool %s: %v", name, err)
		return nil, fmt.Errorf("failed to set require API: %w", err)
	}

	scriptArgs := plugin.VM.ToValue(args)
	result, err := plugin.Call(goja.Undefined(), scriptArgs)
	if err != nil {
		cfg.Logf(1, "Error executing tool %s: %v", name, err)
		return nil, fmt.Errorf("tool error: %w", err)
	}
	cfg.Logf(2, "Tool returned result for %s with raw args %s", name, string(rawArgs))
	exported := result.Export()
	// Normalize integer numeric types to float64 so callers observing JSON-like
	// numbers see consistent types.
	switch v := exported.(type) {
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	default:
		return exported, nil
	}
}

func (pm *PluginManager) GetAllTools(cfg config.Config) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		// Filter out disabled tools from the openapi.json response
		if !cfg.IsToolEnabled(plugin.Name) {
			continue
		}
		tools = append(tools, map[string]interface{}{
			"name":        plugin.Name,
			"description": plugin.Description,
			"tags":        plugin.Tags,
			"inputSchema": plugin.InputSchema,
			"version":     plugin.Version,
			"commit":      plugin.Commit,
			"_meta":       plugin.Meta,
		})
	}
	return tools
}

func OpenapiHandler(cfg config.Config, pm *PluginManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tools := pm.GetAllTools(cfg)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tools)
	}
}
