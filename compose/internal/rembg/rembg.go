// Package rembg removes the background from the avatar module's portrait
// PNG via the rembg CLI (Python, local, free — `pip install rembg`), with
// a cache keyed by script id so a second `compose build` on the same id
// never re-runs it.
package rembg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner removes the background from a single image. CLIRunner is the
// real implementation; tests use FakeRunner.
type Runner interface {
	Remove(ctx context.Context, inputPath, outputPath string) error
}

// CLIRunner shells out to the real rembg CLI: `rembg i <in> <out>`.
type CLIRunner struct {
	Cmd string // "rembg", or a full path
}

func (r CLIRunner) Remove(ctx context.Context, inputPath, outputPath string) error {
	cmd := r.Cmd
	if cmd == "" {
		cmd = "rembg"
	}
	if _, err := exec.LookPath(cmd); err != nil {
		return fmt.Errorf("rembg: %q not found on PATH — install it with `pip install rembg` (requires Python): %w", cmd, err)
	}

	c := exec.CommandContext(ctx, cmd, "i", inputPath, outputPath)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rembg: %s i %s %s: %w\n%s", cmd, inputPath, outputPath, err, out)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		return fmt.Errorf("rembg: command succeeded but %s wasn't created: %w", outputPath, statErr)
	}
	return nil
}

// CutoutPath returns where EnsureCutout puts (or finds) id's cutout —
// exposed so callers can reference the path before/without calling
// EnsureCutout (e.g. to decide whether to log "reusing cached cutout").
func CutoutPath(cacheDir, id string) string {
	return filepath.Join(cacheDir, id, "portrait-cutout.png")
}

// EnsureCutout returns id's background-removed portrait, running runner
// only if there's no cached result already newer than inputPath — re-runs
// automatically if the source portrait.png was regenerated since the last
// cutout (a stale cache is worse than a redundant rembg call).
func EnsureCutout(ctx context.Context, runner Runner, inputPath, cacheDir, id string) (string, error) {
	outPath := CutoutPath(cacheDir, id)

	fresh, err := cacheIsFresh(inputPath, outPath)
	if err != nil {
		return "", err
	}
	if fresh {
		return outPath, nil
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("rembg: create cache dir for %s: %w", id, err)
	}
	if err := runner.Remove(ctx, inputPath, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

// cacheIsFresh reports whether outputPath exists and is at least as new
// as inputPath. inputPath must exist — its absence is a real error (the
// portrait this whole step depends on is missing), not "cache miss."
func cacheIsFresh(inputPath, outputPath string) (bool, error) {
	inStat, err := os.Stat(inputPath)
	if err != nil {
		return false, fmt.Errorf("rembg: source portrait %s: %w", inputPath, err)
	}
	outStat, err := os.Stat(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("rembg: check cached cutout %s: %w", outputPath, err)
	}
	return !outStat.ModTime().Before(inStat.ModTime()), nil
}
