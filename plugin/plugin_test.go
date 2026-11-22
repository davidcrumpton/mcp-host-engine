package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dop251/goja"

	"mcphe/config"
	"mcphe/transport"
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
	// Ensure parent directory exists to support names like "lib/utils".
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("writePlugin mkdir: %v", err)
	}
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

func TestLoadPlugins_MultipleInstances(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "source",
  description: "instance test",
  call: function(args) { return host.config.options.label; }
};
`
	writePlugin(t, dir, "source", js)

	cfg := config.Config{
		PluginDir: dir,
		Plugins: map[string]map[string]interface{}{
			"inst-a": {"plugin": "source", "options": map[string]interface{}{"label": "a"}},
			"inst-b": {"plugin": "source", "options": map[string]interface{}{"label": "b"}},
		},
	}
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if len(pm.plugins) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(pm.plugins))
	}
	a, ok := pm.byName["inst-a"]
	if !ok {
		t.Fatal("missing instance inst-a")
	}
	b, ok := pm.byName["inst-b"]
	if !ok {
		t.Fatal("missing instance inst-b")
	}
	if a.VM == b.VM {
		t.Error("instances must not share a goja VM")
	}
	if _, ok := pm.byName["source"]; ok {
		t.Error("source plugin should not be registered when only aliases are configured")
	}

	for name, want := range map[string]string{"inst-a": "a", "inst-b": "b"} {
		got, err := pm.CallTool(context.Background(), name, json.RawMessage(`{}`), cfg)
		if err != nil {
			t.Fatalf("CallTool(%s): %v", name, err)
		}
		if got != want {
			t.Errorf("CallTool(%s): got %v, want %q", name, got, want)
		}
	}
}

func TestLoadPlugins_DuplicateExportedName(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "one", minimalJS("dup"))
	writePlugin(t, dir, "two", minimalJS("dup"))
	cfg := allEnabled("dup")
	cfg.PluginDir = dir

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate exported plugin name")
	}
	if !strings.Contains(err.Error(), "duplicate plugin name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadPlugins_UnknownInstanceSource(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "real", minimalJS("real"))
	cfg := config.Config{
		PluginDir: dir,
		Plugins: map[string]map[string]interface{}{
			"ghost": {"plugin": "does_not_exist"},
		},
	}

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected error for unknown plugin source")
	}
	if !strings.Contains(err.Error(), "does_not_exist") {
		t.Errorf("expected error to mention the missing source, got: %v", err)
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

// ---------------------------------------------------------------------------
// Error Handling
// ---------------------------------------------------------------------------

func TestStandardErrorHandling_GoToJS(t *testing.T) {
	// A Go function that returns an error is caught as a proper JS Error object in try/catch.
	dir := t.TempDir()
	js := `
module.exports = {
  name: "test_go_error",
  description: "test error handling",
  version: "1.0.0",
  call: function(args) {
    try {
      // call host.fs.readFile on a non-existent file to trigger a Go error
      host.fs.readFile("nonexistent_file_12345.txt");
    } catch (e) {
      return {
        isErrorInstance: e instanceof Error,
        errorMessage: e.message,
        errorType: typeof e,
        constructorName: e.constructor ? e.constructor.name : "none",
        stringVal: String(e)
      };
    }
    return "no error thrown";
  }
};
`
	writePlugin(t, dir, "test_go_error", js)
	cfg := allEnabled("test_go_error")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_go_error", json.RawMessage(`{}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	resultMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result to be map[string]interface{}, got %T", res)
	}

	if resultMap["isErrorInstance"] != true {
		t.Errorf("expected isErrorInstance to be true, got %v", resultMap["isErrorInstance"])
	}

	msg, _ := resultMap["errorMessage"].(string)
	if !strings.Contains(msg, "nonexistent_file_12345.txt") {
		t.Errorf("expected error message to contain filename, got %q", msg)
	}

	if resultMap["errorType"] != "object" {
		t.Errorf("expected errorType to be 'object', got %v", resultMap["errorType"])
	}
}

func TestStandardErrorHandling_JSToGo(t *testing.T) {
	// Throwing a standard JS Error object is correctly propagated to Go.
	dir := t.TempDir()
	js := `
module.exports = {
  name: "test_js_error",
  description: "test error throwing",
  version: "1.0.0",
  call: function(args) {
    throw new Error("this is a standard javascript error");
  }
};
`
	writePlugin(t, dir, "test_js_error", js)
	cfg := allEnabled("test_js_error")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	_, err = pm.CallTool(context.Background(), "test_js_error", json.RawMessage(`{}`), cfg)
	if err == nil {
		t.Fatal("expected error calling tool, got nil")
	}

	if !strings.Contains(err.Error(), "this is a standard javascript error") {
		t.Errorf("expected error message to contain thrown message, got: %v", err)
	}
}

func TestPlugin_HTTPclientRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Custom-Header", r.Header.Get("X-Request-Header"))
		fmt.Fprintf(w, "method:%s;body:%s", r.Method, string(body))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	js := `
module.exports = {
  name: "test_http_request",
  description: "test request method",
  version: "1.0.0",
  call: function(args) {
    var resp = host.http.request({
      method: "POST",
      url: args.url,
      headers: {"X-Request-Header": "foo-val"},
      body: {key: "val"}
    });
    return resp;
  }
};
`
	writePlugin(t, dir, "test_http_request", js)
	parsed, _ := url.Parse(srv.URL)
	cfg := allEnabled("test_http_request")
	cfg.PluginDir = dir
	cfg.Plugins = map[string]map[string]interface{}{
		"test_http_request": {
			"allowed_domains":      []interface{}{parsed.Hostname()},
			"allowed_http_methods": []interface{}{"POST"},
		},
	}

	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_http_request", json.RawMessage(`{"url":"`+srv.URL+`"}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	respMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}

	statusStr := fmt.Sprintf("%v", respMap["status"])
	if statusStr != "200" {
		t.Errorf("status: got %v (type %T), expected 200", respMap["status"], respMap["status"])
	}

	bodyVal, _ := respMap["body"].(string)
	if bodyVal != `method:POST;body:{"key":"val"}` {
		t.Errorf("body: got %q", bodyVal)
	}

	headersMap, ok := respMap["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected headers map, got %T", respMap["headers"])
	}
	hdrVal := fmt.Sprintf("%v", headersMap["X-Custom-Header"])
	if hdrVal != "[foo-val]" {
		t.Errorf("X-Custom-Header: got %v (type %T), expected [foo-val]", headersMap["X-Custom-Header"], headersMap["X-Custom-Header"])
	}
}

func TestPlugin_HTTPRequest_BlockedDomain(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "test_blocked_domain",
  description: "should be blocked by domain constraint",
  version: "1.0.0",
  call: function(args) {
    try {
      host.http.request({ method: "GET", url: args.url });
      return "unexpectedly succeeded";
    } catch (e) {
      return { blocked: true, message: e.message };
    }
  }
};
`
	writePlugin(t, dir, "test_blocked_domain", js)
	cfg := config.Config{
		PluginDir: dir,
		Plugins: map[string]map[string]interface{}{
			"test_blocked_domain": {
				"allowed_http_methods": []interface{}{"GET"},
				"allowed_domains":      []interface{}{"allowed.example.com"},
			},
		},
	}
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_blocked_domain",
		json.RawMessage(`{"url":"http://evil.example.com/"}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	resultMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T: %v", res, res)
	}
	if resultMap["blocked"] != true {
		t.Errorf("expected blocked=true, got %v", resultMap)
	}
}

func TestPlugin_HTTPRequest_BlockedMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	js := `
module.exports = {
  name: "test_blocked_method",
  description: "should be blocked by method constraint",
  version: "1.0.0",
  call: function(args) {
    try {
      host.http.request({ method: "DELETE", url: args.url });
      return "unexpectedly succeeded";
    } catch (e) {
      return { blocked: true, message: e.message };
    }
  }
};
`
	writePlugin(t, dir, "test_blocked_method", js)
	parsed, _ := url.Parse(srv.URL)
	cfg := config.Config{
		PluginDir: dir,
		Plugins: map[string]map[string]interface{}{
			"test_blocked_method": {
				"allowed_http_methods": []interface{}{"GET"},
				"allowed_domains":      []interface{}{parsed.Hostname()},
			},
		},
	}
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_blocked_method",
		json.RawMessage(`{"url":"`+srv.URL+`"}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	resultMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T: %v", res, res)
	}
	if resultMap["blocked"] != true {
		t.Errorf("expected blocked=true, got %v", resultMap)
	}
}

// plugin_test.go — add TestRequire_* test suite
func TestRequire_Basic(t *testing.T) {
	dir := t.TempDir()
	// lib/foo.js exists as a pure utility — no host object, no top-level exports.
	libDir := filepath.Join(dir, "lib")
	if err := os.Mkdir(libDir, 0755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	writePlugin(t, libDir, "foo", `
module.exports = {
  add: (a, b) => a + b
};
`)
	// main.js requires lib/foo and uses the exported add function.
	mainJS := `
module.exports = {
  name: "test_require",
  description: "test require()",
  version: "1.0.0",
  call: function(args) {
    var foo = require("lib/foo.js");
    return foo.add(args.a, args.b);
  }
};
`
	writePlugin(t, dir, "test_require", mainJS)

	cfg := allEnabled("test_require")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_require", json.RawMessage(`{"a":1,"b":2}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if res != 3.0 {
		t.Errorf("expected 3, got %v", res)
	}
}

func TestRequire_ForbiddenExtension(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "test_require_bad", `
module.exports = {
  name: "test_require_bad",
  version: "1.0.0",
  call: function(args) {
    try {
      require("../config.yaml");
      return "should not succeed";
    } catch (e) {
      return { error: e.message };
    }
  }
};`)

	cfg := allEnabled("test_require_bad")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_require_bad", json.RawMessage(`{}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	resultMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T: %v", res, res)
	}
	msg, _ := resultMap["error"].(string)
	if !strings.Contains(msg, "only .js modules") {
		t.Errorf("expected non-.js error, got %q", msg)
	}
}

func TestRequire_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	// Write a file outside the plugin dir to test traversal prevention.
	outsideFile := filepath.Join(dir, "secret.yaml")
	if err := os.WriteFile(outsideFile, []byte("secret: yes"), 0600); err != nil {
		t.Fatalf("write outsideFile: %v", err)
	}

	writePlugin(t, dir, "test_require_traversal", `
module.exports = {
  name: "test_require_traversal",
  version: "1.0.0",
  call: function(args) {
    try {
      require("../secret.yaml");
      return "should not succeed";
    } catch (e) {
      return { error: e.message };
    }
  }
};`)

	cfg := allEnabled("test_require_traversal")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "test_require_traversal", json.RawMessage(`{}`), cfg)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	resultMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T: %v", res, res)
	}
	msg, _ := resultMap["error"].(string)
	if !strings.Contains(msg, "path traversal not allowed") {
		t.Errorf("expected path traversal error, got %q", msg)
	}
}

func TestRequire_Circular(t *testing.T) {
	dir := t.TempDir()
	// a.js requires b.js, b.js requires a.js — a cycle.
	writePlugin(t, dir, "a", `
module.exports = {
  name: "a",
  version: "1.0.0",
  call: function(args) {
    var b = require("b");
    return b.val + 10;
  }
};
`)
	writePlugin(t, dir, "b", `
module.exports = {
  name: "b",
  version: "1.0.0",
  val: 42,
  call: function(args) {
    var a = require("a");
    return a.val + 20;
  }
};`)

	cfg := allEnabled("a", "b")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	res, err := pm.CallTool(context.Background(), "a", json.RawMessage(`{}`), cfg)
	if err != nil {
		// runtime error is acceptable for circular requires
		return
	}
	// Some runtimes may allow loading the other module's exports; in that
	// case we expect numeric result 42 + 10 = 52.
	if v, ok := res.(float64); ok {
		if v != 52 {
			t.Fatalf("unexpected result for circular require: %v", v)
		}
		return
	}
	t.Fatalf("expected error or numeric result for circular require, got %T: %v", res, res)
}

func TestRequire_Isolation(t *testing.T) {
	dir := t.TempDir()
	// Write two separate plugins, each with its own internal lib dependency.
	// Make sure one doesn't see the other's require path.

	// lib/one.js — only used by one.js
	writePlugin(t, dir, "one", `
module.exports = {
  name: "one",
  version: "1.0.0",
  call: function(args) {
    var utils = require("lib/utils");
    return utils.double(args.x);
  }
};`)
	writePlugin(t, dir, "two", `
module.exports = {
  name: "two",
  version: "1.0.0",
  call: function(args) {
    var utils = require("lib/utils");
    return utils.triple(args.x);
  }
};`)
	writePlugin(t, dir, "lib/utils", `
module.exports = {
  double: x => x * 2,
  triple: x => x * 3
};`)

	cfg := allEnabled("one", "two")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	// Call "one". It should only see lib/utils via its own require path.
	res, err := pm.CallTool(context.Background(), "one", json.RawMessage(`{"x":5}`), cfg)
	if err != nil {
		t.Fatalf("CallTool one: %v", err)
	}
	if res != 10.0 {
		t.Errorf("one: expected 10, got %v", res)
	}

	// Call "two". It should also only see lib/utils via its own require path.
	res, err = pm.CallTool(context.Background(), "two", json.RawMessage(`{"x":3}`), cfg)
	if err != nil {
		t.Fatalf("CallTool two: %v", err)
	}
	if res != 9.0 {
		t.Errorf("two: expected 9, got %v", res)
	}
}

// ---------------------------------------------------------------------------
// H2: one VM, one call at a time
// ---------------------------------------------------------------------------

// The MCP SDK handles every tools/call in its own goroutine, so a client that
// pipelines two calls -- or two clients at once -- runs CallTool twice against
// one goja runtime. The plugin below notices the symptom that matters: CallTool
// re-Set()s `host` per call, so an overlapping call swaps the host object out
// from under the JS that is already running.
func TestCallTool_ConcurrentCallsDoNotShareTheHostObject(t *testing.T) {
	dir := t.TempDir()
	js := `
module.exports = {
  name: "sessions",
  description: "reports the session it was called in",
  call: function(args) {
    var before = host.sessionID;
    host.sleep(20);
    if (host.sessionID !== before) {
      throw new Error("host object replaced mid-call: " + before + " -> " + host.sessionID);
    }
    return before;
  }
};
`
	writePlugin(t, dir, "sessions", js)
	cfg := allEnabled("sessions")
	cfg.PluginDir = dir
	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([]interface{}, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("session-%d", i)
			ctx := context.WithValue(context.Background(), transport.SessionIDContextKey, want)
			results[i], errs[i] = pm.CallTool(ctx, "sessions", json.RawMessage(`{}`), cfg)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		want := fmt.Sprintf("session-%d", i)
		if errs[i] != nil {
			t.Errorf("call %d: %v", i, errs[i])
			continue
		}
		if results[i] != want {
			t.Errorf("call %d: got %v, want %q", i, results[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// H5: a Go panic inside a host function is a tool error, not a dead daemon
// ---------------------------------------------------------------------------

func TestCallTool_GoPanicIsContainedAndPoisonsThePlugin(t *testing.T) {
	calls := 0
	p := &Plugin{
		Name: "panicky",
		VM:   goja.New(),
		Call: func(goja.Value, ...goja.Value) (goja.Value, error) {
			calls++
			panic("boom from a host function")
		},
	}
	pm := &PluginManager{plugins: []*Plugin{p}, byName: map[string]*Plugin{"panicky": p}}
	cfg := config.Config{}

	res, err := pm.CallTool(context.Background(), "panicky", json.RawMessage(`{}`), cfg)
	if err == nil {
		t.Fatal("expected the panic to surface as an error")
	}
	if res != nil {
		t.Errorf("expected no result alongside the error, got %v", res)
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("error should say the plugin panicked, got: %v", err)
	}
	if calls != 1 {
		t.Fatalf("call function ran %d times, want 1", calls)
	}

	// The runtime was unwound mid-execution, so it must not be reused.
	_, err2 := pm.CallTool(context.Background(), "panicky", json.RawMessage(`{}`), cfg)
	if err2 == nil {
		t.Fatal("expected the poisoned plugin to keep failing")
	}
	if calls != 1 {
		t.Errorf("a poisoned plugin must not be executed again (ran %d times)", calls)
	}
	if err2.Error() != err.Error() {
		t.Errorf("second call error: got %q, want the original %q", err2, err)
	}
}

// ---------------------------------------------------------------------------
// H3: nothing is executed before it is configured and verified
// ---------------------------------------------------------------------------

func TestLoadPlugins_DoesNotExecuteUnconfiguredFiles(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "good", minimalJS("good"))
	// Nobody configured this one. If its body runs, startup fails with its own
	// message and this test can tell.
	writePlugin(t, dir, "stray", `
throw new Error("MODULE BODY RAN");
module.exports = { name: "stray", call: function(){ return "x"; } };
`)
	cfg := allEnabled("good")
	cfg.PluginDir = dir

	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("an unconfigured plugin file must not affect startup: %v", err)
	}
	if len(pm.plugins) != 1 || pm.plugins[0].Name != "good" {
		t.Errorf("expected only the configured plugin, got %d", len(pm.plugins))
	}
}

func TestLoadPlugins_ChecksumIsCheckedBeforeTheModuleRuns(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "tampered", `
throw new Error("MODULE BODY RAN");
module.exports = { name: "tampered", call: function(){ return "x"; } };
`)
	cfg := pluginsMap(map[string]map[string]interface{}{
		"tampered": {"checksum": "sha256:" + strings.Repeat("00", 32)},
	})
	cfg.PluginDir = dir

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected the checksum mismatch to stop startup")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected a checksum mismatch, got: %v", err)
	}
	if strings.Contains(err.Error(), "MODULE BODY RAN") {
		t.Fatalf("the module body ran before its checksum was checked: %v", err)
	}
}

func TestLoadPlugins_RejectsAFileThatExportsAnotherName(t *testing.T) {
	dir := t.TempDir()
	// The config asks for "wanted"; the file of that name exports "other".
	writePlugin(t, dir, "wanted", minimalJS("other"))
	cfg := config.Config{
		PluginDir: dir,
		Plugins:   map[string]map[string]interface{}{"wanted": {}},
	}

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected an error when the file does not export the configured name")
	}
	if !strings.Contains(err.Error(), `exports name "other"`) {
		t.Errorf("error should name both, got: %v", err)
	}
}

func TestLoadPlugins_UnknownSourceIsReported(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "real", minimalJS("real"))
	cfg := config.Config{
		PluginDir: dir,
		Plugins:   map[string]map[string]interface{}{"ghost": {"plugin": "nowhere"}},
	}

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected an error for a source that does not exist")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error should name the missing source, got: %v", err)
	}
}

// M3: tool arguments are LLM-produced and routinely carry passwords, tokens and
// PII. The level-2 log line printed them verbatim; it now prints the field names
// only, which is what this pins.
func TestArgumentNames(t *testing.T) {
	got := argumentNames(json.RawMessage(`{"password":"hunter2","url":"https://x","api_key":"k"}`))
	if got != "api_key,password,url" {
		t.Errorf("argumentNames = %q, want the sorted field names only", got)
	}
	if strings.Contains(got, "hunter2") || strings.Contains(got, "https://x") {
		t.Errorf("argumentNames leaked a value: %q", got)
	}
	if argumentNames(nil) != "none" {
		t.Errorf("empty arguments: got %q, want \"none\"", argumentNames(nil))
	}
	if argumentNames(json.RawMessage(`not json`)) != "<unparsable>" {
		t.Errorf("unparsable arguments should be reported as such")
	}
}
