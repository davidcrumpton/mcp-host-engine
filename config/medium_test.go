package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// M3 -- sensitive names are recognised by substring, not by an exact list
// ---------------------------------------------------------------------------

func TestIsSensitiveName(t *testing.T) {
	sensitive := []string{
		"authorization", "Authorization", "cookie", "Set-Cookie", "token",
		"password", "X-Api-Key", "api-key", "apikey", "PRIVATE-TOKEN",
		"proxy-authorization", "x-auth-token", "secret", "client_secret",
		"access_key", "credential", "session", "Mcp-Session-Id", "signature",
	}
	for _, name := range sensitive {
		if !IsSensitiveName(name) {
			t.Errorf("%q should be treated as sensitive", name)
		}
	}

	plain := []string{"Accept", "Content-Type", "User-Agent", "Host", "X-Request-Id"}
	for _, name := range plain {
		if IsSensitiveName(name) {
			t.Errorf("%q should not be masked", name)
		}
	}
}

func TestMaskKeyValueWidensBeyondTheOldFourNames(t *testing.T) {
	cfg := Config{}
	// The old implementation masked exactly authorization, cookie, token and
	// password, so these were printed in clear at level 4.
	for _, key := range []string{"X-Api-Key", "PRIVATE-TOKEN", "x-auth-token", "set-cookie"} {
		if got := cfg.MaskKeyValue(key, "hunter2"); got != "***" {
			t.Errorf("MaskKeyValue(%q) = %v, want ***", key, got)
		}
	}
	if got := cfg.MaskKeyValue("Accept", "application/json"); got != "application/json" {
		t.Errorf("a non-sensitive header was masked: %v", got)
	}
}

// ---------------------------------------------------------------------------
// M11 -- http_timeout_seconds is validated, not passed through
// ---------------------------------------------------------------------------

func TestHTTPTimeoutSecondsFor(t *testing.T) {
	cases := []struct {
		name    string
		global  int
		plugin  interface{}
		want    int
		hasPlgn bool
	}{
		{name: "plugin value wins", global: 60, plugin: 30, want: 30, hasPlgn: true},
		{name: "plugin zero means use the global value", global: 60, plugin: 0, want: 60, hasPlgn: true},
		{name: "plugin negative falls back to the global value", global: 60, plugin: -1, want: 60, hasPlgn: true},
		{name: "plugin absurd value falls back to the global value", global: 60, plugin: 1 << 20, want: 60, hasPlgn: true},
		{name: "plugin float", global: 60, plugin: 45.0, want: 45, hasPlgn: true},
		{name: "global zero means the default", global: 0, want: 15},
		{name: "global negative means the default", global: -5, want: 15},
		{name: "nothing configured means the default", global: 0, want: 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{HTTPClientTimeoutSeconds: tc.global}
			if tc.hasPlgn {
				cfg.Plugins = map[string]map[string]interface{}{
					"p": {"http_timeout_seconds": tc.plugin},
				}
			}
			if got := cfg.HTTPTimeoutSecondsFor("p"); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// M10 -- a plugin entry cannot read the daemon's own config
// ---------------------------------------------------------------------------

func TestPluginConfigCannotPointAtTheDaemonConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	body := "plugins:\n  leaky:\n    config: \"file://" + configPath + "\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadConfigBytes(configPath, data)
	if err == nil {
		t.Fatal("a plugin config: pointing at the daemon's own config must be refused")
	}
	if !strings.Contains(err.Error(), "own config file") {
		t.Errorf("error should explain the refusal: %v", err)
	}
}

func TestPluginConfigMayReadAnotherFile(t *testing.T) {
	dir := t.TempDir()
	optionsPath := filepath.Join(dir, "plugin-options.yaml")
	if err := os.WriteFile(optionsPath, []byte("api_url: https://example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	body := "plugins:\n  fine:\n    config: \"file://" + optionsPath + "\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigBytes(configPath, data)
	if err != nil {
		t.Fatalf("a plugin pointing at its own options file should be fine: %v", err)
	}
	if got, _ := cfg.Plugins["fine"]["config"].(string); !strings.Contains(got, "example.com") {
		t.Errorf("the options file was not resolved: %q", got)
	}
}
