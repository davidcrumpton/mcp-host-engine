package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileResolver_Success(t *testing.T) {
	t.Parallel()
	
	tmpDir := t.TempDir()
	
	filePath := filepath.Join(tmpDir, "test.txt")
	content := "this is a test"
	
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	
	resolver := FileResolver{}
	
	// file:// prefix is not stripped by resolver
	ref := filePath
	got, err := resolver.Resolve(ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestFileResolver_EmptyRefFails(t *testing.T) {
	t.Parallel()
	
	resolver := FileResolver{}
	
	_, err := resolver.Resolve("file://")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestFileResolver_FileNotFoundFails(t *testing.T) {
	t.Parallel()
	
	resolver := FileResolver{}
	
	_, err := resolver.Resolve("file:///nonexistent/path.txt")
	if err == nil {
		t.Fatal("expected error for file not found")
	}
}

func TestFileResolver_NonCanonicalPathSuccess(t *testing.T) {
	t.Parallel()
	
	tmpDir := t.TempDir()
	
	// Create nested path: tmp/foo/../bar
	filePath := filepath.Join(tmpDir, "foo", "..", "bar.txt")
	content := "this is a test"
	
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("failed to create nested directory: %v", err)
	}
	
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	
	resolver := FileResolver{}
	
	// Resolve using non-canonical path
	ref := filePath
	got, err := resolver.Resolve(ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestFileResolver_PermissionsErrorFails(t *testing.T) {
	t.Parallel()
	
	tmpDir := t.TempDir()
	
	// Create directory we can't read from
	restrictedDir := filepath.Join(tmpDir, "restricted")
	if err := os.Mkdir(restrictedDir, 0000); err != nil {
		t.Fatalf("failed to create restricted directory: %v", err)
	}
	
	filePath := filepath.Join(restrictedDir, "secret.txt")
	
	// We don't even need to create the file; directory permissions alone should cause failure
	resolver := FileResolver{}
	
	ref := "file:///" + filePath
	_, err := resolver.Resolve(ref)
	if err == nil {
		t.Fatal("expected error for file with restricted permissions")
	}
}
