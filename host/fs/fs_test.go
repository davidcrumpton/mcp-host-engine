package fs

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphe/config"
)

// cfgWithPaths builds a Config that allows read/write to the given dir for
// the named plugin.
func cfgWithPaths(t *testing.T, pluginName, dir string) config.Config {
	t.Helper()
	return config.Config{
		Plugins: map[string]map[string]interface{}{
			pluginName: {
				"allowed_read_file_paths":  []interface{}{dir},
				"allowed_write_file_paths": []interface{}{dir},
			},
		},
	}
}

func TestIsPathAllowed_AllowedSubdir(t *testing.T) {
	dir := t.TempDir()
	allowed := IsPathAllowed(filepath.Join(dir, "subfile.txt"), []string{dir})
	if !allowed {
		t.Errorf("path %q inside allowed dir should be allowed", filepath.Join(dir, "subfile.txt"))
	}
}

func TestIsPathAllowed_NotAllowed(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	allowed := IsPathAllowed(filepath.Join(other, "file.txt"), []string{dir})
	if allowed {
		t.Error("path outside allowed dir should not be allowed")
	}
}

func TestIsPathAllowed_EmptyList(t *testing.T) {
	if IsPathAllowed("/tmp/foo", nil) {
		t.Error("empty allowed list should deny everything")
	}
}

func TestReadFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	os.WriteFile(path, []byte("world"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	got, err := ReadFile(path, cfg, "myplugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "world" {
		t.Errorf("got %q, want %q", got, "world")
	}
}

func TestReadFile_AccessDenied(t *testing.T) {
	_, err := ReadFile("/etc/passwd", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

func TestReadFile_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithPaths(t, "myplugin", dir)
	_, err := ReadFile(filepath.Join(dir, "nope.txt"), cfg, "myplugin")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	cfg := cfgWithPaths(t, "myplugin", dir)

	if err := WriteFile(path, "content", cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "content" {
		t.Errorf("file content: got %q, want %q", string(data), "content")
	}
}

func TestWriteFile_AccessDenied(t *testing.T) {
	err := WriteFile("/etc/passwd", "x", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

func TestListFiles_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	files, err := ListFiles(dir, cfg, "myplugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2", len(files))
	}
}

func TestListFiles_AccessDenied(t *testing.T) {
	_, err := ListFiles("/etc", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

func TestDeleteFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.txt")
	os.WriteFile(path, []byte("bye"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	if err := DeleteFile(path, cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}

func TestDeleteFile_AccessDenied(t *testing.T) {
	err := DeleteFile("/etc/passwd", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

func TestRenameFile_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("data"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	if err := RenameFile(src, dst, cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("destination should exist after rename")
	}
}

func TestRenameFile_AccessDeniedSrc(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithPaths(t, "myplugin", dir)
	err := RenameFile("/etc/passwd", filepath.Join(dir, "out.txt"), cfg, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error for src outside allowed paths")
	}
}

// ---------------------------------------------------------------------------
// ReadStream chunk size (H5)
// ---------------------------------------------------------------------------

func readStreamFixture(t *testing.T) (string, config.Config) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path, cfgWithPaths(t, "myplugin", dir)
}

// chunkSize reaches make([]byte, chunkSize) straight from a tool argument, so an
// unchecked value is a process-killing panic rather than a tool error.
func TestReadStream_RejectsBadChunkSize(t *testing.T) {
	path, cfg := readStreamFixture(t)
	for _, size := range []interface{}{
		float64(-1),
		float64(0),
		float64(maxReadChunkSize + 1),
		float64(1e18),
		math.NaN(),
		math.Inf(1),
		-4096, // an int, as a Go caller could pass
	} {
		err := ReadStream(path, cfg, "myplugin", map[string]interface{}{"chunkSize": size}, func(string) error {
			return nil
		})
		if err == nil {
			t.Errorf("chunkSize %v: expected an error", size)
			continue
		}
		if !strings.Contains(err.Error(), "chunkSize") {
			t.Errorf("chunkSize %v: error should name chunkSize, got: %v", size, err)
		}
	}
}

func TestReadStream_ChunkSizeIsHonoured(t *testing.T) {
	path, cfg := readStreamFixture(t)
	var chunks []string
	err := ReadStream(path, cfg, "myplugin", map[string]interface{}{"chunkSize": float64(4)}, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(chunks, ""); got != "hello world" {
		t.Errorf("content: got %q, want %q", got, "hello world")
	}
	if len(chunks) < 2 {
		t.Errorf("expected the file in several chunks, got %v", chunks)
	}
}

func TestReadStream_NonNumericChunkSizeUsesDefault(t *testing.T) {
	path, cfg := readStreamFixture(t)
	var got strings.Builder
	err := ReadStream(path, cfg, "myplugin", map[string]interface{}{"chunkSize": "big"}, func(chunk string) error {
		got.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "hello world" {
		t.Errorf("content: got %q", got.String())
	}
}

// ---------------------------------------------------------------------------
// M2 -- bounded reads
// ---------------------------------------------------------------------------

func TestReadFile_RejectsAFileOverTheLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	// A sparse file: 32 MiB of zeros without writing them.
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxReadFileBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	cfg := cfgWithPaths(t, "myplugin", dir)
	if _, err := ReadFile(path, cfg, "myplugin"); err == nil {
		t.Fatalf("reading a file over %d bytes should fail, not allocate it", maxReadFileBytes)
	}
}

// ---------------------------------------------------------------------------
// M6 -- writes are atomic and do not destroy what they cannot replace
// ---------------------------------------------------------------------------

func TestWriteStream_KeepsTheOriginalWhenTheCallbackFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := cfgWithPaths(t, "myplugin", dir)
	calls := 0
	err := WriteStream(path, cfg, "myplugin", nil, func() (string, error) {
		calls++
		if calls == 1 {
			return "replacement-start", nil
		}
		return "", os.ErrDeadlineExceeded
	})
	if err == nil {
		t.Fatal("expected the callback error to be returned")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original" {
		t.Errorf("destination was modified by a failed stream: got %q, want %q", got, "original")
	}
	assertNoTempFiles(t, dir)
}

func TestWriteStream_EmptyStreamWritesAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := cfgWithPaths(t, "myplugin", dir)
	// "" on the first call is the documented end-of-stream signal, so a stream
	// with no chunks is a request to write an empty file -- and that is what it
	// does. The difference from before is that this decision is reached only
	// after the callback has run cleanly, so the failure case above leaves the
	// original alone instead of looking identical to this one.
	if err := WriteStream(path, cfg, "myplugin", nil, func() (string, error) {
		return "", nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "" {
		t.Errorf("an empty stream should write an empty file: got %q", got)
	}
	assertNoTempFiles(t, dir)
}

func TestWriteStream_ReplacesContentOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := cfgWithPaths(t, "myplugin", dir)
	chunks := []string{"new-", "content"}
	i := 0
	if err := WriteStream(path, cfg, "myplugin", nil, func() (string, error) {
		if i >= len(chunks) {
			return "", nil
		}
		i++
		return chunks[i-1], nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new-content" {
		t.Errorf("got %q, want %q", got, "new-content")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode: got %v, want the original 0600", info.Mode().Perm())
	}
	assertNoTempFiles(t, dir)
}

func TestWriteFile_PreservesAnExistingModeAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := cfgWithPaths(t, "myplugin", dir)
	if err := WriteFile(path, "#!/bin/sh\necho hi\n", cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode: got %v, want 0755 preserved", info.Mode().Perm())
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "echo hi") {
		t.Errorf("content not replaced: %q", got)
	}
	assertNoTempFiles(t, dir)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
