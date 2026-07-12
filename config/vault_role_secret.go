package config

import (
	"os"
)

// resolveVaultToken returns the Vault token to use, preferring VAULT_TOKEN
// from the environment over the vault_token config field. This avoids
// requiring a plaintext secret in config.yaml just to bootstrap fetching
// other secrets.
func resolveVaultRoleID(cfg Config) string {
	if envVaultRoleID := os.Getenv("VAULT_ROLE_ID"); envVaultRoleID != "" {
		return envVaultRoleID
	}
	return cfg.VaultRoleID
}

func resolveVaultSecretID(cfg Config) string {
	if envVaultSecretID := os.Getenv("VAULT_SECRET_ID"); envVaultSecretID != "" {
		return envVaultSecretID
	}
	return cfg.VaultSecretID
}