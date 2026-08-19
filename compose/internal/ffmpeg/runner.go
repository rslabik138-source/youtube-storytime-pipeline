package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Probe is ffprobe's answer for one media file — just what compose needs
// (duration, and dimensions for the background so a caller could sanity-
// check it against the target resolution before scaling).
type Probe struct {
	DurationSeconds float64
	Width, Height   int
}

// Runner executes ffmpeg/ffprobe. CLIRunner is the real implementation;
// tests use FakeRunner.
type Runner interface {
	Run(ctx context.Context, args []string) error
	Probe(ctx context.Context, path string) (Probe, error)
}

// CLIRunner shells out to real ffmpeg/ffprobe binaries.
type CLIRunner struct {
	FFmpegCmd  string
	FFprobeCmd string
}

func (r CLIRunner) Run(ctx context.Context, args []string) error {
	cmd := r.FFmpegCmd
	if cmd == "" {
		cmd = "ffmpeg"
	}
	c := exec.CommandContext(ctx, cmd, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, lastLines(stderr.String(), 40))
	}
	return nil
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}
type ffprobeStream struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}
type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

func (r CLIRunner) Probe(ctx context.Context, path string) (Probe, error) {
	cmd := r.FFprobeCmd
	if cmd == "" {
		cmd = "ffprobe"
	}
	args := []string{"-v", "error", "-show_entries", "format=duration:stream=width,height", "-of", "json", path}
	c := exec.CommandContext(ctx, cmd, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return Probe{}, fmt.Errorf("ffprobe: %s: %w\n%s", path, err, stderr.String())
	}

	var out ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return Probe{}, fmt.Errorf("ffprobe: parse json output for %s: %w", path, err)
	}
	var duration float64
	if _, err := fmt.Sscanf(out.Format.Duration, "%f", &duration); err != nil {
		return Probe{}, fmt.Errorf("ffprobe: %s: unparseable duration %q: %w", path, out.Format.Duration, err)
	}
	p := Probe{DurationSeconds: duration}
	if len(out.Streams) > 0 {
		p.Width, p.Height = out.Streams[0].Width, out.Streams[0].Height
	}
	return p, nil
}

// lastLines keeps only the last n lines of s — ffmpeg's stderr is
// routinely hundreds of lines of per-frame/per-filter chatter; the error
// is almost always at the very end.
func lastLines(s string, n int) string {
	start := len(s)
	count := 0
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			count++
			if count > n {
				start = i + 1
				break
			}
		}
	}
	return s[start:]
}
