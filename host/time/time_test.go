package time

import (
	"testing"
	"time"
)

func TestSleep(t *testing.T) {
	start := time.Now()
	err := Sleep(100)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	duration := time.Since(start)
	if duration < 100*time.Millisecond {
		t.Errorf("sleep duration is too short: %v", duration)
	}
}
