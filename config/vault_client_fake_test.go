// config/vault_client_fake_test.go
package config

import "fmt"

// fakeVaultClient is a test double. Populate secrets before use; ReadSecret
// returns exactly what's configured, or an error if the key is missing —
// same failure shape as realVaultClient so resolver tests exercise both paths.
type fakeVaultClient struct {
	secrets map[string]map[string]string // path -> key -> value
	err     error                        // if set, ReadSecret always returns this error
}

func (f *fakeVaultClient) ReadSecret(path, key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	keys, ok := f.secrets[path]
	if !ok {
		return "", fmt.Errorf("fakeVaultClient: no secret at path %q", path)
	}
	val, ok := keys[key]
	if !ok {
		return "", fmt.Errorf("fakeVaultClient: no key %q at path %q", key, path)
	}
	return val, nil
}