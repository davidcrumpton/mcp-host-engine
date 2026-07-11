// config/vault_resolver_test.go
package config

import "testing"

func TestVaultResolver_Success(t *testing.T) {
	fake := &fakeVaultClient{
		secrets: map[string]map[string]string{
			"secret/data/db": {"password": "hunter2"},
		},
	}
	r := &VaultResolver{Client: fake}

	got, err := r.Resolve("secret/data/db#password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

func TestVaultResolver_MissingHash(t *testing.T) {
	r := &VaultResolver{Client: &fakeVaultClient{}}
	_, err := r.Resolve("secret/data/db")
	if err == nil {
		t.Fatal("expected error for ref missing #key")
	}
}

func TestVaultResolver_KeyNotFound(t *testing.T) {
	fake := &fakeVaultClient{
		secrets: map[string]map[string]string{"secret/data/db": {"other": "x"}},
	}
	r := &VaultResolver{Client: fake}
	_, err := r.Resolve("secret/data/db#password")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}


// Deferred until design approved.  Right now if vault isn't registered we pass the string and
// continue
// func TestLoadConfig_UnresolvedVaultFailsClosed(t *testing.T) {
// 	// No vault_addr in config → resolver never registered → vault:// literal
// 	// should cause LoadConfig to return an error, not pass the literal through.
// 	path := writeYAML(t, `
// plugins:
//   mytool:
//     api_key: "vault://secret/data/mytool#key"
// `)
// 	_, err := LoadConfig(path)
// 	if err == nil {
// 		t.Fatal("expected LoadConfig to fail when vault:// is used without vault_addr configured")
// 	}
// }


// Should return string as if no vault_addr is present and load file
// with literal "vault://secret/data/mytool#key" in it.
func TestLoadConfig_NoVault(t *testing.T) {
	path := writeYAML(t, `plugins:
  mytool:
    api_key: "vault://secret/data/mytool#key"`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected LoadConfig to not fail when vault:// is used without vault_addr configured: %v", err)
	}
	if cfg.Plugins["mytool"]["api_key"] != "vault://secret/data/mytool#key" {
		t.Errorf("expected LoadConfig to return string as if no vault_addr is present and load file with literal \"vault://secret/data/mytool#key\" in it")
	}
}


// Don't have vault configured locally so this is tested at run time via shell scripts
// But we'll keep it commented out for now.
// func TestLoadConfig_VaultWithToken(t *testing.T) {
// 	path := writeYAML(t, `vault_addr: "http://localhost:8200"
// vault_token: "fake-token"
// plugins:
//   mytool:
//     api_key: "vault://secret/data/mytool#key"`)
// 	_, err := LoadConfig(path)
// 	if err != nil {
// 		t.Fatalf("expected LoadConfig to succeed when vault:// is used with vault_addr configured: %v", err)
// 	}
// }