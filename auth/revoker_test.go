package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// token_revocation_file is optional: NewRevoker documents a missing file as an
// empty list, and most configs leave it unset. UnrevokeIdentity must therefore
// work with no path at all -- it used to write a bare ".tmp" into the working
// directory and then fail to rename it to "", which made `mcphe token create`
// fail outright for anyone who had not configured revocation.
func TestUnrevokeIdentityWithoutRevocationFile(t *testing.T) {
	r, err := NewRevoker("")
	if err != nil {
		t.Fatalf("NewRevoker(\"\"): %v", err)
	}

	if err := r.UnrevokeIdentity("alice", ""); err != nil {
		t.Fatalf("UnrevokeIdentity with no revocation file: %v", err)
	}

	// Nothing should have been created in the working directory.
	if _, err := os.Stat(".tmp"); !os.IsNotExist(err) {
		os.Remove(".tmp")
		t.Errorf(".tmp was written to the working directory (stat err: %v)", err)
	}
}

// With a real path the entry is persisted and removed, and the temp file used
// for the atomic swap must not be left behind.
func TestUnrevokeIdentityPersistsAndCleansUpTemp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.yaml")
	if err := os.WriteFile(path, []byte("alice\nbob:laptop\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("NewRevoker: %v", err)
	}
	if err := r.UnrevokeIdentity("alice", "laptop"); err != nil {
		t.Fatalf("UnrevokeIdentity: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bob:laptop\n" {
		t.Errorf("revocation file: got %q, want %q", got, "bob:laptop\n")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file %s.tmp was left behind", path)
	}
}
