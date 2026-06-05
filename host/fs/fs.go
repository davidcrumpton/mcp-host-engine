// host/fs/fs.go
package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mcphe/config"
)

// isPathAllowed checks whether path falls under one of allowedPaths.
// It resolves symlinks (via filepath.EvalSymlinks) before comparing, preventing
// a symlink inside an allowed directory from pointing outside it.
//
// For paths that do not exist yet (e.g. write targets), we resolve symlinks on
// the parent directory and re-join the filename. This correctly handles systems
// like macOS where os.TempDir() returns a symlink (e.g. /var/folders → /private/var/folders)
// so that a non-existent child path still resolves to the same real root as the
// allowed directory.
func isPathAllowed(path string, allowedPaths []string) bool {
	resolved := resolvePath(path)
	if resolved == "" {
		return false
	}

	for _, allowed := range allowedPaths {
		resolvedAllowed := resolvePath(allowed)
		if resolvedAllowed == "" {
			continue
		}
		// Ensure the prefix match ends on a directory boundary to prevent
		// /allowed/foo matching /allowed/foobar.
		if resolved == resolvedAllowed || strings.HasPrefix(resolved, resolvedAllowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolvePath returns the real absolute path for p, resolving symlinks.
// If p does not exist, it resolves symlinks on the parent directory and
// re-join the base name, so that a non-existent child of a symlinked
// directory (e.g. macOS /var/folders) still compares correctly.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	// p doesn't exist yet — resolve the parent and re-join.
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	parent := filepath.Dir(abs)
	base := filepath.Base(abs)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		// Parent doesn't exist either; best effort with Abs.
		return abs
	}
	return filepath.Join(resolvedParent, base)
}

func ReadFile(path string, cfg config.Config, pluginName string) (string, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))

	if !allowed {
		return "", fmt.Errorf("access to file %q is not allowed", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func WriteFile(path string, content string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))

	if !allowed {
		return fmt.Errorf("writing to file %q is not allowed", path)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func ListFiles(path string, cfg config.Config, pluginName string) ([]string, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	if !allowed {
		return nil, fmt.Errorf("access to directory %q is not allowed", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	return files, nil
}

func DeleteFile(path string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("deleting file %q is not allowed", path)
	}
	return os.Remove(path)
}

func RenameFile(oldPath, newPath string, cfg config.Config, pluginName string) error {
	allowedOld := isPathAllowed(oldPath, cfg.AllowedWriteFilePathsFor(pluginName))
	allowedNew := isPathAllowed(newPath, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowedOld || !allowedNew {
		return fmt.Errorf("renaming file from %q to %q is not allowed", oldPath, newPath)
	}
	return os.Rename(oldPath, newPath)
}

func ReadStream(path string, cfg config.Config, pluginName string, options map[string]interface{}, callback func(chunk string) error) error {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))

	if !allowed {
		return fmt.Errorf("access to file %q is not allowed", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Default chunk size is 4KB
	chunkSize := 4096
	if options != nil {
		if size, ok := options["chunkSize"].(float64); ok {
			chunkSize = int(size)
		}
	}

	buffer := make([]byte, chunkSize)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if err := callback(string(buffer[:n])); err != nil {
			return err
		}

		if err == io.EOF {
			break
		}
	}

	return nil
}

func WriteStream(path string, cfg config.Config, pluginName string, options map[string]interface{}, callback func() (string, error)) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("writing to file %q is not allowed", path)
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	for {
		chunk, err := callback()
		if err != nil {
			return err
		}
		if chunk == "" {
			// End of stream
			break
		}

		// Write the chunk to file
		if _, err := file.WriteString(chunk); err != nil {
			return err
		}
	}

	return nil
}

func RmDir(path string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("removing directory %q is not allowed", path)
	}
	return os.RemoveAll(path)
}

func Mkdir(path string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("writing to directory %q is not allowed", path)
	}
	return os.MkdirAll(path, 0755)
}

func IsDir(path string, cfg config.Config, pluginName string) (bool, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	if !allowed {
		return false, fmt.Errorf("access to directory %q is not allowed", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func IsPathExisted(path string, cfg config.Config, pluginName string) (bool, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	if !allowed {
		return false, fmt.Errorf("access to file %q is not allowed", path)
	}
	_, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return true, nil
}


// isPathAllowed checks whether path falls under one of allowedPaths.
// It resolves symlinks (via filepath.EvalSymlinks) before comparing, preventing
// a symlink inside an allowed directory from pointing outside it.
//
// For paths that do not exist yet (e.g. write targets), we resolve symlinks on
// the parent directory and re-join the filename. This correctly handles systems
// like macOS where os.TempDir() returns a symlink (e.g. /var/folders → /private/var/folders)
// so that a non-existent child path still resolves to the same real root as the
// allowed directory.
func IsPathAllowed(path string, allowedPaths []string) bool {
	resolved := resolvePath(path)
	if resolved == "" {
		return false
	}

	for _, allowed := range allowedPaths {
		resolvedAllowed := resolvePath(allowed)
		if resolvedAllowed == "" {
			continue
		}
		// Ensure the prefix match ends on a directory boundary to prevent
		// /allowed/foo matching /allowed/foobar.
		if resolved == resolvedAllowed || strings.HasPrefix(resolved, resolvedAllowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
