package assemble

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

// SampleSeams extracts n random chunk-boundary regions (5 seconds before
// and after each) from an already-assembled wavPath, using timing's chunk
// boundaries — for `voice speak <id> --sample-seams`, so a seam can be
// checked by ear without listening through the whole file.
func SampleSeams(ctx context.Context, wavPath string, timing Timing, n int, outDir string) ([]string, error) {
	numSeams := len(timing.Chunks) - 1
	if numSeams < 1 {
		return nil, fmt.Errorf("assemble: need at least 2 chunks to sample a seam, got %d", len(timing.Chunks))
	}
	if n <= 0 {
		n = 3
	}
	if n > numSeams {
		n = numSeams
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("assemble: create %s: %w", outDir, err)
	}

	const halfWindow = 5.0 // seconds before and after each seam
	var outPaths []string
	for _, seamIdx := range randomDistinctIndices(numSeams, n) {
		boundary := timing.Chunks[seamIdx].End
		start := boundary - halfWindow
		if start < 0 {
			start = 0
		}
		duration := halfWindow * 2

		outPath := filepath.Join(outDir, fmt.Sprintf("seam_%02d_at_%.1fs.wav", seamIdx, boundary))
		if err := runFFmpeg(ctx, "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", duration), "-i", wavPath, outPath); err != nil {
			return nil, fmt.Errorf("assemble: extract seam at %.1fs: %w", boundary, err)
		}
		outPaths = append(outPaths, outPath)
	}
	return outPaths, nil
}

// randomDistinctIndices picks k distinct indices from [0,n) — every seam
// if k >= n, otherwise a random sample (this is a manual-listening spot
// check, not something that needs reproducibility guarantees).
func randomDistinctIndices(n, k int) []int {
	if k >= n {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	return rand.Perm(n)[:k]
}
