package time

import (
	"context"
	"errors"
	"time"
)

// MaxSleepMilliseconds caps one host.sleep call.
//
// A plugin can ask for any number, and host calls are synchronous: one call to
// sleep(86400000) held that plugin's VM lock for a day, and since the tool call
// is what a client is waiting on, it also pinned a request handler and made
// shutdown wait for it. A sleep is a pacing aid, not a scheduler; a minute is
// already far past anything a tool call should do inside one request.
const MaxSleepMilliseconds = 60_000

// Sleep waits ms milliseconds. It rejects a negative or oversized duration
// rather than passing it to time.Sleep, which accepts both (a negative duration
// returns immediately, so sleep(-1) was a silent no-op that looked like success).
func Sleep(ms int) error {
	if err := validate(ms); err != nil {
		return err
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return nil
}

// SleepContext waits ms milliseconds or until ctx is done, whichever comes
// first, and reports ctx's error when it is cancelled. Without this a signal
// during a long sleep waited out the whole sleep before the process could exit.
func SleepContext(ctx context.Context, ms int) error {
	if err := validate(ms); err != nil {
		return err
	}
	if ctx == nil {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return nil
	}
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validate(ms int) error {
	if ms < 0 {
		return errors.New("sleep duration must be non-negative")
	}
	if ms > MaxSleepMilliseconds {
		return errors.New("sleep duration must be at most 60000 milliseconds")
	}
	return nil
}
