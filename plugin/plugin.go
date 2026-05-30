package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

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

// safeExport safely exports a goja value, returning nil for undefined/null values
func safeExport(val goja.Value) interface{} {
	if val == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			// Handle any panic from Export()
		}
	}()
	exported := val.Export()
	return exported
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

func loadPlugin(path string, cfg config.Config) (*Plugin, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		cfg.Logf(1, "Failed to read plugin file %s: %v", path, err)
		return nil, err
	}

	vm := goja.New()

	script := fmt.Sprintf("var module = { exports: {} }; var exports = module.exports;\n%s\nmodule.exports", content)
	value, err := vm.RunString(script)
	if err != nil {
		cfg.Logf(1, "Error executing plugin %s: %v", path, err)
		return nil, err
	}

	obj := value.ToObject(vm)
	nameVal := obj.Get("name")
	hostValue := host.MakeHostObject(cfg, context.Background(), nameVal.String())
	if err := vm.Set("host", hostValue); err != nil {
		cfg.Logf(1, "Failed to set host API for plugin %s: %v", path, err)
		return nil, err
	}
	callVal := obj.Get("call")
	inputSchemaVal := obj.Get("inputSchema")

	name, ok := nameVal.Export().(string)
	if !ok || name == "" {
		cfg.Logf(1, "Plugin %q missing name", path)
		return nil, fmt.Errorf("plugin %q missing name", path)
	}
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
			tagsVal := obj.Get("Tags")
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
		InputSchema: inputSchemaVal.Export(),
		Call:        callFunc,
		VM:          vm,
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
			"Tags":        plugin.Tags,
			"Commit":      plugin.Commit,
			"inputSchema": plugin.InputSchema,
			"Version":     plugin.Version,
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

	scriptArgs := plugin.VM.ToValue(args)
	result, err := plugin.Call(goja.Undefined(), scriptArgs)
	if err != nil {
		cfg.Logf(1, "Error executing tool %s: %v", name, err)
		return nil, fmt.Errorf("tool error: %w", err)
	}
	return result.Export(), nil
}


func (pm *PluginManager) GetAllTools(cfg config.Config) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
	// ListTools is always enabled, but we still want to filter out disabled tools from the openapi.json response
		if !cfg.IsToolEnabled(plugin.Name) {
			continue
		}
		tools = append(tools, map[string]interface{}{
			"name":        plugin.Name,
			"description": plugin.Description,
			"Tags":        plugin.Tags,
			"Commit":      plugin.Commit,
			"inputSchema": plugin.InputSchema,
			"Version":     plugin.Version,
			"_meta":       plugin.Meta,
			"commit":      plugin.Commit,
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

func (pm *PluginManager) GetToolByName(name string) (*Plugin, bool) {
	plugin, ok := pm.byName[name]
	return plugin, ok
}

