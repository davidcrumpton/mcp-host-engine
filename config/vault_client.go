// config/vault_client.go
package config

import "fmt"

// VaultClient abstracts secret retrieval so config package doesn't force
// a hashicorp/vault/api dependency onto everyone who never uses vault://.
// Keep this interface minimal — path+key in, string out, error on failure.
type VaultClient interface {
	// ReadSecret fetches a single key from a secret at path.
	// path is the full Vault path (e.g. "secret/data/db"), key is the
	// field name within that secret's data (e.g. "password").
	ReadSecret(path, key string) (string, error)
}

// noopVaultClient is the zero-value client used when vault is not
// configured. It fails loudly rather than silently passing through
// vault:// literals — see LoadConfig's fail-closed requirement.
type noopVaultClient struct{}

func (noopVaultClient) ReadSecret(path, key string) (string, error) {
	return "", fmt.Errorf("vault:// reference used but no vault_addr configured")
}