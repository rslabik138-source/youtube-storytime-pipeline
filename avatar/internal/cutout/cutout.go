// Package cutout removes an avatar portrait's background, turning an opaque
// generated image into a transparent PNG ready to overlay (compose consumes
// it as the portrait cutout). It shells out to rembg (an AI matting tool —
// U2-Net / BiRefNet family) because that gives far cleaner hair edges than a
// chroma-key would, and rembg segments the person from ANY background, so no
// green screen is needed at generation time.
package cutout

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Options controls one background-removal run.
type Options struct {
	// RembgCmd is the rembg executable — a bare "rembg" (found on PATH) or a
	// full path to rembg.exe when it isn't on PATH (common with a user-scope
	// Python install on Windows).
	RembgCmd string
	// Model is the rembg model name. "birefnet-portrait" is tuned for human
	// head-and-shoulders portraits (the best hair edges); "u2net_human_seg"
	// and "isnet-general-use" are lighter alternatives. rembg downloads the
	// model on first use.
	Model string
}

// Args builds the rembg argument list for a single-image run reading inPath
// and writing outPath. Split out from RemoveBackground so the exact command
// is unit-testable without rembg installed.
//
//	rembg i -m <model> <in> <out>
func Args(o Options, inPath, outPath string) []string {
	args := []string{"i"}
	if o.Model != "" {
		args = append(args, "-m", o.Model)
	}
	return append(args, inPath, outPath)
}

// RemoveBackground runs rembg on inPNG (an opaque PNG) and returns a
// transparent PNG with the background matted out. It stages the input and
// output in a temp dir (rembg's CLI is path-in/path-out), so callers pass and
// receive bytes and never deal with files. rembg's own stderr is surfaced on
// failure — the usual first-run culprits (model download blocked, rembg not
// found) show up there.
func RemoveBackground(ctx context.Context, o Options, inPNG []byte) ([]byte, error) {
	if o.RembgCmd == "" {
		return nil, fmt.Errorf("cutout: rembg command not configured (settings.yaml cutout.rembg_cmd)")
	}

	dir, err := os.MkdirTemp("", "avatar-cutout-*")
	if err != nil {
		return nil, fmt.Errorf("cutout: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "in.png")
	outPath := filepath.Join(dir, "out.png")
	if err := os.WriteFile(inPath, inPNG, 0o644); err != nil {
		return nil, fmt.Errorf("cutout: write temp input: %w", err)
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, o.RembgCmd, Args(o, inPath, outPath)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("cutout: rembg failed (%v): %s", err, stderr.String())
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("cutout: read rembg output (rembg ran but wrote nothing?): %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cutout: rembg produced an empty file")
	}
	return out, nil
}
