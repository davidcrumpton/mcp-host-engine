// config/vault_token.go
package config

import "os"

// resolveVaultToken returns the Vault token to use, preferring VAULT_TOKEN
// from the environment over the vault_token config field. This avoids
// requiring a plaintext secret in config.yaml just to bootstrap fetching
// other secrets.
func resolveVaultToken(cfg Config) string {
	if envToken := os.Getenv("VAULT_TOKEN"); envToken != "" {
		return envToken
	}
	return cfg.VaultToken
}