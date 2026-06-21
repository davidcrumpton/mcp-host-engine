package auth

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLabelRoundTrip(t *testing.T) {
	tok, err := Create("mcphe", "1.0", "bob", "laptop", time.Hour, "secret")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, err := Validate("mcphe", "1.0", tok, "secret", nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if id.Label != "laptop" {
		t.Errorf("Label: got %q, want %q", id.Label, "laptop")
	}
}

func TestUnlabeledTokenStillWorks(t *testing.T) {
	tok, _ := Create("mcphe", "1.0", "bob", "", time.Hour, "secret")
	id, err := Validate("mcphe", "1.0", tok, "secret", nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if id.Label != "" {
		t.Errorf("expected empty label, got %q", id.Label)
	}
}

func TestProgNameMismatchRejected(t *testing.T) {
	// Mint a token for a different (hypothetical) sibling program, then
	// validate it as an "mcphe" token. Validate's outer progname=="mcphe"
	// gate only checks the caller-supplied value (and passes here, since
	// we pass "mcphe"); this isolates the embedded id.Progname cross-check.
	tok, err := Create("mcphe-companion", "1.0", "bob", "", time.Hour, "secret")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := Validate("mcphe", "1.0", tok, "secret", nil); err == nil {
		t.Fatal("expected mismatched embedded progname to be rejected")
	}
}

func TestRevokerBlanketRevoke(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("NewRevoker: %v", err)
	}
	if err := r.Revoke("bob", 0); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Both labeled and unlabeled tokens for bob should be caught by the
	// blanket username revoke.
	if !r.IsRevoked(Identity{Username: "bob"}) {
		t.Error("expected unlabeled bob token to be revoked")
	}
	if !r.IsRevoked(Identity{Username: "bob", Label: "laptop"}) {
		t.Error("expected labeled bob token to be revoked by blanket entry")
	}
	if r.IsRevoked(Identity{Username: "alice"}) {
		t.Error("alice should not be affected by bob's revocation")
	}
}

func TestRevokerLabelOnlyRevoke(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	r, _ := NewRevoker(path)
	if err := r.Revoke("bob:laptop", 0); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if !r.IsRevoked(Identity{Username: "bob", Label: "laptop"}) {
		t.Error("expected bob:laptop to be revoked")
	}
	if r.IsRevoked(Identity{Username: "bob", Label: "ci-deploy"}) {
		t.Error("bob:ci-deploy should be unaffected by bob:laptop revoke")
	}
	if r.IsRevoked(Identity{Username: "bob"}) {
		t.Error("bare bob (unlabeled) should be unaffected by a label-specific revoke")
	}
}

func TestRevokerEndToEndWithValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")
	r, _ := NewRevoker(path)

	tok, _ := Create("mcphe", "1.0", "bob", "laptop", time.Hour, "secret")

	if _, err := Validate("mcphe", "1.0", tok, "secret", r); err != nil {
		t.Fatalf("expected token to validate before revoke, got: %v", err)
	}

	if err := r.Revoke("bob:laptop", 0); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := Validate("mcphe", "1.0", tok, "secret", r); err == nil {
		t.Fatal("expected token to be rejected after revoke")
	}
}

func TestRevokerPrunesExpiredEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	// Write an already-expired entry directly, then a fresh Revoker load
	// should drop it.
	past := time.Now().Add(-time.Hour).Unix()
	content := "bob:old-laptop " + strconv.FormatInt(past, 10) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("NewRevoker: %v", err)
	}
	if r.IsRevoked(Identity{Username: "bob", Label: "old-laptop"}) {
		t.Error("expired revocation entry should have been pruned on load")
	}
}

func TestRevokeIsIdempotentNoDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	r, _ := NewRevoker(path)
	for i := 0; i < 3; i++ {
		if err := r.Revoke("bob:laptop", 0); err != nil {
			t.Fatalf("Revoke #%d: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := strings.Count(string(data), "bob:laptop"); got != 1 {
		t.Errorf("expected bob:laptop to appear once after repeated revokes, got %d:\n%s", got, data)
	}
}

func TestUnrevokeIdentityClearsBlanketEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.txt")

	r, _ := NewRevoker(path)
	// Blanket-revoke bob (denies every one of bob's tokens), then re-issue a
	// labeled token. UnrevokeIdentity must clear the bare "bob" entry too, or
	// the new token is revoked on arrival.
	if err := r.Revoke("bob", 0); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := r.UnrevokeIdentity("bob", "iMac"); err != nil {
		t.Fatalf("UnrevokeIdentity: %v", err)
	}

	if r.IsRevoked(Identity{Username: "bob", Label: "iMac"}) {
		t.Error("re-issued bob:iMac token should not be revoked after UnrevokeIdentity")
	}
	if r.IsRevoked(Identity{Username: "bob"}) {
		t.Error("blanket bob entry should have been cleared by UnrevokeIdentity")
	}

	// The cleared blanket entry must be gone from disk, not just memory, so a
	// reload from the Watch timer can't resurrect it.
	if err := r.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if r.IsRevoked(Identity{Username: "bob"}) {
		t.Error("blanket bob entry reappeared after reload — purge did not persist")
	}
}

func TestRevokerMissingFileIsEmptyNotError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.txt")

	r, err := NewRevoker(path)
	if err != nil {
		t.Fatalf("expected missing revocation file to be treated as empty, got error: %v", err)
	}
	if r.IsRevoked(Identity{Username: "anyone"}) {
		t.Error("empty revocation list should revoke nobody")
	}
}