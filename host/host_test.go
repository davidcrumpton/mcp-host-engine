package host

import (
	"context"
	"testing"

	"mcphe/config"
)

func TestMakeHostObject_Keys(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"foo": "bar"},
		},
	}
	obj := MakeHostObject(cfg, context.Background(), "myplugin")

	expectedKeys := []string{
		"logger", "config", "pid", "httpHeaders",
		"crypto", "sleep", "console", "path", "fs", "http",
		"exec", "process", "mcp", "server", "utils", "url",
	}
	for _, k := range expectedKeys {
		if _, ok := obj[k]; !ok {
			t.Errorf("host object missing key %q", k)
		}
	}
}

func TestMakeHostObject_ConfigMerge(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"custom_key": "custom_val"},
		},
	}
	obj := MakeHostObject(cfg, context.Background(), "myplugin")
	pluginCfg, ok := obj["config"].(map[string]interface{})
	if !ok {
		t.Fatal("config key should be a map")
	}
	if pluginCfg["custom_key"] != "custom_val" {
		t.Errorf("custom_key not found in plugin config: %v", pluginCfg)
	}
	if _, ok := pluginCfg["mcp-version"]; !ok {
		t.Error("mcp-version should be present in plugin config")
	}
}

func TestMakeHostObject_HTTPRequest(t *testing.T) {
	cfg := config.Config{}
	obj := MakeHostObject(cfg, context.Background(), "myplugin")
	httpObj, ok := obj["http"].(map[string]interface{})
	if !ok {
		t.Fatal("http should be a map")
	}
	reqFn, ok := httpObj["request"].(func(map[string]interface{}) (map[string]interface{}, error))
	if !ok {
		t.Fatal("http.request should be a function with correct signature")
	}

	// Verify that passing no url returns an error
	_, err := reqFn(map[string]interface{}{})
	if err == nil {
		t.Error("expected error when no url is provided")
	}
}