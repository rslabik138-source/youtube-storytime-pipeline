package assemble

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// CheckFFmpeg verifies both ffmpeg and ffprobe are reachable on PATH.
// Call this once at CLI startup — a missing dependency should fail fast,
// before any (possibly slow) Kokoro calls are made, not buried inside the
// first Assemble call.
func CheckFFmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("assemble: ffmpeg not found on PATH — install ffmpeg (see README) before running `voice speak`: %w", err)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return fmt.Errorf("assemble: ffprobe not found on PATH — it ships with ffmpeg, so this usually means an incomplete install: %w", err)
	}
	return nil
}

func runFFmpeg(ctx context.Context, args ...string) error {
	full := append([]string{"-y", "-hide_banner", "-loglevel", "error"}, args...)
	cmd := exec.CommandContext(ctx, "ffmpeg", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// probeDuration returns path's audio duration in seconds via ffprobe —
// this is the "actually measured" half of the brief's requirement that
// timing.json's numbers come from real audio, never a word-count estimate.
func probeDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(stdout.String()), 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: parse duration %q: %w", path, strings.TrimSpace(stdout.String()), err)
	}
	return seconds, nil
}

// trimSilenceFilter removes leading AND trailing silence via the standard
// ffmpeg idiom: trim the start, reverse the stream, trim what's now at the
// start (the original end), reverse back. silenceremove alone only trims
// from the front, hence the reverse/trim/reverse pair.
const trimSilenceFilter = "silenceremove=start_periods=1:start_duration=0:start_threshold=-45dB:detection=peak," +
	"areverse," +
	"silenceremove=start_periods=1:start_duration=0:start_threshold=-45dB:detection=peak," +
	"areverse"

// trimSilence writes a copy of inPath to outPath with leading and trailing
// silence removed — the real per-chunk seam risk the brief calls out:
// Kokoro pads short calls with silence at the edges, and concatenating
// that padding directly produces audible gaps or double-silence at every
// join.
func trimSilence(ctx context.Context, inPath, outPath string) error {
	return runFFmpeg(ctx, "-i", inPath, "-af", trimSilenceFilter, outPath)
}

// normalizeAndFinalize applies dynaudnorm followed by EBU R128 loudness
// normalization, and writes the final 44100Hz mono 16-bit PCM WAV.
//
// dynaudnorm runs first: a real measurement (RMS energy over 20ms windows
// on a raw, unchunked Kokoro sample — see internal/assemble's own
// diagnosis notes, and PR discussion) showed every sentence's first
// ~50-100ms lands 10-15dB quieter than that same sentence's sustained
// voicing, right after Kokoro's own inter-sentence pause — this is Kokoro's
// own prosody, not an artifact of chunking or joining. loudnorm alone
// can't fix this: it normalizes the file's overall INTEGRATED loudness in
// one global pass, which preserves whatever local dip is already there.
// dynaudnorm's sliding-window gain (f=150ms frames, g=15 Gaussian filter
// size) boosts exactly this kind of brief local quiet passage — confirmed
// by the same measurement to close roughly 5dB of that gap. loudnorm
// still runs after it, unchanged, so the final file still lands at the
// requested integrated loudness target.
func normalizeAndFinalize(ctx context.Context, inPath, outPath string, lufs float64) error {
	filter := fmt.Sprintf("dynaudnorm=f=150:g=15,loudnorm=I=%.1f:TP=-1.5:LRA=11", lufs)
	return runFFmpeg(ctx, "-i", inPath, "-af", filter, "-ar", "44100", "-ac", "1", "-c:a", "pcm_s16le", outPath)
}

// joinSpec is one already-written (and, for custom stitch, silence-
// trimmed) piece file plus the pause that follows it. pauseAfter == 0
// means the next piece should crossfade directly against this one
// (custom stitch only) — a real gap already masks any transient, so a
// crossfade there would be pointless.
type joinSpec struct {
	path       string
	pauseAfter float64 // seconds
}

// join builds and runs ONE ffmpeg filter_complex graph over every piece in
// specs, producing outPath — a single pass rather than N sequential
// re-encodes, so joining a ~50-minute script's worth of chunks stays O(N)
// instead of O(N^2). Never concatenates raw WAV files directly (see
// trimSilenceFilter's doc comment for why that's the real seam risk):
//   - custom stitch: a zero pause crossfades (acrossfade, opts.CrossfadeMs)
//     directly onto the running track; a real pause inserts that exact
//     amount of generated silence (anullsrc) and joins with a plain concat
//     (no crossfade needed — the silence itself prevents any click).
//   - builtin stitch: always a plain concat, pause silence included when
//     pauseAfter > 0 — this is the "trust Kokoro, skip our own
//     post-processing" comparison path the brief asks for, not the
//     recommended default.
// maxJoinBatch caps how many pieces one filter_complex pass handles. This
// ffmpeg build (2026-07-27 gyan.dev full_build) doesn't accept
// -filter_complex_script (confirmed: "Unrecognized option"), so the whole
// filtergraph has to go inline as one argv element — and for a long script
// (150+ chunks) that graph runs to tens of thousands of characters, which
// pushes the total command line past what Windows' CreateProcess accepts.
// ffmpeg then fails to even start ("The filename or extension is too
// long"), despite every chunk having synthesized fine (hit 2026-08-17 on a
// 178-chunk script). Past this many pieces, join in batches and then join
// the batch outputs together — see joinBatched.
const maxJoinBatch = 50

func join(ctx context.Context, specs []joinSpec, opts Options, outPath string) error {
	if len(specs) == 0 {
		return fmt.Errorf("assemble: no pieces to join")
	}
	if len(specs) == 1 {
		return runFFmpeg(ctx, "-i", specs[0].path, "-ar", strconv.Itoa(opts.SampleRate), "-ac", "1", outPath)
	}
	if len(specs) > maxJoinBatch {
		return joinBatched(ctx, specs, opts, outPath)
	}

	args := make([]string, 0, len(specs)*2)
	for _, s := range specs {
		args = append(args, "-i", s.path)
	}

	silenceInputIdx := map[int]int{} // specs index i -> ffmpeg input index, for specs[i].pauseAfter > 0
	nextInput := len(specs)
	for i := 0; i < len(specs)-1; i++ {
		if specs[i].pauseAfter > 0 {
			args = append(args, "-f", "lavfi", "-t", fmt.Sprintf("%.3f", specs[i].pauseAfter),
				"-i", fmt.Sprintf("anullsrc=r=%d:cl=mono", opts.SampleRate))
			silenceInputIdx[i] = nextInput
			nextInput++
		}
	}

	var filter strings.Builder
	fmt.Fprintf(&filter, "[0:a]aresample=%d[a0]", opts.SampleRate)
	runningLabel := "a0"
	crossfadeSec := float64(opts.CrossfadeMs) / 1000.0

	for i := 1; i < len(specs); i++ {
		pieceLabel := fmt.Sprintf("p%d", i)
		nextLabel := fmt.Sprintf("a%d", i)
		fmt.Fprintf(&filter, ";[%d:a]aresample=%d[%s]", i, opts.SampleRate, pieceLabel)

		prevPause := specs[i-1].pauseAfter
		useCrossfade := opts.Stitch == StitchCustom && prevPause == 0
		if useCrossfade {
			fmt.Fprintf(&filter, ";[%s][%s]acrossfade=d=%.3f:c1=tri:c2=tri[%s]", runningLabel, pieceLabel, crossfadeSec, nextLabel)
		} else if prevPause > 0 {
			silLabel := fmt.Sprintf("sil%d", i)
			fmt.Fprintf(&filter, ";[%d:a]aresample=%d[%s]", silenceInputIdx[i-1], opts.SampleRate, silLabel)
			fmt.Fprintf(&filter, ";[%s][%s][%s]concat=n=3:v=0:a=1[%s]", runningLabel, silLabel, pieceLabel, nextLabel)
		} else {
			fmt.Fprintf(&filter, ";[%s][%s]concat=n=2:v=0:a=1[%s]", runningLabel, pieceLabel, nextLabel)
		}
		runningLabel = nextLabel
	}

	args = append(args, "-filter_complex", filter.String(), "-map", "["+runningLabel+"]",
		"-ar", strconv.Itoa(opts.SampleRate), "-ac", "1", outPath)
	return runFFmpeg(ctx, args...)
}

// joinBatched handles more than maxJoinBatch pieces by joining maxJoinBatch
// at a time into intermediate WAVs, then recursively joining those
// intermediates (which is how it also copes with, say, 3000 pieces: the
// second-level join call is itself subject to the same maxJoinBatch check).
// Each intermediate's pauseAfter carries over from the ORIGINAL last piece
// in its batch, so the pause/crossfade at every batch boundary matches
// exactly what a single unbatched pass would have produced — batching is
// purely a command-line-length workaround, not a change in output audio.
func joinBatched(ctx context.Context, specs []joinSpec, opts Options, outPath string) error {
	tmpDir, err := os.MkdirTemp("", "voice-join-batch-*")
	if err != nil {
		return fmt.Errorf("assemble: create batch temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	reduced := make([]joinSpec, 0, (len(specs)+maxJoinBatch-1)/maxJoinBatch)
	for start := 0; start < len(specs); start += maxJoinBatch {
		end := start + maxJoinBatch
		if end > len(specs) {
			end = len(specs)
		}
		batchOut := filepath.Join(tmpDir, fmt.Sprintf("batch_%04d.wav", start))
		if err := join(ctx, specs[start:end], opts, batchOut); err != nil {
			return fmt.Errorf("assemble: join batch [%d:%d): %w", start, end, err)
		}
		reduced = append(reduced, joinSpec{path: batchOut, pauseAfter: specs[end-1].pauseAfter})
	}
	return join(ctx, reduced, opts, outPath)
}
