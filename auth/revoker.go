package auth

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Revoker tracks revoked tokens.
//
// Revoked entries are either a bare username ("bob"), which revokes every
// token that user holds, or "username:label" ("bob:laptop"), which revokes
// only the matching labeled token.
//
// The set is refreshed periodically from a flat file on disk so that
// revocation takes effect without restarting the server. That file is
// intentionally separate from config.yaml: config is loaded once at
// startup, but nothing about that requires every piece of server state to
// be frozen at startup too -- this just re-reads its own small file on a
// timer, independent of the one-time config load.
//
// File format, one entry per line, optional trailing expiry in unix
// seconds:
//
//	bob
//	bob:laptop
//	alice:ci-deploy 1750531200
//
// The trailing expiry is optional and rarely needed -- it exists so that,
// if you happen to know the original token's exp, an entry can self-prune
// once that token would have expired naturally anyway. Entries with no
// expiry (or expiry 0) stay until removed by hand. Lines starting with '#'
// and blank lines are ignored.
type Revoker struct {
	path string

	mu      sync.RWMutex
	revoked map[string]int64 // key -> expiry (0 = no expiry, kept until removed)
}

// NewRevoker creates a Revoker backed by path and performs an initial
// synchronous load. A missing file is treated as an empty revocation list
// rather than an error, so the feature is opt-in -- nothing breaks if you
// never create the file.
func NewRevoker(path string) (*Revoker, error) {
	r := &Revoker{path: path, revoked: map[string]int64{}}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Watch starts a background goroutine that reloads the revocation file
// every interval. It returns a stop function; call it during shutdown.
// Reload errors are reported via logf (e.g. cfg.LogfForPlugin-style) and
// otherwise ignored -- a transient read failure keeps the previous
// in-memory snapshot rather than making every token look revoked or
// suddenly look valid.
func (r *Revoker) Watch(interval time.Duration, logf func(level int, format string, args ...interface{})) (stop func()) {
	// time.NewTicker panics on a non-positive interval, and this is a public API
	// taking a duration from its caller, so an unusable value falls back to the
	// documented default instead of taking the process down.
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	done := make(chan struct{})
	var once sync.Once
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := r.reload(); err != nil && logf != nil {
					logf(1, "revocation list reload failed: %v", err)
				}
			case <-done:
				return
			}
		}
	}()
	// The stop function is idempotent: closing a channel twice panics, and a
	// stop function that can only be called once is a shutdown-order trap.
	return func() { once.Do(func() { close(done) }) }
}

// defaultWatchInterval is used when Watch is given a non-positive interval.
const defaultWatchInterval = 5 * time.Second

// IsRevoked reports whether id matches a revoked entry: the bare-username
// key (blanket revoke) always, and the username:label key (specific
// revoke) when id has a label.
func (r *Revoker) IsRevoked(id Identity) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.revoked[id.Username]; ok {
		return true
	}
	if id.Label != "" {
		if _, ok := r.revoked[id.Username+":"+id.Label]; ok {
			return true
		}
	}
	return false
}

// Revoke adds key (a username, or "username:label") to the revocation file
// and refreshes the in-memory set immediately. expiry is an optional
// unix-seconds self-prune hint; pass 0 if you don't know or don't care.
//
// Revoking is idempotent: the file is rewritten from the de-duplicated set,
// so revoking an already-revoked key just refreshes its expiry rather than
// appending a duplicate line (the old append-only behaviour grew the file
// without bound on repeated revokes).

// Revoke needs to remove all entries when just username is passed without :label
// e.g. 'bear' needs to remove 'bear:windows' and 'bear' itself.
func (r *Revoker) Revoke(key string, expiry int64) error {
	// A key with whitespace or an extra colon cannot be represented in the file,
	// and one that is *almost* representable is worse: it silently revokes a
	// different identity than the operator asked for.
	if err := ValidateRevocationKey(key); err != nil {
		return err
	}
	// Read-modify-write under the file lock: two `mcphe token revoke` processes
	// running at once would otherwise each read the old list and the second
	// rename would silently drop the first one's entry.
	return r.withLock(func() error {
		entries, err := r.readEntries()
		if err != nil {
			return err
		}
		// If the key contains a colon, it's a specific token.
		// If the key does not contain a colon, it's a blanket revoke. Reduce the list to just the username
		if strings.Contains(key, ":") {
			entries[key] = expiry
		} else {
			// Remove all entries for that user
			for k := range entries {
				if strings.HasPrefix(k, key+":") {
					delete(entries, k)
				}
			}
			entries[key] = expiry
		}
		return r.writeEntries(entries)
	})
}

// Unrevoke removes all entries matching key from the revocation file and
// refreshes the in-memory set immediately. It's the inverse of Revoke, used
// when re-issuing a token to a previously-revoked key.
func (r *Revoker) Unrevoke(key string) error {
	if err := ValidateRevocationKey(key); err != nil {
		return err
	}
	return r.purge(key)
}

// UnrevokeIdentity clears every revocation entry that would block a freshly
// issued token for username/label, in a single rewrite: both the specific
// "username:label" entry and the bare "username" blanket entry. The blanket
// entry matters because IsRevoked treats a bare username as denying *all* of
// that user's tokens (see IsRevoked), so re-issuing without purging it would
// hand back a token that's revoked on arrival.
//
// Note this is deliberately broad: clearing the blanket "username" entry
// un-revokes every other labeled token that user holds too, not just the one
// being issued. That's the intended trade-off -- a blanket revoke is a
// coarse instrument, and issuing a new token is taken as a decision to trust
// the user again.
func (r *Revoker) UnrevokeIdentity(username, label string) error {
	if err := ValidateIdentityName("username", username); err != nil {
		return err
	}
	if label != "" {
		if err := ValidateIdentityName("label", label); err != nil {
			return err
		}
	}
	return r.withLock(func() error {
		entries, err := r.readEntries()
		if err != nil {
			return err
		}
		delete(entries, username)
		if label != "" {
			delete(entries, username+":"+label)
		}
		return r.writeEntries(entries)
	})
}

// purge rewrites the revocation file without any entry matching key (and
// without entries whose expiry has already passed), then swaps in the new
// set. Unlike before, the change is persisted to disk -- previously purge
// only mutated the in-memory map, so the next reload from the Watch timer
// (or a restart) resurrected the entry straight off disk.
func (r *Revoker) purge(key string) error {
	return r.withLock(func() error {
		entries, err := r.readEntries()
		if err != nil {
			return err
		}
		delete(entries, key)
		return r.writeEntries(entries)
	})
}

// reload re-reads the revocation file from disk and atomically swaps in the
// new set, leaving the file itself untouched.
func (r *Revoker) reload() error {
	entries, err := r.readEntries()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.revoked = entries
	r.mu.Unlock()
	return nil
}

// readEntries parses the revocation file into a de-duplicated map, dropping
// blank/comment lines and entries whose optional expiry has already passed.
// A missing file yields an empty map (not an error) so revocation stays
// opt-in.
// maxRevocationLine bounds one line of the revocation file. bufio.Scanner's
// default is 64 KB, which is not a lot for a line an operator might paste: one
// longer line made every reload fail, and because Watch only logs, the stale
// snapshot stayed live while revocations silently stopped taking effect. 1 MiB
// is far more than any real entry and still bounded.
const maxRevocationLine = 1 << 20

func (r *Revoker) readEntries() (map[string]int64, error) {
	f, err := os.Open(r.path)
	if os.IsNotExist(err) {
		return map[string]int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	now := time.Now().Unix()
	entries := map[string]int64{}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRevocationLine)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		key := fields[0]

		// A key that cannot be written by Revoke can still arrive from a hand
		// edit or an older file. Skipping it keeps every other revocation in
		// force, which is the safe direction: refusing the whole file would
		// un-revoke everybody else.
		if err := ValidateRevocationKey(key); err != nil {
			continue
		}

		// The line is "<key>" or "<key> <expiry>" and nothing else. Parsing
		// only the first field meant "bad key here" was read as a revocation
		// of the user "bad", so a malformed line quietly revoked someone.
		var expiry int64
		switch len(fields) {
		case 1:
		case 2:
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue // not an expiry -- a malformed line, not a key
			}
			expiry = n
		default:
			continue
		}
		if expiry != 0 && expiry < now {
			continue // expired entry -- prune it
		}
		entries[key] = expiry
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", r.path, err)
	}
	return entries, nil
}

// writeEntries rewrites the revocation file from entries (one key per line,
// with the optional expiry appended) and swaps the in-memory set to match.
// The write goes through a temp file + rename so a crash mid-write can't
// leave a truncated revocation list on disk.
func (r *Revoker) writeEntries(entries map[string]int64) error {
	var b strings.Builder
	for key, expiry := range entries {
		b.WriteString(key)
		if expiry != 0 {
			b.WriteByte(' ')
			b.WriteString(strconv.FormatInt(expiry, 10))
		}
		b.WriteByte('\n')
	}

	// An empty path means no revocation file was configured. Revocation is
	// opt-in (see NewRevoker), so there is nothing to persist: just keep the
	// in-memory set in sync. Falling through would write a bare ".tmp" into the
	// working directory and then fail to rename it to "", which is what made
	// `mcphe token create` fail outright when token_revocation_file was unset.
	if r.path == "" {
		r.mu.Lock()
		r.revoked = entries
		r.mu.Unlock()
		return nil
	}

	// A fixed temp name means two concurrent writers share one scratch file and
	// one of them renames a half-written list into place. os.CreateTemp in the
	// same directory gives each writer its own, and rename is still atomic.
	f, err := os.CreateTemp(filepath.Dir(r.path), "."+filepath.Base(r.path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		os.Remove(tmp)
		return err
	}

	r.mu.Lock()
	r.revoked = entries
	r.mu.Unlock()
	return nil
}

// lockPollInterval and lockTimeout bound how long a read-modify-write cycle
// waits for a concurrent writer (another `mcphe token revoke`, or a running
// daemon) to release the lock.
const (
	lockPollInterval = 10 * time.Millisecond
	lockTimeout      = 5 * time.Second
	// staleLockAge is how old a lock file must be before it is assumed to have
	// been left behind by a process that died while holding it. Nothing is held
	// across an operation that can take this long.
	staleLockAge = 30 * time.Second
)

// withLock runs fn while holding an exclusive lock file next to the revocation
// file, serialising the read-modify-write cycles in Revoke, UnrevokeIdentity and
// purge.
//
// This is an O_EXCL lock file rather than flock so it behaves the same on every
// platform the daemon builds for, and so a lock left by a killed process is
// recoverable: a lock older than staleLockAge is removed rather than wedging
// revocation forever.
func (r *Revoker) withLock(fn func() error) error {
	// No revocation file configured: nothing is persisted, so nothing to
	// serialise against other processes.
	if r.path == "" {
		return fn()
	}
	lock := r.path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			defer os.Remove(lock)
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("revocation lock %s: %w", lock, err)
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the revocation lock %s", lock)
		}
		time.Sleep(lockPollInterval)
	}
}
