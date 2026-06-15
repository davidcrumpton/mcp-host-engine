package fs

import (
	"os"
	"path/filepath"
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
