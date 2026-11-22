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
	c, err := newVaultClient(addr)
	if err != nil {
		return nil, err
	}
	token, err := c.Logical().Write("auth/"+approle+"/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("vault %s login: %w", approle, err)
	}
	// A login that answers 204, or with a body that has no auth block, used to
	// nil-dereference here and take the process down before it had logged
	// anything. Treat it as the failed login it is.
	if token == nil || token.Auth == nil {
		return nil, fmt.Errorf("vault %s login: no auth data in response (check the role_id/secret_id and that the approle auth method is enabled)", approle)
	}
	if token.Auth.ClientToken == "" {
		return nil, fmt.Errorf("vault %s login: response carried an empty client token", approle)
	}
	c.SetToken(token.Auth.ClientToken)
	return &VaultClientAppRole{client: c}, nil
}

func (r *VaultClientAppRole) ReadSecret(path, key string) (string, error) {
	secret, err := r.client.Logical().Read(path)
	if err != nil {
		return "", fmt.Errorf("vault read %q: %w", path, err)
	}
	return secretValue(secret, path, key)
}
