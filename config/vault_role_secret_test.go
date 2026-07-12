// config/vault_role_secret_test.go
package config

import (
	"os"
	"testing"
)

func TestResolveVaultRoleID_EnvTakesPrecedence(t *testing.T) {
	os.Setenv("VAULT_ROLE_ID", "env-roleid")
	defer os.Unsetenv("VAULT_ROLE_ID")

	cfg := Config{VaultRoleID: "yaml-roleid"}
	got := resolveVaultRoleID(cfg)
	if got != "env-roleid" {
		t.Errorf("got %q, want %q (env should win)", got, "env-roleid")
	}
}

func TestResolveVaultRoleID_FallsBackToYAML(t *testing.T) {
	os.Unsetenv("VAULT_ROLE_ID") // ensure clean state
	cfg := Config{VaultRoleID: "yaml-roleid"}
	got := resolveVaultRoleID(cfg)
	if got != "yaml-roleid" {
		t.Errorf("got %q, want %q", got, "yaml-roleid")
	}
}

func TestResolveVaultRoleID_EmptyWhenNeitherSet(t *testing.T) {
	os.Unsetenv("VAULT_ROLE_ID")
	cfg := Config{}
	got := resolveVaultRoleID(cfg)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}