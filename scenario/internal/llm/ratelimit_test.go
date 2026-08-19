package llm

import (
	"context"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestWithRateLimitThrottlesCalls(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	limiter := rate.NewLimiter(rate.Limit(5), 1) // ~1 token per 200ms, burst 1

	c := WithRateLimit(inner, limiter)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Complete(context.Background(), "hi", Options{}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 300*time.Millisecond {
		t.Fatalf("expected rate limiting to slow 3 calls to at least ~300ms, took %v", elapsed)
	}
}

func TestWithRateLimitNilLimiterIsNoOp(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	c := WithRateLimit(inner, nil)
	if c != Client(inner) {
		t.Fatalf("expected WithRateLimit(c, nil) to return c unchanged")
	}
}

func TestWithRateLimitRespectsContextCancellation(t *testing.T) {
	inner := &stepClient{resp: Response{Text: "ok"}}
	limiter := rate.NewLimiter(rate.Limit(0.1), 1) // burst of 1, then a ~10s refill
	c := WithRateLimit(inner, limiter)

	// Burn the single burst token immediately.
	if _, err := c.Complete(context.Background(), "hi", Options{}); err != nil {
		t.Fatalf("first Complete: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Complete(ctx, "hi", Options{}); err == nil {
		t.Fatalf("expected an error when the limiter wait exceeds the context deadline")
	}
}
