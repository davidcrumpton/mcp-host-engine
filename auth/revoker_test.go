package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// ---------------------------------------------------------------------------
// M7 -- the revocation file survives concurrent writers, and Watch is safe
// ---------------------------------------------------------------------------

// Two `mcphe token revoke` processes at once used to lose an update: both read
// the old list, and the second rename silently dropped the first one's entry.
func TestConcurrentRevokesDoNotLoseEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.txt")

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		key := fmt.Sprintf("user%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := NewRevoker(path)
			if err != nil {
				errs <- err
				return
			}
			if err := r.Revoke(key, 0); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Revoke: %v", err)
	}

	r, err := NewRevoker(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < writers; i++ {
		key := fmt.Sprintf("user%d", i)
		if !r.IsRevoked(Identity{Username: key}) {
			t.Errorf("%s was lost: the read-modify-write cycles were not serialised", key)
		}
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Errorf("lock file %s.lock was left behind", path)
	}
}

// The stop function used to be func(){ close(done) }, which panics the second
// time it is called -- a trap for any shutdown path with more than one caller.
func TestWatchStopIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.txt")
	r, err := NewRevoker(path)
	if err != nil {
		t.Fatal(err)
	}

	stop := r.Watch(10*time.Millisecond, nil)
	stop()
	stop() // must not panic
}

// time.NewTicker panics for a non-positive interval, and Watch takes the
// interval from its caller.
func TestWatchWithNonPositiveIntervalDoesNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revocations.txt")
	r, err := NewRevoker(path)
	if err != nil {
		t.Fatal(err)
	}

	stop := r.Watch(0, nil)
	stop()
	stop = r.Watch(-time.Second, nil)
	stop()
}
