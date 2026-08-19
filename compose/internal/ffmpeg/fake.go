package ffmpeg

import (
	"context"
	"errors"
	"strings"
)

// FakeRunner is a Runner that never spawns a process: it records every
// Run call's args and returns a queued Probe / a configurable failure —
// FailArgsContaining makes Run fail whenever the args contain a given
// substring (e.g. "h264_nvenc"), the mechanism RunWithFallback's tests
// use to simulate "no GPU available."
type FakeRunner struct {
	RunCalls    [][]string
	ProbeResult Probe
	ProbeErr    error
	// FailArgsContaining: Run returns an error whenever any argument
	// equals or the joined arg string contains this substring. Empty
	// means never fail.
	FailArgsContaining string
}

func (f *FakeRunner) Run(ctx context.Context, args []string) error {
	f.RunCalls = append(f.RunCalls, args)
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.FailArgsContaining != "" && strings.Contains(strings.Join(args, " "), f.FailArgsContaining) {
		return errors.New("fake: simulated failure (" + f.FailArgsContaining + ")")
	}
	return nil
}

func (f *FakeRunner) Probe(ctx context.Context, path string) (Probe, error) {
	if f.ProbeErr != nil {
		return Probe{}, f.ProbeErr
	}
	return f.ProbeResult, nil
}
