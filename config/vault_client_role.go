// config/vault_client_role.go
// uses role and secretid
package config

import (
	"fmt"

	vaultapi "github.com/hashicorp/vault/api"
)

type VaultClientAppRole struct {
	client *vaultapi.Client
}

func NewVaultClientAppRole(addr, roleID, secretID string, approle string) (VaultClient, error) {
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr
	c, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating vault client: %w", err)
	}
	token, err := c.Logical().Write("auth/" + approle + "/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("vault %s login: %w", approle, err)
	}
	c.SetToken(token.Auth.ClientToken)
	return &VaultClientAppRole{client: c}, nil
}

func (r *VaultClientAppRole) ReadSecret(path, key string) (string, error) {
	secret, err := r.client.Logical().Read(path)
	if err != nil {
		return "", fmt.Errorf("vault read %q: %w", path, err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("vault path %q: no data returned", path)
	}

	// KV v2 nests actual fields under "data"; KV v1 does not. Handle both
	// so admins don't have to know which engine version they're on.
	data := secret.Data
	if nested, ok := secret.Data["data"].(map[string]interface{}); ok {
		data = nested
	}

	val, ok := data[key]
	if !ok {
		return "", fmt.Errorf("vault path %q: key %q not found", path, key)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("vault path %q key %q: value is not a string (%T)", path, key, val)
	}
	return str, nil
}