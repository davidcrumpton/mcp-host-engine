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
	"sync"

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

	// SourceName is the name the module exports, which is what a config entry's
	// "plugin:" alias refers to. It is Name unless this plugin is an aliased
	// instance of another entry.
	SourceName string

	// mu serialises access to VM.
	//
	// goja.Runtime is documented as not safe for concurrent use, and the MCP
	// SDK handles every tools/call in its own goroutine (jsonrpc2.Async), so
	// two requests for the same tool really do overlap -- an agent batching
	// independent calls is enough. Without the lock they run in one VM at once:
	// "concurrent map writes" kills the process, and even when it survives,
	// CallTool re-Set()s host and require per call, so one request can swap the
	// host object out from under another.
	//
	// Host functions are synchronous, so serialising per plugin costs no I/O
	// parallelism that this design had anyway.
	mu sync.Mutex

	// poisoned is set when the VM panicked mid-execution. A goja runtime that
	// has been unwound by a Go panic is not safe to reuse, so the instance is
	// disabled rather than risking corrupted state on the next call.
	poisoned error
}

type PluginManager struct {
	plugins []*Plugin
	byName  map[string]*Plugin

	// manifest, when non-nil, is the whole-directory checksum manifest from
	// config. Every plugin's entry file is verified at load time; this covers
	// the other files require() can pull in from plugin_dir, which are read at
	// call time and would otherwise never be checked.
	manifest *Manifest
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

	// Build the integrity policy before touching any plugin file, so a malformed
	// checksum or unreadable manifest is reported as a config error rather than
	// after some plugin code has already run.
	policy, err := newChecksumPolicy(cfg)
	if err != nil {
		return nil, err
	}
	if policy.manifest != nil {
		cfg.Logf(2, "Verifying plugin files against checksum manifest %s (%d entries)", policy.manifest.Path, policy.manifest.Len())
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

	// A checksum manifest describes the whole directory, so with one configured
	// the directory itself is under the operator's control: every .js in it must
	// be listed and must match, configured or not. This is checked here -- before
	// anything is executed and before the name index is built -- so a dropped-in
	// file can neither run nor influence which file a config entry resolves to.
	if policy.manifest != nil {
		for _, path := range files {
			content, readErr := readPluginFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("reading plugin %q: %w", path, readErr)
			}
			if verifyErr := policy.manifest.Verify(dir, path, content); verifyErr != nil {
				return nil, fmt.Errorf("plugin %q: %w", path, verifyErr)
			}
		}
	}

	// Index the directory by the name each file exports, read statically.
	//
	// Discovering a name by running the file (which is what this used to do)
	// meant executing every .js in the directory at startup, including files the
	// config never mentioned and files whose checksum was only checked
	// afterwards. The invariant is now: no bytes reach the JavaScript runtime
	// until they have been verified and their plugin has been configured.
	//
	// DetectPluginName reads the file as text, so a file whose name it cannot
	// find is still addressable -- under its file name -- and is only executed
	// if a config entry (or the legacy tools map) asks for it.
	bySource := make(map[string]string, len(files))
	for _, path := range files {
		name := DetectPluginName(path)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), ".js")
		}
		if prev, dup := bySource[name]; dup {
			return nil, fmt.Errorf("duplicate plugin name %q exported by %q and %q", name, prev, path)
		}
		bySource[name] = path
	}

	plugins := make([]*Plugin, 0, len(files))
	byName := make(map[string]*Plugin, len(files))

	// addInstance loads source plugin code as instanceName. Every instance gets
	// its own VM, so state is never shared between two tools -- including two
	// tools backed by the same file.
	addInstance := func(instanceName, source string) error {
		path, ok := bySource[source]
		if !ok {
			// A config entry may name the file instead of the name it exports
			// (useful when the export is not statically detectable). Only a
			// bare file name is considered, never a path.
			candidate := filepath.Join(dir, source+".js")
			if filepath.Base(source) != source || !fileExists(candidate) {
				return fmt.Errorf("plugin %q: source module %q not found in %s", instanceName, source, dir)
			}
			path = candidate
		}
		// Only plugins that are actually being loaded are subject to
		// require_plugin_checksums; a disabled entry never reaches here.
		if err := policy.requireDeclared(instanceName); err != nil {
			return err
		}
		cfg.Logf(2, "Loading plugin %s as %s", path, instanceName)
		p, err := loadPluginAs(path, cfg, instanceName, source, policy)
		if err != nil {
			return fmt.Errorf("loading plugin %q as %q: %w", path, instanceName, err)
		}
		plugins = append(plugins, p)
		byName[instanceName] = p
		return nil
	}

	// Phase 2a: every entry in the plugins map is an instance (or a disabled
	// entry). The map key is the tool name; "plugin:" selects the source.
	// Sorted so that a config with two bad entries always reports the same one.
	configuredSources := make(map[string]bool, len(cfg.Plugins))
	instanceNames := make([]string, 0, len(cfg.Plugins))
	for instanceName := range cfg.Plugins {
		instanceNames = append(instanceNames, instanceName)
	}
	sort.Strings(instanceNames)
	for _, instanceName := range instanceNames {
		source := cfg.PluginSource(instanceName)
		configuredSources[source] = true
		if !cfg.IsToolEnabled(instanceName) {
			cfg.Logf(2, "Plugin %s disabled by config", instanceName)
			continue
		}
		if err := addInstance(instanceName, source); err != nil {
			return nil, err
		}
	}

	// Phase 2b: keep legacy auto-registration for plugins that have no entry
	// in the plugins map (e.g. enabled via the old top-level tools map).
	sourceNames := make([]string, 0, len(bySource))
	for name := range bySource {
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)
	for _, name := range sourceNames {
		if configuredSources[name] {
			continue
		}
		if !cfg.IsToolEnabled(name) {
			cfg.Logf(2, "Plugin %s disabled by config", name)
			continue
		}
		if err := addInstance(name, name); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })
	return &PluginManager{plugins: plugins, byName: byName, manifest: policy.manifest}, nil
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
// defaultInputSchema is the schema used for a plugin that declares none: an
// object schema with no properties, which accepts any arguments.
//
// MCP requires a tool's inputSchema to be an object schema, and the SDK panics
// (it does not return an error) when a tool's schema is nil, empty, or not an
// object. A plugin that merely omits inputSchema must therefore be normalised
// here rather than allowed to take the whole server down at registration.
func defaultInputSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

// normalizeInputSchema validates the inputSchema exported by a plugin and
// returns a value that is safe to hand to the MCP SDK.
//
// A missing schema becomes an empty object schema. An object schema that omits
// "type" gets type "object" added, since that is what the author means and what
// the spec requires. Anything else is rejected: the SDK responds to a malformed
// schema by panicking, so the check has to happen while we can still report
// which plugin file is at fault and fail cleanly.
func normalizeInputSchema(val goja.Value) (interface{}, error) {
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return defaultInputSchema(), nil
	}
	exported := safeExport(val)
	if exported == nil {
		return defaultInputSchema(), nil
	}

	schema, ok := exported.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("inputSchema must be a JSON object, got %T", exported)
	}

	if rawType, present := schema["type"]; present {
		typeName, ok := rawType.(string)
		if !ok || typeName != "object" {
			return nil, fmt.Errorf(`inputSchema "type" must be the string "object", got %v`, rawType)
		}
	} else {
		schema["type"] = "object"
	}
	return schema, nil
}

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
func makeModuleLoader(vm *goja.Runtime, pluginDir string, cfg config.Config, manifest *Manifest) func(modulePath string, visited map[string]bool) goja.Value {
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

			content, err := readPluginFile(resolved)
			if err != nil {
				lastErr = fmt.Errorf("cannot read %s: %w", modulePath, err)
				continue
			}

			// With a manifest configured, a required module must be listed and
			// must match. This is the only place a require()'d file can be
			// checked: it is read here, at call time, long after startup
			// verified the entry file. A failure aborts the require outright
			// rather than falling through to the next candidate name.
			if manifest != nil {
				if verifyErr := manifest.Verify(pluginDir, resolved, content); verifyErr != nil {
					lastErr = fmt.Errorf("%s: %w", modulePath, verifyErr)
					cfg.Logf(1, "require: refusing to load %s: %v", resolved, verifyErr)
					break
				}
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
func makeRequire(vm *goja.Runtime, pluginDir string, cfg config.Config, manifest *Manifest) func(goja.FunctionCall) goja.Value {
	loader := makeModuleLoader(vm, pluginDir, cfg, manifest)
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

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// loadPluginAs loads the JavaScript at path and registers it under instanceName.
// Supplying an instance name is what allows the same plugin file to be loaded
// multiple times under different tool names with independent configuration;
// sourceName is the name the file is expected to export, which is what a
// "plugin:" alias refers to.
//
// The checksum is verified against the exact bytes about to be executed, before
// they reach the JavaScript runtime, so nothing runs unverified.
func loadPluginAs(path string, cfg config.Config, instanceName, sourceName string, policy *checksumPolicy) (*Plugin, error) {
	content, err := readPluginFile(path)
	if err != nil {
		cfg.Logf(1, "Failed to read plugin file %s: %v", path, err)
		return nil, err
	}

	// Enforce the integrity policy on the bytes just read.
	if err := policy.verifyEntry(instanceName, path, content); err != nil {
		cfg.Logf(1, "Plugin %s (%s) failed checksum verification: %v", instanceName, path, err)
		return nil, fmt.Errorf("plugin %q (%s): %w", instanceName, path, err)
	}

	vm := goja.New()

	// Inject the host API into the VM *before* running the plugin script so
	// that plugins which call host functions at module-load time (outside of
	// the call() function) see a properly initialised host object rather than
	// undefined. The instance name is used from the start, so per-instance
	// config is visible at module-load time and there is no window in which the
	// module body runs against another entry's policy. CallTool re-Sets it per
	// call, with that call's context.
	if err := vm.Set("host", host.MakeHostObject(cfg, context.Background(), instanceName)); err != nil {
		cfg.Logf(1, "Failed to set initial host API for plugin %s: %v", path, err)
		return nil, err
	}

	pluginDir := filepath.Dir(path)
	if err := vm.Set("require", makeRequire(vm, pluginDir, cfg, policy.manifest)); err != nil {
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

	// The file has to be the one the config asked for: an instance alias is
	// resolved through the exported name, and a stale index entry (a name found
	// in the wrong place, or a file replaced between indexing and loading) must
	// not silently register one plugin's code under another plugin's policy.
	if name != sourceName {
		cfg.Logf(1, "Plugin %q exports name %q, expected %q", path, name, sourceName)
		return nil, fmt.Errorf("plugin %q exports name %q but was loaded as %q", path, name, sourceName)
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
	inputSchema, err := normalizeInputSchema(inputSchemaVal)
	if err != nil {
		cfg.Logf(1, "Plugin %q (%s): %v", name, path, err)
		return nil, fmt.Errorf("plugin %q (%s): %w", name, path, err)
	}

	return &Plugin{
		Name:        instanceName,
		SourceName:  name,
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
		InputSchema: inputSchema,
		Call:        callFunc,
		VM:          vm,
		Dir:         pluginDir,
		File:        path,
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

// argumentNames lists the top-level field names of a tools/call argument
// object, for logging.
//
// The values are deliberately not logged. Tool arguments are produced by an LLM
// from prompt-influenced content and routinely carry passwords, API tokens and
// PII, and the old log line printed them verbatim at level 2 -- which the
// shipped config.yaml enables. Names are enough to see what a call looked like;
// anyone who needs a value can turn on the plugin's own logging.
func argumentNames(rawArgs json.RawMessage) string {
	if len(rawArgs) == 0 {
		return "none"
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawArgs, &fields); err != nil {
		return "<unparsable>"
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func (pm *PluginManager) CallTool(ctx context.Context, name string, rawArgs json.RawMessage, cfg config.Config) (result interface{}, err error) {
	cfg.Logf(2, "Calling tool %s with arguments [%s]", name, argumentNames(rawArgs))
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

	// One call at a time per plugin instance. See Plugin.mu: the VM is not safe
	// for concurrent use, and host/require are re-Set per call, so overlapping
	// calls do not merely corrupt the VM -- they hand one caller another
	// caller's context.
	plugin.mu.Lock()
	defer plugin.mu.Unlock()

	if plugin.poisoned != nil {
		return nil, plugin.poisoned
	}

	// A Go panic raised inside a host function is re-panicked by goja rather
	// than converted into a JS exception, and tools/call runs on a goroutine
	// that net/http does not own, so nothing else would recover it: the panic
	// would take the whole daemon down. Convert it into a tool error instead,
	// and disable the instance -- a runtime unwound mid-execution is not safe to
	// hand to the next caller.
	defer func() {
		if r := recover(); r != nil {
			plugin.poisoned = fmt.Errorf("plugin %q panicked and has been disabled for this process: %v", name, r)
			cfg.Logf(1, "%v", plugin.poisoned)
			result, err = nil, plugin.poisoned
		}
	}()

	if err := plugin.VM.Set("host", host.MakeHostObject(cfg, ctx, plugin.Name)); err != nil {
		cfg.Logf(1, "Failed to set host API for tool %s: %v", name, err)
		return nil, fmt.Errorf("failed to set host API: %w", err)
	}

	// Install a per-call `require` that shares a single `visited` map across
	// nested requires invoked during this plugin.Call invocation so circular
	// imports can be detected.
	loader := makeModuleLoader(plugin.VM, plugin.Dir, cfg, pm.manifest)
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
	callResult, err := plugin.Call(goja.Undefined(), scriptArgs)
	if err != nil {
		cfg.Logf(1, "Error executing tool %s: %v", name, err)
		return nil, fmt.Errorf("tool error: %w", err)
	}
	cfg.Logf(2, "Tool %s returned a result (arguments [%s])", name, argumentNames(rawArgs))
	exported := callResult.Export()
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
