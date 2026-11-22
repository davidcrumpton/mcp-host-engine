package config

import (
	"os"
	"path/filepath"
	"testing"

	vaultapi "github.com/hashicorp/vault/api"
)

// L6: `token_secret: file:///etc/mcphe/secret` used to include the file's
// trailing newline, so the secret that was configured was never the secret that
// was used -- invisibly, since the value is not logged.
func TestFileResolverStripsTrailingNewlines(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ content, want string }{
		{"secret\n", "secret"},
		{"secret\r\n", "secret"},
		{"secret", "secret"},
		{"secret\n\n", "secret"},
		{"pass phrase with spaces\n", "pass phrase with spaces"},
		// Only *trailing* newlines: an embedded one is data.
		{"line1\nline2\n", "line1\nline2"},
	} {
		path := filepath.Join(dir, "value")
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		got, err := FileResolver{}.Resolve(path)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.content, err)
		}
		if got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.content, got, tc.want)
		}
	}
}

// L8: vault_addr had no scheme check and no client timeout. Over http:// the
// token travels in clear text; https:// is always fine, http:// only on loopback.
func TestValidateVaultAddr(t *testing.T) {
	ok := []string{
		"https://vault.example.com:8200",
		"https://127.0.0.1:8200",
		"http://127.0.0.1:8200",
		"http://localhost:8200",
		"http://[::1]:8200",
	}
	for _, addr := range ok {
		if err := validateVaultAddr(addr); err != nil {
			t.Errorf("validateVaultAddr(%q): unexpected error: %v", addr, err)
		}
	}

	bad := []string{
		"http://vault.example.com:8200", // token in clear text over the network
		"http://10.0.0.5:8200",
		"vault.example.com:8200", // no scheme
		"ftp://vault.example.com",
		"://nonsense",
	}
	for _, addr := range bad {
		if err := validateVaultAddr(addr); err == nil {
			t.Errorf("validateVaultAddr(%q) was accepted", addr)
		}
	}
}

// The client itself must carry a timeout: an unreachable vault_addr used to hang
// startup forever with no message.
func TestNewVaultClientSetsATimeout(t *testing.T) {
	c, err := newVaultClient("https://127.0.0.1:8200")
	if err != nil {
		t.Fatalf("newVaultClient: %v", err)
	}
	if got := c.CloneConfig().Timeout; got != vaultClientTimeout {
		t.Errorf("timeout = %v, want %v", got, vaultClientTimeout)
	}
}

// L7: a 204 or empty login response left token.Auth nil, which panicked at
// startup. secretValue is the same guard on the read path.
func TestSecretValueHandlesMissingData(t *testing.T) {
	if _, err := secretValue(nil, "secret/data/x", "key"); err == nil {
		t.Error("a nil secret was accepted")
	}
	if _, err := secretValue(&vaultapi.Secret{}, "secret/data/x", "key"); err == nil {
		t.Error("a secret with no data was accepted")
	}

	// KV v1: fields at the top level.
	v1 := &vaultapi.Secret{Data: map[string]interface{}{"key": "value"}}
	if got, err := secretValue(v1, "secret/x", "key"); err != nil || got != "value" {
		t.Errorf("KV v1: got %q, %v; want \"value\", nil", got, err)
	}

	// KV v2: fields nested under "data".
	v2 := &vaultapi.Secret{Data: map[string]interface{}{
		"data": map[string]interface{}{"key": "nested"},
	}}
	if got, err := secretValue(v2, "secret/data/x", "key"); err != nil || got != "nested" {
		t.Errorf("KV v2: got %q, %v; want \"nested\", nil", got, err)
	}

	if _, err := secretValue(v1, "secret/x", "missing"); err == nil {
		t.Error("a missing key was accepted")
	}
	if _, err := secretValue(&vaultapi.Secret{Data: map[string]interface{}{"key": 1}}, "secret/x", "key"); err == nil {
		t.Error("a non-string value was accepted")
	}
}

// L5: two configs must not share the vault client. Loading a second config used
// to inherit the first one's client (and credentials) from a package global.
func TestLoadConfigBytesGivesEachConfigItsOwnResolvers(t *testing.T) {
	first, err := LoadConfigBytes("first.yaml", []byte("transport: http\n"))
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if first.resolvers == nil {
		t.Fatal("LoadConfigBytes left the resolver set nil")
	}
	// Builtins are present; no vault scheme without a vault_addr.
	if _, ok := first.resolvers["env"]; !ok {
		t.Error("env:// resolver missing")
	}
	if _, ok := first.resolvers["vault"]; ok {
		t.Error("vault:// resolver registered without a vault_addr")
	}

	second, err := LoadConfigBytes("second.yaml", []byte("transport: http\n"))
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	// Mutating one config's set must not be visible to the other.
	second.resolvers["vault"] = fakeResolver("second")
	if _, ok := first.resolvers["vault"]; ok {
		t.Error("configs share one resolver map")
	}
}
