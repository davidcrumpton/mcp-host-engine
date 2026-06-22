package auth

import (
	"bufio"
	"os"
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
	done := make(chan struct{})
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
	return func() { close(done) }
}

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
	entries, err := r.readEntries()
	if err != nil {
		return err
	}
	// If the key contains a colon, it's a specific token. Remove it.
	// If the key does not contain a colon, it's a blanket revoke. Reduce the list to just the username
	if strings.Contains(key, ":") {
		delete(entries, key)
	} else {
		// Remove all entries for that user
		for k := range entries {
			if strings.HasPrefix(k, key+":") {
				delete(entries, k)
			}
		}
		delete(entries, key)
		// Add back the entry with the new expiry
		entries[key] = expiry
	}
	return r.writeEntries(entries)
}

// Unrevoke removes all entries matching key from the revocation file and
// refreshes the in-memory set immediately. It's the inverse of Revoke, used
// when re-issuing a token to a previously-revoked key.
func (r *Revoker) Unrevoke(key string) error {
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
	entries, err := r.readEntries()
	if err != nil {
		return err
	}
	delete(entries, username)
	if label != "" {
		delete(entries, username+":"+label)
	}
	return r.writeEntries(entries)
}

// purge rewrites the revocation file without any entry matching key (and
// without entries whose expiry has already passed), then swaps in the new
// set. Unlike before, the change is persisted to disk -- previously purge
// only mutated the in-memory map, so the next reload from the Watch timer
// (or a restart) resurrected the entry straight off disk.
func (r *Revoker) purge(key string) error {
	entries, err := r.readEntries()
	if err != nil {
		return err
	}
	delete(entries, key)
	return r.writeEntries(entries)
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
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		key := fields[0]

		var expiry int64
		if len(fields) > 1 {
			expiry, _ = strconv.ParseInt(fields[1], 10, 64)
		}
		if expiry != 0 && expiry < now {
			continue // expired entry -- prune it
		}
		entries[key] = expiry
	}
	if err := scanner.Err(); err != nil {
		return nil, err
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

	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return err
	}

	r.mu.Lock()
	r.revoked = entries
	r.mu.Unlock()
	return nil
}