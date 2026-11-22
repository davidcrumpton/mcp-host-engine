package time

import (
	"context"
	"errors"
	"testing"
	"time"
)

// L10: host.sleep called time.Sleep directly, so this validated helper was
// bypassed entirely. A negative duration returns immediately from time.Sleep, so
// sleep(-1) reported success without sleeping.
func TestSleepRejectsNegative(t *testing.T) {
	if err := Sleep(-1); err == nil {
		t.Error("Sleep(-1) was accepted")
	}
}

// A plugin could ask for a day. host calls are synchronous and hold the plugin's
// VM lock, so an unbounded sleep blocked that plugin, its request handler, and
// shutdown.
func TestSleepRejectsOverTheCap(t *testing.T) {
	start := time.Now()
	if err := Sleep(MaxSleepMilliseconds + 1); err == nil {
		t.Error("Sleep above the cap was accepted")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the rejected sleep actually slept for %v", elapsed)
	}
}

// Cancelling the request must cut a long sleep short; otherwise SIGTERM waited
// out the whole sleep before the process could exit.
func TestSleepContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := SleepContext(ctx, MaxSleepMilliseconds)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("SleepContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cancelled sleep took %v", elapsed)
	}
}

func TestSleepContextWaits(t *testing.T) {
	start := time.Now()
	if err := SleepContext(context.Background(), 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("sleep returned after %v, want at least 50ms", elapsed)
	}
}

func TestSleepContextValidates(t *testing.T) {
	for _, ms := range []int{-1, MaxSleepMilliseconds + 1} {
		if err := SleepContext(context.Background(), ms); err == nil {
			t.Errorf("SleepContext(%d) was accepted", ms)
		}
	}
}
