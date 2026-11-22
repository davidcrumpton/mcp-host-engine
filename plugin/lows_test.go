package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// L15: the directory was scanned with filepath.Glob(dir + "/*.js"), which treats
// metacharacters in *dir* as part of the pattern. A plugin_dir containing "[" or
// "*" changed the match set, and a directory whose own name ends in ".js"
// matched itself. The directory is data, not a pattern.
func TestPluginFilesInTreatsTheDirectoryAsData(t *testing.T) {
	base := t.TempDir()
	for _, dirName := range []string{"plain", "with[bracket]", "with*star", "looks.js"} {
		dir := filepath.Join(base, dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		for _, name := range []string{"a.js", "b.js", "notes.txt", "nested"} {
			path := filepath.Join(dir, name)
			if name == "nested" {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", path, err)
				}
				continue
			}
			if err := os.WriteFile(path, []byte("module.exports = {};"), 0o644); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
		}

		files, err := pluginFilesIn(dir)
		if err != nil {
			t.Fatalf("pluginFilesIn(%q): %v", dir, err)
		}
		want := []string{filepath.Join(dir, "a.js"), filepath.Join(dir, "b.js")}
		if len(files) != len(want) {
			t.Fatalf("pluginFilesIn(%q) = %v, want %v", dir, files, want)
		}
		for i := range want {
			if files[i] != want[i] {
				t.Errorf("pluginFilesIn(%q)[%d] = %q, want %q", dir, i, files[i], want[i])
			}
		}
	}
}

func TestPluginFilesInMissingDirectory(t *testing.T) {
	if _, err := pluginFilesIn(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory should be an error")
	}
}
