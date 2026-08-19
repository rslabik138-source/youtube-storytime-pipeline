package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stepClient returns errs[i] (or, once exhausted, a success) on the i-th
// call — lets retry tests control exactly how many times a call fails
// before succeeding.
type stepClient struct {
	calls    int
	errs     []error
	resp     Response
	lastOpts Options
}

func (s *stepClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	i := s.calls
	s.calls++
	s.lastOpts = opts
	if i < len(s.errs) {
		return Response{}, s.errs[i]
	}
	return s.resp, nil
}

// fastDelays gives n 1ms delays — enough retries for tests without ever
// waiting the real production schedule (seconds to minutes).
func fastDelays(n int) []time.Duration {
	d := make([]time.Duration, n)
	for i := range d {
		d[i] = time.Millisecond
	}
	return d
}

func TestWithRetrySucceedsAfterRetryableErrors(t *testing.T) {
	inner := &stepClient{errs: []error{ErrRateLimited, ErrServer}, resp: Response{Text: "ok"}}
	c := WithRetry(inner, RetryConfig{Delays: fastDelays(5)})

	resp, err := c.Complete(context.Background(), "hi", Options{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("expected %q, got %q", "ok", resp.Text)
	}
	if inner.calls != 3 {
		t.Fatalf("expected 3 calls (2 failures + 1 success), got %d", inner.calls)
	}
}

func TestWithRetryGivesUpOnNonRetryableError(t *testing.T) {
	wantErr := errors.New("bad request")
	inner := &stepClient{errs: []error{wantErr, ErrRateLimited}}
	c := WithRetry(inner, RetryConfig{Delays: fastDelays(5)})

	_, err := c.Complete(context.Background(), "hi", Options{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the non-retryable error to surface immediately, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected exactly 1 call (no retry on a non-retryable error), got %d", inner.calls)
	}
}

func TestWithRetryDoesNotRetryModelNotFound(t *testing.T) {
	inner := &stepClient{errs: []error{ErrModelNotFound}}
	c := WithRetry(inner, RetryConfig{Delays: fastDelays(5)})

	_, err := c.Complete(context.Background(), "hi", Options{})
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound to surface, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected exactly 1 call — a 404 is a config error, not something retrying fixes, got %d", inner.calls)
	}
}

func TestWithRetryExhaustsDelaysThenGivesUp(t *testing.T) {
	inner := &stepClient{errs: []error{ErrServer, ErrServer, ErrServer, ErrServer, ErrServer}}
	c := WithRetry(inner, RetryConfig{Delays: fastDelays(2)}) // 2 delays = 3 total attempts

	_, err := c.Complete(context.Background(), "hi", Options{})
	if !errors.Is(err, ErrServer) {
		t.Fatalf("expected the last retryable error to surface, got %v", err)
	}
	if inner.calls != 3 {
		t.Fatalf("expected exactly len(Delays)+1=3 calls, got %d", inner.calls)
	}
}

func TestWithRetryDefaultsToFiveRetriesWhenDelaysEmpty(t *testing.T) {
	cfg := RetryConfig{}
	got := cfg.delays()
	if len(got) != len(DefaultRetryDelays) {
		t.Fatalf("expected the zero-value config to fall back to DefaultRetryDelays (%d entries), got %d", len(DefaultRetryDelays), len(got))
	}
	want := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 45 * time.Second, 120 * time.Second}
	for i, d := range want {
		if got[i] != d {
			t.Fatalf("DefaultRetryDelays[%d] = %v, want %v", i, got[i], d)
		}
	}
}

func TestWithRetryRespectsContextCancellation(t *testing.T) {
	inner := &stepClient{errs: []error{ErrServer, ErrServer, ErrServer}}
	c := WithRetry(inner, RetryConfig{Delays: []time.Duration{50 * time.Millisecond, time.Second, time.Second}})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.Complete(ctx, "hi", Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if inner.calls >= 3 {
		t.Fatalf("expected cancellation to cut the retry loop short, got %d calls", inner.calls)
	}
}

func TestWithJitterStaysWithinTwentyPercent(t *testing.T) {
	base := 10 * time.Second
	for i := 0; i < 50; i++ {
		got := withJitter(base)
		lo, hi := base*8/10, base*12/10
		if got < lo || got > hi {
			t.Fatalf("withJitter(%v) = %v, want within [%v, %v]", base, got, lo, hi)
		}
	}
}
