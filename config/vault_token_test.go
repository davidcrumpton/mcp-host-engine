// config/vault_token_test.go
package config

import (
	"os"
	"testing"
)

func TestResolveVaultToken_EnvTakesPrecedence(t *testing.T) {
	os.Setenv("VAULT_TOKEN", "env-token")
	defer os.Unsetenv("VAULT_TOKEN")

	cfg := Config{VaultToken: "yaml-token"}
	got := resolveVaultToken(cfg)
	if got != "env-token" {
		t.Errorf("got %q, want %q (env should win)", got, "env-token")
	}
}

func TestResolveVaultToken_FallsBackToYAML(t *testing.T) {
	os.Unsetenv("VAULT_TOKEN") // ensure clean state
	cfg := Config{VaultToken: "yaml-token"}
	got := resolveVaultToken(cfg)
	if got != "yaml-token" {
		t.Errorf("got %q, want %q", got, "yaml-token")
	}
}

func TestResolveVaultToken_EmptyWhenNeitherSet(t *testing.T) {
	os.Unsetenv("VAULT_TOKEN")
	cfg := Config{}
	got := resolveVaultToken(cfg)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}