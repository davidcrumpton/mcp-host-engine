package time

import (
	"errors"
	"time"
)

func Sleep(ms int) error {
	if ms < 0 {
		return errors.New("sleep duration must be non-negative")
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return nil
}