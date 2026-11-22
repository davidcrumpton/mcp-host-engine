// host/fs/fs.go
package fs

import (
	"fmt"
	"io"
	"math"
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
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// The content is handed to plugin JS as one string, so an unbounded read is
	// an unbounded allocation: an allow-listed path that happens to be
	// /dev/zero, a FIFO, or simply a huge log file would take the daemon down.
	data, err := io.ReadAll(io.LimitReader(f, maxReadFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxReadFileBytes {
		return "", fmt.Errorf("file %q exceeds the %d byte read limit", path, maxReadFileBytes)
	}
	return string(data), nil
}

// maxReadFileBytes bounds host.fs.readFile. 32 MiB is generous for a file a
// plugin is going to hold in memory as a string anyway.
const maxReadFileBytes = 32 << 20

func WriteFile(path string, content string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))

	if !allowed {
		return fmt.Errorf("writing to file %q is not allowed", path)
	}
	return writeFileAtomic(path, []byte(content))
}

// writeFileAtomic writes data to path through a temp file in the same directory
// and a rename.
//
// os.WriteFile truncates the destination first, so a crash or a short write
// leaves a truncated file where the old content used to be. Rename within a
// directory is atomic, so the destination is either the old content or the new
// content, never half of either. An existing file's permission bits are
// preserved: overwriting a 0755 script with a 0644 one silently made it
// unrunnable.
func writeFileAtomic(path string, data []byte) error {
	f, tmp, err := createTempBeside(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	return finishTemp(f, tmp, path)
}

// createTempBeside opens a temp file in path's directory -- the rename in
// finishTemp is only atomic within one filesystem, so os.TempDir() would not do.
func createTempBeside(path string) (*os.File, string, error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return nil, "", err
	}
	return f, f.Name(), nil
}

// finishTemp flushes, applies the destination's existing mode (or 0644), closes
// and renames the temp file over path. On any failure the temp file is removed
// and the destination is left untouched.
func finishTemp(f *os.File, tmp, path string) error {
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
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

// ReadStream reads path in chunks, calling callback for each one.
//
// The chunk size comes from a tool argument, so it is validated before it is
// used as a slice length: a negative or absurd value would panic in
// make([]byte, chunkSize), goja re-panics a Go panic rather than throwing a JS
// error, and nothing on the tools/call path recovers it -- the daemon would die.
// A size of 0 is refused too: it makes Read return n=0, err=nil, which the loop
// below would report as a successful empty read.
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

	// Default chunk size is 4KB.
	chunkSize := defaultReadChunkSize
	if options != nil {
		if raw, ok := options["chunkSize"]; ok && raw != nil {
			if size, isNumber := numericToInt(raw); isNumber {
				if size <= 0 || size > maxReadChunkSize {
					return fmt.Errorf("readStream: chunkSize must be between 1 and %d bytes, got %d", maxReadChunkSize, size)
				}
				chunkSize = size
			}
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

const (
	// defaultReadChunkSize is used when the caller asks for nothing in
	// particular.
	defaultReadChunkSize = 4096
	// maxReadChunkSize bounds one chunk. Each chunk is handed to JS as a string,
	// so this is also a bound on the memory one callback can be asked to accept.
	maxReadChunkSize = 8 << 20 // 8 MiB
)

// numericToInt converts a JS-supplied number to an int for use as a size. Values
// that cannot be a size -- NaN, ±Inf, anything outside the int32 range -- come
// back as badSize so the caller's range check rejects them instead of the
// conversion silently wrapping to something that looks valid. A value that is
// not a number at all reports false, leaving the caller's default in place.
func numericToInt(v interface{}) (int, bool) {
	const badSize = -1
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		if n > math.MaxInt32 {
			return math.MaxInt32, true
		}
		if n < math.MinInt32 {
			return badSize, true
		}
		return int(n), true
	case float32:
		return numericToInt(float64(n))
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < math.MinInt32 || n > math.MaxInt32 {
			return badSize, true
		}
		return int(n), true
	}
	return 0, false
}

// WriteStream writes path in chunks supplied by callback, through a temp file
// and a rename.
//
// os.Create truncates the destination immediately, before a single byte of
// replacement content exists. If callback throws, or returns "" on its first
// call (the documented end-of-stream signal), the original file is already gone
// and the caller is left with an empty file. With the temp file the destination
// is only replaced once the stream has finished cleanly.
func WriteStream(path string, cfg config.Config, pluginName string, options map[string]interface{}, callback func() (string, error)) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("writing to file %q is not allowed", path)
	}
	file, tmp, err := createTempBeside(path)
	if err != nil {
		return err
	}

	for {
		chunk, err := callback()
		if err != nil {
			file.Close()
			os.Remove(tmp)
			return err
		}
		if chunk == "" {
			// End of stream
			break
		}

		// Write the chunk to file
		if _, err := file.WriteString(chunk); err != nil {
			file.Close()
			os.Remove(tmp)
			return err
		}
	}

	return finishTemp(file, tmp, path)
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
