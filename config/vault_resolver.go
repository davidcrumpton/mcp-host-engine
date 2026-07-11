// config/vault_resolver.go
package config

import (
	"fmt"
	"strings"
)

type VaultResolver struct {
	Client VaultClient // interface wrapping hashicorp/vault/api, injected so config pkg stays dependency-light
}

// ref looks like "secret/data/db#password"
func (v *VaultResolver) Resolve(ref string) (string, error) {
	path, key, ok := strings.Cut(ref, "#")
	if !ok {
		return "", fmt.Errorf("vault ref %q missing #key", ref)
	}
	return v.Client.ReadSecret(path, key)
}