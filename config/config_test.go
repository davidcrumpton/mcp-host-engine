package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
// PluginSource
// ---------------------------------------------------------------------------

func TestPluginSource(t *testing.T) {
	cfg := Config{
		Plugins: map[string]map[string]interface{}{
			"mcwsd-prod":  {"plugin": "mcwsd"},
			"plain":       {},
			"empty-alias": {"plugin": ""},
			"non-string":  {"plugin": 42},
		},
	}
	tests := map[string]string{
		"mcwsd-prod":  "mcwsd",
		"plain":       "plain",
		"empty-alias": "empty-alias",
		"non-string":  "non-string",
		"absent":      "absent",
	}
	for instance, want := range tests {
		if got := cfg.PluginSource(instance); got != want {
			t.Errorf("PluginSource(%q): got %q, want %q", instance, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// PluginChecksum
// ---------------------------------------------------------------------------

func TestPluginChecksum(t *testing.T) {
	cfg := Config{
		Plugins: map[string]map[string]interface{}{
			"direct":   {"checksum": "sha256:aa"},
			"declared": {"checksum": "sha256:bb"},
			"alias":    {"plugin": "declared"},
			"own":      {"plugin": "declared", "checksum": "sha256:cc"},
			"none":     {},
			"absent":   {},
			// A non-string value must be surfaced, not swallowed, so the plugin
			// package can report a precise error instead of loading unverified.
			"typed": {"checksum": 1234},
		},
	}

	tests := []struct {
		instance string
		want     interface{}
		wantOK   bool
	}{
		{instance: "direct", want: "sha256:aa", wantOK: true},
		{instance: "alias", want: "sha256:bb", wantOK: true}, // inherits its source
		{instance: "own", want: "sha256:cc", wantOK: true},   // its own wins
		{instance: "declared", want: "sha256:bb", wantOK: true},
		{instance: "none", wantOK: false},
		{instance: "typed", want: 1234, wantOK: true},
		{instance: "not-in-config", wantOK: false},
	}
	for _, tc := range tests {
		got, ok := cfg.PluginChecksum(tc.instance)
		if ok != tc.wantOK {
			t.Errorf("PluginChecksum(%q): ok = %v, want %v", tc.instance, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("PluginChecksum(%q): got %v, want %v", tc.instance, got, tc.want)
		}
	}
}

func TestLoadConfigPluginIntegrityFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
plugin_dir: plugins
require_plugin_checksums: true
plugin_checksum_manifest: checksums.txt
plugins:
  ping:
    checksum: "sha256:abc"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.RequirePluginChecksums {
		t.Error("require_plugin_checksums was not loaded")
	}
	if cfg.PluginChecksumManifest != "checksums.txt" {
		t.Errorf("plugin_checksum_manifest = %q", cfg.PluginChecksumManifest)
	}
	if got, ok := cfg.PluginChecksum("ping"); !ok || got != "sha256:abc" {
		t.Errorf("PluginChecksum(ping) = %v, %v", got, ok)
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

func TestSplitListValue(t *testing.T) {
	tests := []struct {
		input    string
		expected []interface{}
	}{
		{"", []interface{}{}},
		{"a", []interface{}{"a"}},
		{"a,b,c", []interface{}{"a", "b", "c"}},
		{" a , b , c ", []interface{}{"a", "b", "c"}},
		{"a,,b", []interface{}{"a", "b"}},
	}
	for _, tt := range tests {
		actual := splitListValue(tt.input)
		if !reflect.DeepEqual(actual, tt.expected) {
			t.Errorf("splitListValue(%q): expected %v, got %v", tt.input, tt.expected, actual)
		}
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

// ---------------------------------------------------------------------------
// Config integrity
// ---------------------------------------------------------------------------

func TestLoadConfig_RequireConfigChecksum(t *testing.T) {
	path := writeYAML(t, "require_config_checksum: true\nplugins:\n  ping:\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.RequireConfigChecksum {
		t.Error("require_config_checksum was not loaded")
	}

	// Absent means off: the gate is opt-in, so existing configs keep working.
	cfg, err = LoadConfig(writeYAML(t, "plugins:\n  ping:\n"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.RequireConfigChecksum {
		t.Error("require_config_checksum should default to false")
	}
}

// PeekRequireConfigChecksum reads one boolean out of bytes that have not been
// verified yet, so it must work without resolution and must not invent a value
// for a file it cannot parse.
func TestPeekRequireConfigChecksum(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
		wantErr bool
	}{
		{name: "true", content: "require_config_checksum: true\n", want: true},
		{name: "false", content: "require_config_checksum: false\n", want: false},
		{name: "absent", content: "port: \"8001\"\n", want: false},
		{name: "empty", content: "", want: false},
		// A reference in an unrelated field must not be resolved, so a broken
		// env:// here cannot stop the gate from reading the flag.
		{name: "unresolvable reference", content: "token_secret: \"env://NOPE_NOT_SET\"\nrequire_config_checksum: true\n", want: true},
		{name: "not yaml", content: "\tnot: [valid\n", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PeekRequireConfigChecksum([]byte(tc.content))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.content)
				}
				return
			}
			if err != nil {
				t.Fatalf("PeekRequireConfigChecksum: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// LoadConfigBytes must behave exactly like LoadConfig: it is what the checksum
// gate calls after verifying the bytes it read.
func TestLoadConfigBytes_MatchesLoadConfig(t *testing.T) {
	const content = "port: \"9999\"\nplugin_dir: /tmp/plugins\nplugins:\n  ping:\n    checksum: \"sha256:abc\"\n"
	path := writeYAML(t, content)

	fromPath, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	fromBytes, err := LoadConfigBytes(path, []byte(content))
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if !reflect.DeepEqual(fromPath, fromBytes) {
		t.Errorf("LoadConfigBytes differs from LoadConfig:\n%+v\n%+v", fromPath, fromBytes)
	}

	// A parse error names the file, since the gate may be reading a path the
	// operator passed explicitly.
	if _, err := LoadConfigBytes("/etc/mcphe/config.yaml", []byte("\tnot: [valid\n")); err == nil {
		t.Error("expected an error for malformed YAML")
	} else if !strings.Contains(err.Error(), "/etc/mcphe/config.yaml") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestResolveValue(t *testing.T) {
	t.Setenv("MCPHE_RESOLVE_TEST", "from-env")
	filePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(filePath, []byte("from-file"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "env", in: "env://MCPHE_RESOLVE_TEST", want: "from-env"},
		{name: "file", in: "file://" + filePath, want: "from-file"},
		{name: "plain value", in: "sha256:abc", want: "sha256:abc"},
		{name: "plain path", in: "/etc/mcphe/config.yaml", want: "/etc/mcphe/config.yaml"},
		{name: "empty", in: "", want: ""},
		{name: "unset env", in: "env://MCPHE_RESOLVE_TEST_UNSET", wantErr: true},
		{name: "missing file", in: "file:///nonexistent/mcphe/nope", wantErr: true},
		// vault:// needs a config, which is the thing being verified, so it is
		// deliberately unavailable here rather than silently unresolved.
		{name: "vault scheme", in: "vault://secret/x#y", want: "vault://secret/x#y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveValue(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveValue(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
