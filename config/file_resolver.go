// config/file_resolver.go
package config

import (
	"fmt"
	"io"
	"os"
)

// maxResolvedFileBytes bounds a file:// reference. Config values are resolved
// before anything else runs, so "token_secret: file:///dev/zero" was a startup
// hang with an unbounded allocation behind it. 1 MiB is far more than any
// credential or options file a config value should name.
const maxResolvedFileBytes = 1 << 20

// FileResolver resolves file:// references against the process environment.
// This reads in the whole file.  It is up to the caller to ensure that
// the file does not contain any sensitive information.
type FileResolver struct{}

func (FileResolver) Resolve(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("file:// reference missing file path")
	}
	f, err := os.Open(ref)
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", ref, err)
	}
	defer f.Close()

	content, err := io.ReadAll(io.LimitReader(f, maxResolvedFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", ref, err)
	}
	if len(content) > maxResolvedFileBytes {
		return "", fmt.Errorf("file %q exceeds the %d byte limit for a file:// reference", ref, maxResolvedFileBytes)
	}
	return string(content), nil
}
