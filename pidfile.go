// pidfile.go
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// pidLock is a held single-instance lock, backed by the pid file itself.
//
// The pid file used to be written unconditionally: a second `mcphe` overwrote
// the first one's pid, and when the first one exited its removePidFile deleted
// the *second* instance's file. Nothing prevented two daemons from serving the
// same config, sharing a revocation file and a plugin directory.
//
// The lock is the pid file's own handle, so there is no second path to keep in
// sync: acquiring it means holding an exclusive lock on that inode and having
// written our pid into it. Release unlinks the file *while still holding the
// lock*, so a successor that creates the same path gets a new inode and cannot
// have its file removed by us.
type pidLock struct {
	f    *os.File
	path string
}

// pidLockDisabled is the zero lock, used when no pid_file is configured.
// Release on it is a no-op, so callers never need a nil check.
func (l *pidLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	// Unlink before unlocking: another process that creates the path after this
	// gets a fresh inode, so this removal can only ever delete our own file.
	os.Remove(l.path)
	l.unlockAndClose()
}

// readPid returns the pid recorded in f, or 0 when it holds no usable value.
// The file is rewound afterwards so the caller can overwrite it.
func readPid(f *os.File) int {
	if _, err := f.Seek(0, 0); err != nil {
		return 0
	}
	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// writePid truncates f and writes the current process id into it.
func writePid(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncating pid file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("rewinding pid file: %w", err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		return fmt.Errorf("writing pid file: %w", err)
	}
	return f.Sync()
}

// alreadyRunning builds the refusal message for a pid file another instance
// holds, naming the pid when the file still says who it was.
func alreadyRunning(path string, pid int) error {
	if pid > 0 {
		return fmt.Errorf("another instance is already running (pid %d, pid file %s)", pid, path)
	}
	return fmt.Errorf("another instance is already running (pid file %s)", path)
}

// acquirePidLockForConfig takes the lock named by cfg.PidFile. An empty
// pid_file means single-instance protection is not configured, which is the
// documented behaviour: nothing is written and the returned lock is inert.
func acquirePidLockForConfig(path string) (*pidLock, error) {
	if path == "" {
		return &pidLock{}, nil
	}
	return acquirePidLock(path)
}
