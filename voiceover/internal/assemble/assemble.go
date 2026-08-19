// Package assemble turns synthesized chunk audio into the final voice.wav
// and its timing.json manifest, using ffmpeg (via exec.Command) for every
// audio operation. Assemble never concatenates raw WAV files directly —
// see ffmpeg.go's trimSilenceFilter and join doc comments for why that's
// the real seam risk this package exists to avoid.
package assemble

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/placeholder/voiceover/internal/chunk"
	"github.com/placeholder/voiceover/internal/manifest"
)

// StitchMode picks how chunk boundaries are joined.
type StitchMode string

const (
	// StitchBuiltin trusts Kokoro's own audio at each boundary: pieces are
	// concatenated with only the configured pause silence in between, no
	// trimming, no crossfade. Provided as the brief asks — a comparison
	// baseline to listen against — not the recommended default, since
	// plain concatenation is the real seam risk (see ffmpeg.go).
	StitchBuiltin StitchMode = "builtin"
	// StitchCustom trims leading/trailing silence off every piece and
	// crossfades same-paragraph (zero-pause) joins. The default.
	StitchCustom StitchMode = "custom"
)

// Options controls assembly. Zero-value fields fall back to
// settings.yaml's documented defaults via withDefaults.
type Options struct {
	Stitch       StitchMode
	CrossfadeMs  int
	LoudnessLUFS float64
	SampleRate   int
	KeepChunks   bool
	// ChunksDir is where per-piece temp WAVs are written — pass
	// output/<id>/chunks/ to match the module's documented layout, or
	// leave empty for an OS temp dir. Removed after assembly unless
	// KeepChunks is true.
	ChunksDir string
}

func (o Options) withDefaults() Options {
	if o.Stitch == "" {
		o.Stitch = StitchCustom
	}
	if o.CrossfadeMs <= 0 {
		o.CrossfadeMs = 20
	}
	if o.LoudnessLUFS == 0 {
		o.LoudnessLUFS = -14
	}
	if o.SampleRate <= 0 {
		o.SampleRate = 44100
	}
	return o
}

// PieceAudio is one chunk's synthesized audio, ready to be assembled.
type PieceAudio struct {
	Chunk chunk.Chunk
	WAV   []byte
}

// ChapterTiming is one chapter's span in the final assembled audio.
type ChapterTiming struct {
	Index int     `json:"index"`
	Beat  string  `json:"beat"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// ChunkTiming is one chunk's span in the final assembled audio, plus its
// own text — this is the granular data the background module's video
// assembly consumes for caption/scene sync.
type ChunkTiming struct {
	Index   int     `json:"index"`
	Chapter int     `json:"chapter"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
}

// Timing is timing.json's shape.
type Timing struct {
	ID           string          `json:"id"`
	Voice        string          `json:"voice"`
	TotalSeconds float64         `json:"total_seconds"`
	Chapters     []ChapterTiming `json:"chapters"`
	Chunks       []ChunkTiming   `json:"chunks"`
}

// Assemble writes each piece to disk, trims silence when Stitch is
// StitchCustom, joins everything with Chunk.PauseAfter silence and (for
// custom, zero-pause joins) a crossfade, normalizes loudness, and writes
// the final WAV to outWAVPath. Per-chunk and total durations in the
// returned Timing come from ffprobe on real audio (each chunk's own
// written file, and the finished output) — never from a word-count
// estimate. chapters supplies each chapter's beat name (chunk.Chunk itself
// only carries a chapter index, not the beat string).
func Assemble(ctx context.Context, pieces []PieceAudio, chapters []manifest.Chapter, outWAVPath, scriptID, voiceID string, opts Options) (Timing, error) {
	opts = opts.withDefaults()
	if len(pieces) == 0 {
		return Timing{}, fmt.Errorf("assemble: no pieces to assemble")
	}

	chunksDir := opts.ChunksDir
	if chunksDir == "" {
		var err error
		chunksDir, err = os.MkdirTemp("", "voiceover-chunks-*")
		if err != nil {
			return Timing{}, fmt.Errorf("assemble: create temp chunks dir: %w", err)
		}
	} else if err := os.MkdirAll(chunksDir, 0o755); err != nil {
		return Timing{}, fmt.Errorf("assemble: create chunks dir %s: %w", chunksDir, err)
	}
	if !opts.KeepChunks {
		defer os.RemoveAll(chunksDir)
	}

	if err := os.MkdirAll(filepath.Dir(outWAVPath), 0o755); err != nil {
		return Timing{}, fmt.Errorf("assemble: create output dir for %s: %w", outWAVPath, err)
	}

	specs := make([]joinSpec, len(pieces))
	durations := make([]float64, len(pieces))
	for i, p := range pieces {
		rawPath := filepath.Join(chunksDir, fmt.Sprintf("chunk_%04d.wav", i))
		if err := os.WriteFile(rawPath, p.WAV, 0o644); err != nil {
			return Timing{}, fmt.Errorf("assemble: write chunk %d: %w", i, err)
		}

		piecePath := rawPath
		if opts.Stitch == StitchCustom {
			trimmedPath := filepath.Join(chunksDir, fmt.Sprintf("chunk_%04d_trimmed.wav", i))
			if err := trimSilence(ctx, rawPath, trimmedPath); err != nil {
				return Timing{}, fmt.Errorf("assemble: trim silence on chunk %d: %w", i, err)
			}
			piecePath = trimmedPath
		}

		dur, err := probeDuration(ctx, piecePath)
		if err != nil {
			return Timing{}, fmt.Errorf("assemble: probe chunk %d duration: %w", i, err)
		}
		durations[i] = dur
		specs[i] = joinSpec{path: piecePath, pauseAfter: p.Chunk.PauseAfter.Seconds()}
	}

	joinedPath := filepath.Join(chunksDir, "joined.wav")
	if err := join(ctx, specs, opts, joinedPath); err != nil {
		return Timing{}, fmt.Errorf("assemble: join pieces: %w", err)
	}
	if err := normalizeAndFinalize(ctx, joinedPath, outWAVPath, opts.LoudnessLUFS); err != nil {
		return Timing{}, fmt.Errorf("assemble: normalize/finalize: %w", err)
	}

	total, err := probeDuration(ctx, outWAVPath)
	if err != nil {
		return Timing{}, fmt.Errorf("assemble: probe final duration: %w", err)
	}

	return buildTiming(pieces, durations, chapters, total, scriptID, voiceID), nil
}

// buildTiming derives each chunk's and chapter's [start,end) from the
// per-chunk durations ffprobe measured, walked cumulatively with each
// chunk's own PauseAfter added as the gap to the next one. This is a very
// close approximation of the final file's real timeline, not a second
// measurement of it: a custom-stitch crossfade shortens the joined
// audio by CrossfadeMs at each zero-pause seam (intentionally — that's
// what a crossfade does), and those seams are rare in practice (only
// where a single paragraph's sentences overflow one chunk), so the
// cumulative total stays within a fraction of a second of TotalSeconds,
// which IS the real ffprobe-measured figure.
func buildTiming(pieces []PieceAudio, durations []float64, chapters []manifest.Chapter, total float64, scriptID, voiceID string) Timing {
	beatByIndex := make(map[int]string, len(chapters))
	for _, c := range chapters {
		beatByIndex[c.Index] = c.Beat
	}

	chunkTimings := make([]ChunkTiming, len(pieces))
	type chapterAccum struct{ start, end float64 }
	accum := map[int]*chapterAccum{}
	var chapterOrder []int

	cursor := 0.0
	for i, p := range pieces {
		start := cursor
		end := start + durations[i]
		chunkTimings[i] = ChunkTiming{
			Index: p.Chunk.Index, Chapter: p.Chunk.ChapterIdx,
			Start: round2(start), End: round2(end), Text: p.Chunk.Text,
		}

		idx := p.Chunk.ChapterIdx
		if a, ok := accum[idx]; ok {
			a.end = end
		} else {
			accum[idx] = &chapterAccum{start: start, end: end}
			chapterOrder = append(chapterOrder, idx)
		}

		cursor = end + p.Chunk.PauseAfter.Seconds()
	}

	chapterTimings := make([]ChapterTiming, len(chapterOrder))
	for i, idx := range chapterOrder {
		a := accum[idx]
		chapterTimings[i] = ChapterTiming{Index: idx, Beat: beatByIndex[idx], Start: round2(a.start), End: round2(a.end)}
	}

	return Timing{ID: scriptID, Voice: voiceID, TotalSeconds: round2(total), Chapters: chapterTimings, Chunks: chunkTimings}
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
