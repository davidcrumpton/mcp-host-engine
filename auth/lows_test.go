package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// L12: bufio.Scanner's default line limit is 64 KB. One longer line made every
// reload fail, and because Watch only logs, the stale snapshot stayed live while
// revocations silently stopped taking effect.
func TestReadEntriesHandlesLongLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.yaml")

	// A 128 KB line -- past the old 64 KB limit, inside the new one -- followed
	// by a real entry. The long line is not a valid key, so it is skipped; the
	// real entry must still load.
	long := strings.Repeat("x", 128*1024)
	content := "# comment\n" + long + "\nbob:laptop 0\nalice\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing revocation file: %v", err)
	}

	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("NewRevoker: %v", err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.revoked["alice"]; !ok {
		t.Error("the valid entry after a long line was not loaded")
	}
	if _, ok := r.revoked["bob:laptop"]; !ok {
		t.Error("bob:laptop was not loaded")
	}
}

// A line beyond even the raised cap is an explicit error naming the file, not a
// silent partial load.
func TestReadEntriesReportsAnOverlongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.yaml")
	huge := strings.Repeat("y", maxRevocationLine+1024)
	if err := os.WriteFile(path, []byte(huge+"\n"), 0o644); err != nil {
		t.Fatalf("writing revocation file: %v", err)
	}
	if _, err := NewRevoker(path); err == nil {
		t.Fatal("expected an error for a line beyond maxRevocationLine")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should name the file", err)
	}
}

// L13: revocation keys were unvalidated. A username containing ":" made
// `alice:laptop` both a blanket username and a label key, and a username with
// whitespace broke the file format outright.
func TestValidateRevocationKey(t *testing.T) {
	valid := []string{"alice", "alice:laptop", "svc-deploy_1", "a.b@c", "user+tag:ci-1", strings.Repeat("a", maxIdentityNameLen)}
	for _, key := range valid {
		if err := ValidateRevocationKey(key); err != nil {
			t.Errorf("ValidateRevocationKey(%q): unexpected error: %v", key, err)
		}
	}

	invalid := []string{
		"", " ", "alice:", ":laptop", "alice:la bel", "al ice",
		"alice\nlaptop", "alice\teve", "alice:laptop:extra", "alice;rm -rf /",
		strings.Repeat("a", maxIdentityNameLen+1),
	}
	for _, key := range invalid {
		if err := ValidateRevocationKey(key); err == nil {
			t.Errorf("ValidateRevocationKey(%q) accepted an unusable key", key)
		}
	}
}

// A token whose username cannot be written to the revocation file should never
// be minted: it could not be revoked unambiguously afterwards.
func TestCreateRejectsUnrevocableIdentity(t *testing.T) {
	for _, tc := range []struct{ user, label string }{
		{"alice:laptop", ""},
		{"al ice", ""},
		{"alice", "la bel"},
		{"alice", "a:b"},
		{"", ""},
	} {
		if _, err := Create("mcphe", "v", tc.user, tc.label, time.Hour, "secret"); err == nil {
			t.Errorf("Create(%q, %q) accepted an identity that cannot be revoked", tc.user, tc.label)
		}
	}
	if _, err := Create("mcphe", "v", "alice", "laptop", time.Hour, "secret"); err != nil {
		t.Errorf("Create(alice, laptop): unexpected error: %v", err)
	}
}

func TestCreateRejectsNonPositiveTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Hour} {
		if _, err := Create("mcphe", "v", "alice", "", ttl, "secret"); err == nil {
			t.Errorf("Create with ttl %v was accepted", ttl)
		}
	}
}

// Revoke writes the key straight into the file, so it must refuse one it cannot
// represent -- and must not fall back to writing a mangled line.
func TestRevokeRejectsAnUnrepresentableKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.yaml")
	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("NewRevoker: %v", err)
	}
	if err := r.Revoke("alice:laptop:extra", 0); err == nil {
		t.Fatal("Revoke accepted a key with two colons")
	}
	if err := r.Revoke("alice eve", 0); err == nil {
		t.Fatal("Revoke accepted a key with a space")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a rejected Revoke wrote the revocation file anyway: %v", err)
	}
	if err := r.Revoke("alice:laptop", 0); err != nil {
		t.Fatalf("Revoke(alice:laptop): unexpected error: %v", err)
	}
	if !r.IsRevoked(Identity{Username: "alice", Label: "laptop"}) {
		t.Error("alice:laptop is not reported as revoked")
	}
}

// A malformed line in the file (a hand edit, an older version) is skipped rather
// than failing the whole reload: refusing the file would un-revoke everyone else.
func TestReadEntriesSkipsMalformedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.yaml")
	if err := os.WriteFile(path, []byte("bad key here\nalice 0\n"), 0o644); err != nil {
		t.Fatalf("writing revocation file: %v", err)
	}
	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("NewRevoker: %v", err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.revoked["alice"]; !ok {
		t.Error("a malformed line stopped the valid entry from loading")
	}
	if _, ok := r.revoked["bad"]; ok {
		t.Error(`"bad key here" was loaded as the key "bad"`)
	}
}
