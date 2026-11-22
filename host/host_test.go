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

// M10: server.config(name) used to take any plugin name from JS and return that
// plugin's whole config block -- which is where operators are told to put API
// keys. A plugin may now see its own block and nothing else.
func TestServerConfigOnlyExposesTheCallersOwnPlugin(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"mine":      {"options": map[string]interface{}{"api_token": "mine-token"}},
			"neighbour": {"options": map[string]interface{}{"api_token": "neighbour-token"}},
		},
	}
	obj := MakeHostObject(cfg, context.Background(), "mine")
	server := obj["server"].(map[string]interface{})
	getConfig := server["config"].(func(string) map[string]interface{})

	own := getConfig("mine")
	if own["options"] == nil {
		t.Error("a plugin should still see its own config block")
	}

	for _, name := range []string{"neighbour", "does-not-exist"} {
		got := getConfig(name)
		if _, leaked := got["options"]; leaked {
			t.Errorf("config(%q) leaked another plugin's options: %v", name, got)
		}
		if _, ok := got["mcp-version"]; !ok {
			t.Errorf("config(%q) should still carry the version keys", name)
		}
	}

	// No name means "my own", which is what existing plugins rely on.
	if getConfig("")["options"] == nil {
		t.Error("config(\"\") should return the caller's own config")
	}
}
