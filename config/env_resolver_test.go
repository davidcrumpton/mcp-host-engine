// config/env_resolver_test.go
package config

import (
	"os"
	"testing"
)

func TestEnvResolver_Success(t *testing.T) {
	os.Setenv("MCPHE_TEST_VAR", "hello")
	defer os.Unsetenv("MCPHE_TEST_VAR")

	r := EnvResolver{}
	got, err := r.Resolve("MCPHE_TEST_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestEnvResolver_NotSet(t *testing.T) {
	os.Unsetenv("MCPHE_DEFINITELY_NOT_SET")
	r := EnvResolver{}
	_, err := r.Resolve("MCPHE_DEFINITELY_NOT_SET")
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
}

func TestEnvResolver_SetButEmpty(t *testing.T) {
	os.Setenv("MCPHE_EMPTY_VAR", "")
	defer os.Unsetenv("MCPHE_EMPTY_VAR")

	r := EnvResolver{}
	got, err := r.Resolve("MCPHE_EMPTY_VAR")
	if err != nil {
		t.Fatalf("unexpected error for set-but-empty var: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestEnvResolver_EmptyRef(t *testing.T) {
	r := EnvResolver{}
	_, err := r.Resolve("")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}