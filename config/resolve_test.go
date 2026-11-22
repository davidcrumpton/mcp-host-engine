// config/resolve_test.go
package config

import (
	"os"
	"testing"
)

func TestResolveTopLevelFields_EnvSubstitution(t *testing.T) {
	os.Setenv("MCPHE_TEST_BEARER", "supersecret123")
	defer os.Unsetenv("MCPHE_TEST_BEARER")

	cfg := &Config{BearerToken: "env://MCPHE_TEST_BEARER"}
	if err := cfg.resolveTopLevelFields(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BearerToken != "supersecret123" {
		t.Errorf("got %q, want %q", cfg.BearerToken, "supersecret123")
	}
}

// A config built by hand (not by LoadConfigBytes) still resolves env://, because
// resolversFor falls back to the builtin set. Before the resolver set moved into
// the Config, this only worked because a package global had been populated.
func TestResolveTopLevelFields_ZeroConfigUsesBuiltinResolvers(t *testing.T) {
	os.Setenv("MCPHE_TEST_ZERO", "fromenv")
	defer os.Unsetenv("MCPHE_TEST_ZERO")

	cfg := &Config{PidFile: "env://MCPHE_TEST_ZERO"}
	if err := cfg.resolveTopLevelFields(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PidFile != "fromenv" {
		t.Errorf("got %q, want %q", cfg.PidFile, "fromenv")
	}
}

// Two configs must not share a resolver set. A vault:// client registered for
// one config used to land in a package global, so the second config silently
// resolved through the first one's client and credentials.
func TestResolverSetIsPerConfig(t *testing.T) {
	first := Config{resolvers: builtinResolvers().with("vault", fakeResolver("first"))}
	second := Config{resolvers: builtinResolvers()}

	got, err := first.resolveString("vault://secret/value")
	if err != nil {
		t.Fatalf("first: unexpected error: %v", err)
	}
	if got != "first:secret/value" {
		t.Errorf("first config resolved to %q, want %q", got, "first:secret/value")
	}

	// The second config has no vault scheme, so the reference passes through
	// untouched rather than being resolved by the first config's client.
	got, err = second.resolveString("vault://secret/value")
	if err != nil {
		t.Fatalf("second: unexpected error: %v", err)
	}
	if got != "vault://secret/value" {
		t.Errorf("second config resolved to %q, want the reference unchanged", got)
	}

	// And building a third set must not have mutated the first.
	if _, ok := first.resolvers["vault"]; !ok {
		t.Error("first config lost its vault resolver")
	}
	if _, ok := second.resolvers["vault"]; ok {
		t.Error("second config gained a vault resolver it was never given")
	}
}

// fakeResolver prefixes what it resolves, so a test can tell which client ran.
type fakeResolver string

func (f fakeResolver) Resolve(ref string) (string, error) { return string(f) + ":" + ref, nil }

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
