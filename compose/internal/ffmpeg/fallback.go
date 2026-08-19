package ffmpeg

import (
	"context"
	"fmt"
)

// RunWithFallback runs buildArgs(primary) first; if that fails, it logs
// via onFallback (nil is fine — no-op) and retries the WHOLE render with
// buildArgs(fallback) — never a partial retry, since a filter-graph
// failure partway through leaves no usable partial output to resume from.
// Returns which encoder actually produced the output.
func RunWithFallback(ctx context.Context, runner Runner, buildArgs func(Encoder) []string, primary, fallback Encoder, onFallback func(primaryErr error)) (Encoder, error) {
	primaryErr := runner.Run(ctx, buildArgs(primary))
	if primaryErr == nil {
		return primary, nil
	}
	if onFallback != nil {
		onFallback(primaryErr)
	}
	if fbErr := runner.Run(ctx, buildArgs(fallback)); fbErr != nil {
		return Encoder{}, fmt.Errorf("both encoders failed — %s: %v; %s: %v", primary.Name, primaryErr, fallback.Name, fbErr)
	}
	return fallback, nil
}
