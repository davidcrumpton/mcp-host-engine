package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// ---------------------------------------------------------------------------
// LoadConfig
// ---------------------------------------------------------------------------

func TestLoadConfig_Defaults(t *testing.T) {
	// Non-existent path → returns empty values
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does_not_exist.yaml"))
	if err == nil {
		t.Errorf("error loading config: %v", err)
	}
	if cfg.Port != "" {
		t.Errorf("port: got %q, want %q", cfg.Port, "")
	}
	if cfg.Host != "" {
		t.Errorf("host: got %q, want %q", cfg.Host, "")
	}
	if cfg.PluginDir != "" {
		t.Errorf("plugin_dir: got %q, want %q", cfg.PluginDir, "")
	}
}

func TestLoadConfig_OverridesDefaults(t *testing.T) {
	yaml := `
port: "9090"
host: "0.0.0.0"
plugin_dir: "myplugins"
verbosity_level: 3
`
	path := writeYAML(t, yaml)
	cfg, _ := LoadConfig(path)

	if cfg.Port != "9090" {
		t.Errorf("port: got %q, want 9090", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("host: got %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.PluginDir != "myplugins" {
		t.Errorf("plugin_dir: got %q, want myplugins", cfg.PluginDir)
	}
	if cfg.Verbosity != 3 {
		t.Errorf("verbosity: got %d, want 3", cfg.Verbosity)
	}
}

func TestLoadConfig_EmptyPluginDirFallsBackToDefault(t *testing.T) {
	yaml := `plugin_dir: ""`
	path := writeYAML(t, yaml)
	cfg, _ := LoadConfig(path)
	if cfg.PluginDir != DefaultConfig.PluginDir {
		t.Errorf("plugin_dir: got %q, want %q", cfg.PluginDir, DefaultConfig.PluginDir)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := writeYAML(t, "{ bad yaml: [")
	// Should not panic; returns empty config.
	cfg, _ := LoadConfig(path)
	if cfg.Port != "" {
		t.Errorf("port: got %q, want empty", cfg.Port)
	}
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

func TestLoadConfig_TransportDefaultsToHTTP(t *testing.T) {
	// A config with no transport field should default to "http".
	path := writeYAML(t, `port: "9090"`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport != TransportHTTP {
		t.Errorf("transport: got %q, want %q", cfg.Transport, TransportHTTP)
	}
}

func TestLoadConfig_TransportEmptyFallsBackToHTTP(t *testing.T) {
	path := writeYAML(t, `transport: ""`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport != TransportHTTP {
		t.Errorf("transport: got %q, want %q", cfg.Transport, TransportHTTP)
	}
}

func TestLoadConfig_TransportStdio(t *testing.T) {
	path := writeYAML(t, `transport: "stdio"`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport != TransportStdio {
		t.Errorf("transport: got %q, want %q", cfg.Transport, TransportStdio)
	}
}

func TestLoadConfig_TransportHTTPExplicit(t *testing.T) {
	path := writeYAML(t, `transport: "http"`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport != TransportHTTP {
		t.Errorf("transport: got %q, want %q", cfg.Transport, TransportHTTP)
	}
}

func TestLoadConfig_TransportInvalidReturnsError(t *testing.T) {
	path := writeYAML(t, `transport: "websocket"`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid transport value")
	}
}

// ---------------------------------------------------------------------------
// IsToolEnabled
// ---------------------------------------------------------------------------

func TestIsToolEnabled(t *testing.T) {
	cfg := Config{
		Tools: map[string]bool{
			"alpha": true,
			"beta":  false,
		},
	}
	if !cfg.IsToolEnabled("alpha") {
		t.Error("alpha should be enabled")
	}
	if cfg.IsToolEnabled("beta") {
		t.Error("beta should be disabled")
	}
	if cfg.IsToolEnabled("gamma") {
		t.Error("unknown tool should not be enabled")
	}
}

func TestIsToolEnabled_NilTools(t *testing.T) {
	cfg := Config{}
	if cfg.IsToolEnabled("anything") {
		t.Error("nil tools map should report disabled")
	}
}

func TestIsToolEnabled_PluginPresent(t *testing.T) {
	cfg := Config{
		Plugins: map[string]map[string]interface{}{
			"wikipedia_search": {"allowed_domains": []string{"en.wikipedia.org"}},
		},
	}
	if !cfg.IsToolEnabled("wikipedia_search") {
		t.Error("plugin present in plugins map without 'enabled' key should be enabled")
	}
}

func TestIsToolEnabled_PluginExplicitTrue(t *testing.T) {
	cfg := Config{
		Plugins: map[string]map[string]interface{}{
			"get_ip": {"enabled": true, "allowed_domains": []string{"ifconfig.io"}},
		},
	}
	if !cfg.IsToolEnabled("get_ip") {
		t.Error("plugin with enabled: true should be enabled")
	}
}

func TestIsToolEnabled_PluginExplicitFalse(t *testing.T) {
	cfg := Config{
		Plugins: map[string]map[string]interface{}{
			"run_command": {"enabled": false, "allowed_commands": []string{"ls"}},
		},
	}
	if cfg.IsToolEnabled("run_command") {
		t.Error("plugin with enabled: false should be disabled")
	}
}

func TestIsToolEnabled_PluginAbsentAndNoToolsMap(t *testing.T) {
	cfg := Config{}
	if cfg.IsToolEnabled("nonexistent") {
		t.Error("plugin absent from both maps should be disabled")
	}
}

func TestIsToolEnabled_LegacyToolsMapStillWorks(t *testing.T) {
	cfg := Config{
		Tools: map[string]bool{"ping": true, "old_tool": false},
	}
	if !cfg.IsToolEnabled("ping") {
		t.Error("legacy tools map: ping should be enabled")
	}
	if cfg.IsToolEnabled("old_tool") {
		t.Error("legacy tools map: old_tool should be disabled")
	}
}

func TestIsToolEnabled_PluginMapTakesPrecedenceOverToolsMap(t *testing.T) {
	// plugins map wins even if tools map says otherwise
	cfg := Config{
		Tools: map[string]bool{"wikipedia_search": false},
		Plugins: map[string]map[string]interface{}{
			"wikipedia_search": {"allowed_domains": []string{"en.wikipedia.org"}},
		},
	}
	if !cfg.IsToolEnabled("wikipedia_search") {
		t.Error("plugins map presence should take precedence over tools map")
	}
}

// ---------------------------------------------------------------------------
// Verbose / Logf
// ---------------------------------------------------------------------------

func TestVerbose(t *testing.T) {
	cfg := Config{Verbosity: 2}
	if !cfg.Verbose(1) {
		t.Error("level 1 should be verbose at verbosity 2")
	}
	if !cfg.Verbose(2) {
		t.Error("level 2 should be verbose at verbosity 2")
	}
	if cfg.Verbose(3) {
		t.Error("level 3 should NOT be verbose at verbosity 2")
	}
}

// Logf must not panic at any verbosity level.
func TestLogf_NoPanic(t *testing.T) {
	cfg := Config{Verbosity: 5}
	cfg.Logf(1, "hello %s", "world")
}

// ---------------------------------------------------------------------------
// AllowedReadFilePathsFor
// ---------------------------------------------------------------------------

func pluginCfg(key string, value interface{}) Config {
	return Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {key: value},
		},
	}
}

func TestAllowedReadFilePathsFor_StringSlice(t *testing.T) {
	cfg := pluginCfg("allowed_read_file_paths", []string{"/tmp", "/var"})
	paths := cfg.AllowedReadFilePathsFor("myplugin")
	if len(paths) != 2 || paths[0] != "/tmp" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestAllowedReadFilePathsFor_InterfaceSlice(t *testing.T) {
	cfg := pluginCfg("allowed_read_file_paths", []interface{}{"/tmp", "/var"})
	paths := cfg.AllowedReadFilePathsFor("myplugin")
	if len(paths) != 2 || paths[1] != "/var" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestAllowedReadFilePathsFor_MissingPlugin(t *testing.T) {
	cfg := Config{}
	if got := cfg.AllowedReadFilePathsFor("nope"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAllowedReadFilePathsFor_MissingKey(t *testing.T) {
	cfg := Config{Plugins: map[string]map[string]interface{}{"myplugin": {}}}
	if got := cfg.AllowedReadFilePathsFor("myplugin"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// AllowedWriteFilePathsFor
// ---------------------------------------------------------------------------

func TestAllowedWriteFilePathsFor_StringSlice(t *testing.T) {
	cfg := pluginCfg("allowed_write_file_paths", []string{"/out"})
	paths := cfg.AllowedWriteFilePathsFor("myplugin")
	if len(paths) != 1 || paths[0] != "/out" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestAllowedWriteFilePathsFor_InterfaceSlice(t *testing.T) {
	cfg := pluginCfg("allowed_write_file_paths", []interface{}{"/out", "/tmp"})
	paths := cfg.AllowedWriteFilePathsFor("myplugin")
	if len(paths) != 2 {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestAllowedWriteFilePathsFor_MissingPlugin(t *testing.T) {
	cfg := Config{}
	if got := cfg.AllowedWriteFilePathsFor("x"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// AllowedDomainsFor
// ---------------------------------------------------------------------------

func TestAllowedDomainsFor_StringSlice(t *testing.T) {
	cfg := pluginCfg("allowed_domains", []string{"example.com"})
	domains := cfg.AllowedDomainsFor("myplugin")
	if len(domains) != 1 || domains[0] != "example.com" {
		t.Errorf("unexpected domains: %v", domains)
	}
}

func TestAllowedDomainsFor_InterfaceSlice(t *testing.T) {
	cfg := pluginCfg("allowed_domains", []interface{}{"a.com", "b.org"})
	domains := cfg.AllowedDomainsFor("myplugin")
	if len(domains) != 2 {
		t.Errorf("unexpected domains: %v", domains)
	}
}

func TestAllowedDomainsFor_MissingPlugin(t *testing.T) {
	cfg := Config{}
	if got := cfg.AllowedDomainsFor("nope"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// AllowedRunCommandsFor
// ---------------------------------------------------------------------------

func TestAllowedRunCommandsFor_StringSlice(t *testing.T) {
	cfg := pluginCfg("allowed_commands", []string{"echo", "ls"})
	cmds := cfg.AllowedRunCommandsFor("myplugin")
	if len(cmds) != 2 || cmds[0] != "echo" {
		t.Errorf("unexpected cmds: %v", cmds)
	}
}

func TestAllowedRunCommandsFor_InterfaceSlice(t *testing.T) {
	cfg := pluginCfg("allowed_commands", []interface{}{"echo", "ls"})
	cmds := cfg.AllowedRunCommandsFor("myplugin")
	if len(cmds) != 2 {
		t.Errorf("unexpected cmds: %v", cmds)
	}
}

func TestAllowedRunCommandsFor_MissingPlugin(t *testing.T) {
	cfg := Config{}
	if got := cfg.AllowedRunCommandsFor("nope"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// AllowedENVsFor
// ---------------------------------------------------------------------------

func TestAllowedENVsFor_StringSlice(t *testing.T) {
	cfg := pluginCfg("allowed_env_vars", []string{"HOME", "PATH"})
	envs := cfg.AllowedENVsFor("myplugin")
	if len(envs) != 2 || envs[0] != "HOME" {
		t.Errorf("unexpected envs: %v", envs)
	}
}

func TestAllowedENVsFor_InterfaceSlice(t *testing.T) {
	cfg := pluginCfg("allowed_env_vars", []interface{}{"HOME"})
	envs := cfg.AllowedENVsFor("myplugin")
	if len(envs) != 1 || envs[0] != "HOME" {
		t.Errorf("unexpected envs: %v", envs)
	}
}

func TestAllowedENVsFor_MissingPlugin(t *testing.T) {
	cfg := Config{}
	if got := cfg.AllowedENVsFor("nope"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestMaskKeyValue(t *testing.T) {
	// Sensitive keys matching (case-insensitive), key prefix matching, full value matching, no matching
	// Sensitive keys are hardcoded as of this release
	// But should be made variable in config file.
	// cfg := Config{
	// 	SensitiveKeys: []string{"password", "token", "secret"},
	// }

	// Masker is simple and only returns *** for now
	cfg := DefaultConfig

	tests := []struct {
		key      string
		value    interface{}
		expected string
	}{
		{"password", "secret123", "***"},
		{"token", "tkn_abc123", "***"},
		{"PASSWORD", "secret123", "***"},
		{"Authorization", "Bearer tkn_abc123", "***"},
	}

	for _, tt := range tests {
		actual := cfg.MaskKeyValue(tt.key, tt.value)
		if actual != tt.expected {
			t.Errorf("key=%s, value=%v: expected %q, got %q", tt.key, tt.value, tt.expected, actual)
		}
	}
}

func TestMaskForNotDefinedValues(t *testing.T) {
	cfg := DefaultConfig

	tests := []struct {
		key      string
		value    interface{}
		expected string
	}{
		{"other", "value", "value"},
		{"", "value", "value"},
		{"NotASen", "value", "value"},
	}

	for _, tt := range tests {
		actual := cfg.MaskKeyValue(tt.key, tt.value)
		if actual != tt.expected {
			t.Errorf("key=%s, value=%v: expected %q, got %q", tt.key, tt.value, tt.expected, actual)
		}
	}
}

func TestAllowedHTTPMethodsFor_StringSlice(t *testing.T) {
	cfg := pluginCfg("allowed_http_methods", []string{"GET", "POST"})
	methods := cfg.AllowedHTTPMethodsFor("myplugin")
	if len(methods) != 2 || methods[0] != "GET" {
		t.Errorf("unexpected methods: %v", methods)
	}
}

func TestAllowedHTTPMethodsFor_InterfaceSlice(t *testing.T) {
	cfg := pluginCfg("allowed_http_methods", []interface{}{"GET", "POST"})
	methods := cfg.AllowedHTTPMethodsFor("myplugin")
	if len(methods) != 2 || methods[0] != "GET" {
		t.Errorf("unexpected methods: %v", methods)
	}
}

func TestAllowedHTTPMethodsFor_MissingPlugin(t *testing.T) {
	cfg := Config{}
	if got := cfg.AllowedHTTPMethodsFor("nope"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAllowedHTTPMethodsFor_MissingKey(t *testing.T) {
	cfg := Config{Plugins: map[string]map[string]interface{}{"myplugin": {}}}
	if got := cfg.AllowedHTTPMethodsFor("myplugin"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}