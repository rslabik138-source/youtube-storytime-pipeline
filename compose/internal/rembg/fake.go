package rembg

import (
	"context"
	"os"
)

// FakeRunner is a Runner that never shells out: it records every
// (inputPath, outputPath) it was asked to process and writes a small
// placeholder file at outputPath so callers checking for its existence
// still see a real file.
type FakeRunner struct {
	Calls []struct{ Input, Output string }
	Err   error
}

func (f *FakeRunner) Remove(ctx context.Context, inputPath, outputPath string) error {
	f.Calls = append(f.Calls, struct{ Input, Output string }{inputPath, outputPath})
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.Err != nil {
		return f.Err
	}
	return os.WriteFile(outputPath, []byte("fake-cutout-png"), 0o644)
}
