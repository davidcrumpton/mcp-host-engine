package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"mcphe/config"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// allEnabled returns a Config where every named tool is enabled.
func allEnabled(names ...string) config.Config {
	tools := make(map[string]bool, len(names))
	for _, n := range names {
		tools[n] = true
	}
	return config.Config{Verbosity: 0, Tools: tools}
}

// writePlugin writes a minimal valid JS plugin file to dir and returns its path.
func writePlugin(t *testing.T, dir, name, js string) string {
	t.Helper()
	path := filepath.Join(dir, name+".js")
	if err := os.WriteFile(path, []byte(js), 0644); err != nil {
		t.Fatalf("writePlugin: %v", err)
	}
	return path
}

// minimalJS returns the smallest JS export that loadPlugin will accept.
func minimalJS(name string) string {
	return `
module.exports = {
  name: "` + name + `",
  description: "test plugin",
  version: "1.0.0",
  call: function(args) { return "ok"; }
};
`
}

// loadOne is a convenience wrapper that loads a single plugin from a temp dir.
func loadOne(t *testing.T, name, js string) (*PluginManager, config.Config) {
	t.Helper()
	dir := t.TempDir()
	writePlugin(t, dir, name, js)
	cfg := allEnabled(name)
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	return pm, cfg
}

// ---------------------------------------------------------------------------
// valueToString
// ---------------------------------------------------------------------------

func TestValueToString_Nil(t *testing.T) {
	if got := valueToString(nil); got != "" {
		t.Errorf("expected \"\", got %q", got)
	}
}

// ---------------------------------------------------------------------------
// LoadPlugins — error paths
// ---------------------------------------------------------------------------

func TestLoadPlugins_MissingDir(t *testing.T) {
	cfg := config.Config{PluginDir: filepath.Join(t.TempDir(), "no_such_dir")}
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestLoadPlugins_EmptyDir(t *testing.T) {
	cfg := config.Config{PluginDir: t.TempDir()}
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error when no .js files are present")
	}
}

func TestLoadPlugins_DefaultPluginDir(t *testing.T) {
	// PluginDir == "" should fall back to "plugins"; that path won't exist in
	// the test environment, so we just verify we get an error rather than a panic.
	cfg := config.Config{PluginDir: ""}
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error when default 'plugins' dir does not exist")
	}
}

func TestLoadPlugins_BadJS(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "bad", "this is not valid JS }{{{")
	cfg := allEnabled("bad")
	cfg.PluginDir = dir
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error for invalid JS")
	}
}

func TestLoadPlugins_MissingName(t *testing.T) {
	dir := t.TempDir()
	js := `module.exports = { call: function(a){return a;} };`
	writePlugin(t, dir, "noname", js)
	cfg := allEnabled("noname")
	cfg.PluginDir = dir
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error for plugin missing name")
	}
}

func TestLoadPlugins_MissingCallFunction(t *testing.T) {
	dir := t.TempDir()
	js := `module.exports = { name: "nocall", description: "x" };`
	writePlugin(t, dir, "nocall", js)
	cfg := allEnabled("nocall")
	cfg.PluginDir = dir
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error for plugin missing call function")
	}
}

func TestLoadPlugins_CallNotFunction(t *testing.T) {
	dir := t.TempDir()
	js := `module.exports = { name: "badcall", call: "not a function" };`
	writePlugin(t, dir, "badcall", js)
	cfg := allEnabled("badcall")
	cfg.PluginDir = dir
	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error when call is not a function")
	}
}

// ---------------------------------------------------------------------------
// LoadPlugins — happy path
// ---------------------------------------------------------------------------

func TestLoadPlugins_Success(t *testing.T) {
	pm, _ := loadOne(t, "myplugin", minimalJS("myplugin"))
	if pm == nil {
		t.Fatal("expected non-nil PluginManager")
	}
}

func TestLoadPlugins_SortedByName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zebra", "alpha", "mango"} {
		writePlugin(t, dir, name, minimalJS(name))
	}
	cfg := allEnabled("zebra", "alpha", "mango")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	names := make([]string, len(pm.plugins))
	for i, p := range pm.plugins {
		names[i] = p.Name
	}
	expected := []string{"alpha", "mango", "zebra"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("plugins[%d]: got %q, want %q", i, names[i], want)
		}
	}
}

func TestLoadPlugins_DisabledToolSkipped(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "enabled", minimalJS("enabled"))
	writePlugin(t, dir, "disabled", minimalJS("disabled"))
	cfg := config.Config{
		PluginDir: dir,
		Tools:     map[string]bool{"enabled": true, "disabled": false},
	}
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(pm.plugins) != 1 || pm.plugins[0].Name != "enabled" {
		t.Errorf("expected only 'enabled' plugin, got %v", pm.plugins)
	}
}

func TestLoadPlugins_PluginFields(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "rich",
  description: "rich plugin",
  version: "2.3.4",
  commit: "abc123",
  call: function(args) { return args; }
};
`
	writePlugin(t, dir, "rich", js)
	cfg := allEnabled("rich")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	p := pm.plugins[0]
	if p.Name != "rich" {
		t.Errorf("Name: got %q", p.Name)
	}
	if p.Description != "rich plugin" {
		t.Errorf("Description: got %q", p.Description)
	}
	if p.Version != "2.3.4" {
		t.Errorf("Version: got %q", p.Version)
	}
	if p.Commit != "abc123" {
		t.Errorf("Commit: got %q", p.Commit)
	}
}

// ---------------------------------------------------------------------------
// Annotations
// ---------------------------------------------------------------------------

func TestLoadPlugins_Annotations(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "annotated",
  description: "has annotations",
  annotations: { readOnlyHint: true, destructiveHint: false },
  call: function(args) { return "ok"; }
};
`
	writePlugin(t, dir, "annotated", js)
	cfg := allEnabled("annotated")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	ann := pm.plugins[0].Annotations
	if ann == nil {
		t.Fatal("expected non-nil annotations")
	}
	if ann.ReadOnlyHint == nil || !*ann.ReadOnlyHint {
		t.Error("readOnlyHint should be true")
	}
	if ann.DestructiveHint == nil || *ann.DestructiveHint {
		t.Error("destructiveHint should be false")
	}
}

func TestLoadPlugins_AnnotationsAllAbsent(t *testing.T) {
	// annotations object exists but has no recognised bool fields → nil
	dir := t.TempDir()
	js := `
module.exports = {
  name: "nohints",
  annotations: { someOtherKey: "value" },
  call: function(args) { return "ok"; }
};
`
	writePlugin(t, dir, "nohints", js)
	cfg := allEnabled("nohints")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if pm.plugins[0].Annotations != nil {
		t.Error("expected nil annotations when no hints are set")
	}
}

func TestLoadPlugins_NoAnnotations(t *testing.T) {
	pm, _ := loadOne(t, "plain", minimalJS("plain"))
	if pm.plugins[0].Annotations != nil {
		t.Error("expected nil annotations for plugin without annotations property")
	}
}

// ---------------------------------------------------------------------------
// PluginManager.ListTools
// ---------------------------------------------------------------------------

func TestListTools_Empty(t *testing.T) {
	pm := &PluginManager{plugins: nil, byName: map[string]*Plugin{}}
	cfg := config.Config{Tools: map[string]bool{}}
	tools := pm.ListTools(cfg)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestListTools_ContainsExpectedKeys(t *testing.T) {
	pm, cfg := loadOne(t, "myplugin", minimalJS("myplugin"))
	tools := pm.ListTools(cfg)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	for _, key := range []string{"name", "description", "inputSchema", "annotations"} {
		if _, ok := tool[key]; !ok {
			t.Errorf("tool map missing key %q", key)
		}
	}
	if tool["name"] != "myplugin" {
		t.Errorf("name: got %v", tool["name"])
	}
}

func TestListTools_FilterDisabled(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "on", minimalJS("on"))
	writePlugin(t, dir, "off", minimalJS("off"))
	cfg := config.Config{
		PluginDir: dir,
		Tools:     map[string]bool{"on": true, "off": true},
	}
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	// Now disable "off" at list time
	listCfg := config.Config{Tools: map[string]bool{"on": true, "off": false}}
	tools := pm.ListTools(listCfg)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["name"] != "on" {
		t.Errorf("expected 'on', got %v", tools[0]["name"])
	}
}

// ---------------------------------------------------------------------------
// PluginManager.GetAllTools
// ---------------------------------------------------------------------------

func TestGetAllTools_ContainsExpectedKeys(t *testing.T) {
	pm, cfg := loadOne(t, "tool1", minimalJS("tool1"))
	tools := pm.GetAllTools(cfg)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	for _, key := range []string{"name", "description", "inputSchema"} {
		if _, ok := tools[0][key]; !ok {
			t.Errorf("GetAllTools map missing key %q", key)
		}
	}
	// GetAllTools should NOT include "annotations"
	if _, ok := tools[0]["annotations"]; ok {
		t.Error("GetAllTools should not include 'annotations'")
	}
}

func TestGetAllTools_FilterDisabled(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "a", minimalJS("a"))
	writePlugin(t, dir, "b", minimalJS("b"))
	cfg := config.Config{
		PluginDir: dir,
		Tools:     map[string]bool{"a": true, "b": true},
	}
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	listCfg := config.Config{Tools: map[string]bool{"a": true, "b": false}}
	tools := pm.GetAllTools(listCfg)
	if len(tools) != 1 || tools[0]["name"] != "a" {
		t.Errorf("expected only tool 'a', got %v", tools)
	}
}

// ---------------------------------------------------------------------------
// PluginManager.GetToolByName
// ---------------------------------------------------------------------------

func TestGetToolByName_Found(t *testing.T) {
	pm, _ := loadOne(t, "finder", minimalJS("finder"))
	p, ok := pm.GetToolByName("finder")
	if !ok {
		t.Fatal("expected to find 'finder'")
	}
	if p.Name != "finder" {
		t.Errorf("Name: got %q", p.Name)
	}
}

func TestGetToolByName_NotFound(t *testing.T) {
	pm, _ := loadOne(t, "finder", minimalJS("finder"))
	_, ok := pm.GetToolByName("nonexistent")
	if ok {
		t.Error("expected false for nonexistent tool")
	}
}

// ---------------------------------------------------------------------------
// PluginManager.CallTool
// ---------------------------------------------------------------------------

func TestCallTool_NotFound(t *testing.T) {
	pm, cfg := loadOne(t, "present", minimalJS("present"))
	_, err := pm.CallTool(context.Background(), "absent", json.RawMessage(`{}`), cfg)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestCallTool_Success(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "adder",
  description: "adds two numbers",
  call: function(args) { return (args.x || 0) + (args.y || 0); }
};
`
	writePlugin(t, dir, "adder", js)
	cfg := allEnabled("adder")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	result, err := pm.CallTool(context.Background(), "adder", json.RawMessage(`{"x":3,"y":4}`), cfg)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// goja exports numbers as int64 or float64
	switch v := result.(type) {
	case int64:
		if v != 7 {
			t.Errorf("expected 7, got %d", v)
		}
	case float64:
		if v != 7 {
			t.Errorf("expected 7, got %f", v)
		}
	default:
		t.Errorf("unexpected result type %T: %v", result, result)
	}
}

func TestCallTool_EmptyArgs(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "echo",
  description: "returns a fixed string",
  call: function(args) { return "hello"; }
};
`
	writePlugin(t, dir, "echo", js)
	cfg := allEnabled("echo")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	result, err := pm.CallTool(context.Background(), "echo", json.RawMessage(``), cfg)
	if err != nil {
		t.Fatalf("CallTool with empty args: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected \"hello\", got %v", result)
	}
}

func TestCallTool_InvalidJSON(t *testing.T) {
	pm, cfg := loadOne(t, "myplugin", minimalJS("myplugin"))
	_, err := pm.CallTool(context.Background(), "myplugin", json.RawMessage(`{bad json`), cfg)
	if err == nil {
		t.Fatal("expected error for invalid JSON args")
	}
}

func TestCallTool_ToolError(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "failer",
  description: "always throws",
  call: function(args) { throw new Error("intentional failure"); }
};
`
	writePlugin(t, dir, "failer", js)
	cfg := allEnabled("failer")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	_, err = pm.CallTool(context.Background(), "failer", json.RawMessage(`{}`), cfg)
	if err == nil {
		t.Fatal("expected error when JS throws")
	}
}

// ---------------------------------------------------------------------------
// OpenapiHandler
// ---------------------------------------------------------------------------

func TestOpenapiHandler_ResponseIsJSON(t *testing.T) {
	pm, cfg := loadOne(t, "myapi", minimalJS("myapi"))
	handler := OpenapiHandler(cfg, pm)
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q", ct)
	}
	var tools []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["name"] != "myapi" {
		t.Errorf("tool name: got %v", tools[0]["name"])
	}
}

func TestOpenapiHandler_EmptyManager(t *testing.T) {
	pm := &PluginManager{plugins: nil, byName: map[string]*Plugin{}}
	cfg := config.Config{Tools: map[string]bool{}}
	handler := OpenapiHandler(cfg, pm)
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	var tools []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}
