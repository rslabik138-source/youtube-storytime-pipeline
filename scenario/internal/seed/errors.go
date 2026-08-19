package seed

import "fmt"

// ErrAxesExhausted is returned when MaxAttempts draws in a row all failed a
// dedup constraint. Reason names which constraint was blocking most often,
// so the caller knows which config file to widen instead of just retrying.
type ErrAxesExhausted struct {
	Attempts int
	Reason   string
}

func (e *ErrAxesExhausted) Error() string {
	return fmt.Sprintf("seed: exhausted %d attempts drawing a seed: %s", e.Attempts, e.Reason)
}
