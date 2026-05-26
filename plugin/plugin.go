package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dop251/goja"

	"mcphe/config"
	"mcphe/host"
)

type Plugin struct {
	Name        string
	Description string
	Version     string
	Commit      string
	Tags        []string
	InputSchema interface{}
	Call        goja.Callable
	VM          *goja.Runtime
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
		Description: obj.Get("description").String(),
		Version:     obj.Get("version").String(),
		Tags: func() []string {
			tagsVal := obj.Get("tags")
			if tagsVal == nil {
				return nil
			}
			tagsArray, ok := tagsVal.Export().([]interface{})
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
		Commit:      obj.Get("commit").String(),
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
