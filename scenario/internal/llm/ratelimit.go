package llm

import (
	"context"

	"golang.org/x/time/rate"
)

type rateLimitedClient struct {
	inner   Client
	limiter *rate.Limiter
}

// WithRateLimit throttles every call to limiter's rate — a single limiter
// shared across every provider when wrapped around a WithFailover client,
// so the whole pipeline never bursts past a configured RPS regardless of
// which provider ends up serving a given call. Optional: enabled only when
// Settings.RateLimitRPS > 0.
func WithRateLimit(c Client, limiter *rate.Limiter) Client {
	if limiter == nil {
		return c
	}
	return &rateLimitedClient{inner: c, limiter: limiter}
}

func (r *rateLimitedClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return Response{}, err
	}
	return r.inner.Complete(ctx, prompt, opts)
}
