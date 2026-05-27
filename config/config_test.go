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
	// Non-existent path → returns DefaultConfig unchanged.
	cfg, _ := LoadConfig(filepath.Join(t.TempDir(), "does_not_exist.yaml"))
	if cfg.Port != DefaultConfig.Port {
		t.Errorf("port: got %q, want %q", cfg.Port, DefaultConfig.Port)
	}
	if cfg.Host != DefaultConfig.Host {
		t.Errorf("host: got %q, want %q", cfg.Host, DefaultConfig.Host)
	}
	if cfg.PluginDir != DefaultConfig.PluginDir {
		t.Errorf("plugin_dir: got %q, want %q", cfg.PluginDir, DefaultConfig.PluginDir)
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
	// Should not panic; returns DefaultConfig.
	cfg, _ := LoadConfig(path)
	if cfg.Port != DefaultConfig.Port {
		t.Errorf("port: got %q, want %q", cfg.Port, DefaultConfig.Port)
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
