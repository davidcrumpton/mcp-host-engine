//go:build windows

package main

import (
	"fmt"
	"os"
)

// acquirePidLock on Windows uses exclusive creation instead of an advisory
// lock, because Windows has no flock. O_EXCL gives the same "only one process
// can create this file" guarantee for the moment of acquisition.
//
// The difference from the unix path is the crash case: a process that dies
// without releasing leaves the file behind, and the next start has to be told
// to remove it. That is deliberate -- guessing whether a pid is still alive is
// how two instances end up running -- so the message says what to do.
func acquirePidLock(path string) (*pidLock, error) {
	if _, err := os.Stat(path); err == nil {
		pid := readPidPath(path)
		return nil, fmt.Errorf("%w; if no mcphe is running, delete the stale file", alreadyRunning(path, pid))
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			// Lost the race to another instance, which is the same refusal.
			return nil, alreadyRunning(path, readPidPath(path))
		}
		return nil, fmt.Errorf("creating pid file %s: %w", path, err)
	}
	if err := writePid(f); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return &pidLock{f: f, path: path}, nil
}

func (l *pidLock) unlockAndClose() {
	l.f.Close()
	l.f = nil
}

// readPidPath reads the pid out of path, or 0 when it cannot.
func readPidPath(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	return readPid(f)
}
