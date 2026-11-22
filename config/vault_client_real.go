// config/vault_client_real.go
package config

import (
	"fmt"

	vaultapi "github.com/hashicorp/vault/api"
)

// realVaultClient wraps the official Vault API client. Constructed only
// when vault_addr is present in config — see NewVaultResolverFromConfig.
type realVaultClient struct {
	client *vaultapi.Client
}

func NewRealVaultClient(addr, token string) (VaultClient, error) {
	c, err := newVaultClient(addr)
	if err != nil {
		return nil, err
	}
	if token != "" {
		c.SetToken(token)
	}
	return &realVaultClient{client: c}, nil
}

func (r *realVaultClient) ReadSecret(path, key string) (string, error) {
	secret, err := r.client.Logical().Read(path)
	if err != nil {
		return "", fmt.Errorf("vault read %q: %w", path, err)
	}
	return secretValue(secret, path, key)
}

// secretValue pulls key out of a Vault read response, handling both KV v1
// (fields at the top level) and KV v2 (fields nested under "data").
//
// A nil secret -- which is what a 404, a 204 or an empty body produces -- used
// to be dereferenced by the caller, panicking the process at startup.
func secretValue(secret *vaultapi.Secret, path, key string) (string, error) {
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("vault path %q: no data returned", path)
	}

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
