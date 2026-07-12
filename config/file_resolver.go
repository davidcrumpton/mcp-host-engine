// config/file_resolver.go
package config

import (
	"fmt"
	"os"
)

// FileResolver resolves file:// references against the process environment.
// This reads in the whole file.  It is up to the caller to ensure that
// the file does not contain any sensitive information.
type FileResolver struct{}

func (FileResolver) Resolve(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("file:// reference missing file path")
	}
	content, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", ref, err)
	}
	return string(content), nil
}