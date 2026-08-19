package llm

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// DefaultRetryDelays is the pause before each successive retry on a
// retryable error (ErrRateLimited, ErrServer): 5 retries after the initial
// attempt, giving external rate limits and transient overload room to
// clear before WithFailover gives up on a provider entirely.
var DefaultRetryDelays = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
	45 * time.Second,
	120 * time.Second,
}

type RetryConfig struct {
	// Delays is the ordered list of pauses before retry 1, 2, 3, ... A call
	// makes len(Delays)+1 attempts total (the initial try plus one per
	// delay). Empty uses DefaultRetryDelays.
	Delays []time.Duration
}

func (c RetryConfig) delays() []time.Duration {
	if len(c.Delays) == 0 {
		return DefaultRetryDelays
	}
	return c.Delays
}

type retryingClient struct {
	inner Client
	cfg   RetryConfig
}

// WithRetry wraps c with backoff-and-jitter on retryable errors
// (ErrRateLimited, ErrServer). Any other error — including
// ErrModelNotFound — or ctx cancellation returns immediately. This is the
// per-provider retry layer; WithFailover is the layer that moves to the
// next provider once this one gives up.
func WithRetry(c Client, cfg RetryConfig) Client {
	return &retryingClient{inner: c, cfg: cfg}
}

func (r *retryingClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	delays := r.cfg.delays()

	var lastErr error
	for attempt := 0; attempt <= len(delays); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(withJitter(delays[attempt-1])):
			}
		}

		resp, err := r.inner.Complete(ctx, prompt, opts)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		if !errors.Is(err, ErrRateLimited) && !errors.Is(err, ErrServer) {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

// withJitter spreads d by roughly +/-20%, so many concurrent callers
// backing off on the same delay schedule don't all retry in lockstep.
func withJitter(d time.Duration) time.Duration {
	spread := int64(d) / 5
	if spread <= 0 {
		return d
	}
	jitter := time.Duration(rand.Int63n(spread + 1))
	if rand.Intn(2) == 0 {
		return d - jitter
	}
	return d + jitter
}
