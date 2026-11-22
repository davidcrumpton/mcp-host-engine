// lows_test.go
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mcphe/config"
)

// L3: parseTokenDuration used to accept "-1h", "NaNd" and "1e400d". The last two
// are not rejected by ParseFloat+float→Duration: ParseFloat returns ±Inf/NaN for
// them and the conversion overflows to a negative Duration. A negative TTL is
// not a rejected input, it is a wrong answer -- `token revoke bob -1h` wrote an
// already-expired entry that readEntries pruned on the next reload, so the
// revocation silently vanished.
func TestParseTokenDurationRejectsNonPositiveAndNonFinite(t *testing.T) {
	for _, in := range []string{
		"-1h", "-1d", "-1y", "0s", "0d", "0y", "",
		"NaNd", "Infy", "-Infy", "1e400d", "1e400y", "-1e400d",
		"999999999999y", "abc", "1x",
	} {
		if d, err := parseTokenDuration(in); err == nil {
			t.Errorf("parseTokenDuration(%q) = %v, want an error", in, d)
		}
	}
}

func TestParseTokenDurationAcceptsPositiveSpans(t *testing.T) {
	cases := map[string]time.Duration{
		"12h":  12 * time.Hour,
		"30m":  30 * time.Minute,
		"30d":  30 * 24 * time.Hour,
		"1y":   365 * 24 * time.Hour,
		" 1d ": 24 * time.Hour,
		"2.5d": 60 * time.Hour,
	}
	for in, want := range cases {
		got, err := parseTokenDuration(in)
		if err != nil {
			t.Errorf("parseTokenDuration(%q): unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseTokenDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

// L16: isLoopback compared against three literals, so `host: localhost` -- the
// most common way to write "listen locally" -- produced the "the API is
// unauthenticated" warning on every start.
func TestIsLoopback(t *testing.T) {
	loopback := []string{"127.0.0.1", "127.0.0.5", "::1", "[::1]", "localhost", "LOCALHOST", "app.localhost", " 127.0.0.1 "}
	for _, h := range loopback {
		if !isLoopback(h) {
			t.Errorf("isLoopback(%q) = false, want true", h)
		}
	}
	// A name that does not resolve is not loopback: the warning should err
	// towards firing.
	notLoopback := []string{"", "0.0.0.0", "::", "192.168.1.10", "example.com", "localhost.example.com"}
	for _, h := range notLoopback {
		if isLoopback(h) {
			t.Errorf("isLoopback(%q) = true, want false", h)
		}
	}
}

// L4: the pid file was written unconditionally, so a second instance clobbered
// the first one's pid and the first one's removePidFile then deleted the
// *second* instance's file. Two daemons could serve one config.
func TestPidLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcphe.pid")

	first, err := acquirePidLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := acquirePidLock(path); err == nil {
		t.Fatal("second acquire succeeded; only one instance may hold the pid file")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Errorf("second acquire error = %q, want it to say another instance is running", err)
	}

	// The first instance's pid survived the second attempt.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file contains %q, want the first instance's pid %d", data, os.Getpid())
	}

	first.Release()

	// After release the lock is available again, and the file is gone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("pid file still exists after Release: %v", err)
	}
	second, err := acquirePidLock(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	second.Release()
}

// A crashed instance leaves the file but not the lock: the kernel drops an
// advisory lock when the process dies, so a successor does not have to guess
// whether the recorded pid is still alive (which races with pid reuse).
func TestPidLockIgnoresAStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcphe.pid")
	if err := os.WriteFile(path, []byte("999999"), 0o644); err != nil {
		t.Fatalf("writing stale pid file: %v", err)
	}
	lock, err := acquirePidLock(path)
	if err != nil {
		t.Fatalf("acquire over a stale pid file: %v", err)
	}
	defer lock.Release()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file contains %q, want the current pid", data)
	}
}

// Release must never delete a successor's file. It unlinks while still holding
// the lock, so a file created at the same path afterwards is a different inode.
func TestPidLockReleaseDoesNotDeleteASuccessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcphe.pid")

	first, err := acquirePidLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	first.Release()

	second, err := acquirePidLock(path)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	// A late Release on the first (already released) lock is a no-op, not a
	// deletion of the second instance's file.
	first.Release()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("successor's pid file was removed: %v", err)
	}
	second.Release()
}

// An unset pid_file means no single-instance protection, which is the
// documented behaviour: nothing is written and Release is inert.
func TestPidLockWithoutPidFileIsInert(t *testing.T) {
	lock, err := acquirePidLockForConfig("")
	if err != nil {
		t.Fatalf("acquire with no pid_file: %v", err)
	}
	lock.Release() // must not panic
}

// L17: the root check has to read run_as_root out of the raw bytes, because
// acting on the config (resolving env://, logging in to Vault) is exactly what
// must not happen before the decision.
func TestPeekRunAsRoot(t *testing.T) {
	allowed, err := peekRunAsRoot(t, "run_as_root: true\nport: \"1\"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("run_as_root: true was not seen")
	}

	allowed, err = peekRunAsRoot(t, "port: \"1\"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("run_as_root defaulted to true")
	}
}

// peekRunAsRoot reads run_as_root out of raw config bytes.
func peekRunAsRoot(t *testing.T, yaml string) (bool, error) {
	t.Helper()
	return config.PeekRunAsRoot([]byte(yaml))
}
