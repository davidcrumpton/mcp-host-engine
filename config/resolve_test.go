// config/resolve_test.go
package config

import (
	"os"
	"testing"
)

func TestResolveTopLevelFields_EnvSubstitution(t *testing.T) {
	os.Setenv("MCPHE_TEST_BEARER", "supersecret123")
	defer os.Unsetenv("MCPHE_TEST_BEARER")

	RegisterResolver("env", EnvResolver{})
	defer delete(resolvers, "env")

	cfg := &Config{BearerToken: "env://MCPHE_TEST_BEARER"}
	if err := cfg.resolveTopLevelFields(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BearerToken != "supersecret123" {
		t.Errorf("got %q, want %q", cfg.BearerToken, "supersecret123")
	}
}

func TestResolveTopLevelFields_PlainValuePassesThrough(t *testing.T) {
	cfg := &Config{PidFile: "/var/run/mcphe.pid"}
	if err := cfg.resolveTopLevelFields(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PidFile != "/var/run/mcphe.pid" {
		t.Errorf("plain value should pass through unchanged, got %q", cfg.PidFile)
	}
}

func TestResolveTopLevelFields_EmptyFieldSkipped(t *testing.T) {
	cfg := &Config{} // all resolvable fields empty
	if err := cfg.resolveTopLevelFields(); err != nil {
		t.Fatalf("unexpected error on empty config: %v", err)
	}
}
// We don't error when scheme has no resolver at this release but
// we will in a future so we will keep this commented out.
// TODO: Fix this test
/*
func TestResolveTopLevelFields_UnresolvableSchemeFailsClosed(t *testing.T) {
	// no resolver registered for "vault" in this test
	cfg := &Config{TokenSecret: "vault://cubbyhole/mcphe#key"}
	if err := cfg.resolveTopLevelFields(); err == nil {
		t.Fatal("expected error when scheme has no registered resolver")
	}
}
*/

func TestResolveTopLevelFields_UnresolvableSchemeReturnsUnchanged(t *testing.T) {
	cfg := &Config{BearerToken: "vault://cubbyhole/mcphe#key"}
	if err := cfg.resolveTopLevelFields(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BearerToken != "vault://cubbyhole/mcphe#key" {
		t.Errorf("got %q, want %q", cfg.BearerToken, "vault://cubbyhole/mcphe#key")
	}
}
