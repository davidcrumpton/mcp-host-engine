//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquirePidLock takes an exclusive advisory lock on the pid file and records
// this process's pid in it. The kernel releases the lock when the process dies,
// so a crashed instance never leaves a lock a successor has to reason about --
// unlike a "does this pid still exist" check, which races with pid reuse.
func acquirePidLock(path string) (*pidLock, error) {
	// Opening and then locking is two steps, and the file can be replaced or
	// unlinked in between. Verifying that the locked handle is still the file at
	// path, and retrying otherwise, is what makes the lock actually exclusive:
	// without it two processes can each hold a lock on a different inode with
	// the same name.
	for attempt := 0; attempt < 5; attempt++ {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening pid file %s: %w", path, err)
		}

		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			pid := readPid(f)
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
				return nil, alreadyRunning(path, pid)
			}
			return nil, fmt.Errorf("locking pid file %s: %w", path, err)
		}

		locked, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("stat of pid file %s: %w", path, err)
		}
		current, err := os.Stat(path)
		if err != nil || !os.SameFile(locked, current) {
			// The file we locked is no longer the one at path (removed or
			// replaced while we waited). Drop it and try again.
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			continue
		}

		if err := writePid(f); err != nil {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			return nil, err
		}
		return &pidLock{f: f, path: path}, nil
	}
	return nil, fmt.Errorf("could not lock pid file %s: it kept being replaced", path)
}

func (l *pidLock) unlockAndClose() {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
	l.f = nil
}
