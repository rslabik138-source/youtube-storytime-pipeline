// Package subtitles builds an ASS subtitle file from voiceover's
// timing.json. Kokoro (and voiceover's own pipeline) reports no
// word-level alignment — only each chunk's whole-block [start, end] and
// its text (see manifest.ChunkTiming's doc comment) — so per-word
// timestamps here are ESTIMATED by distributing a chunk's duration across
// its words proportional to word length, not measured. This is accurate
// at chunk boundaries (real ffprobe-measured audio) and approximate
// mid-chunk; a chunk commonly spans many sentences and tens of seconds,
// so drift within one chunk is real but bounded.
package subtitles

import (
	"strings"

	"github.com/placeholder/compose/internal/manifest"
)

// Word is one estimated word-level timing.
type Word struct {
	Text  string
	Start float64
	End   float64
}

// InterpolateChunk splits chunk.Text on whitespace and distributes
// chunk.End-chunk.Start proportionally across the words by raw token
// length (a longer word/token takes proportionally longer to speak) —
// the standard fallback technique when only block-level timing exists.
// Empty text or a zero/negative span returns nil.
func InterpolateChunk(chunk manifest.ChunkTiming) []Word {
	fields := strings.Fields(chunk.Text)
	duration := chunk.End - chunk.Start
	if len(fields) == 0 || duration <= 0 {
		return nil
	}

	totalLen := 0
	for _, f := range fields {
		totalLen += len(f)
	}
	if totalLen == 0 {
		return nil
	}

	words := make([]Word, len(fields))
	cursor := chunk.Start
	for i, f := range fields {
		share := float64(len(f)) / float64(totalLen) * duration
		words[i] = Word{Text: f, Start: cursor, End: cursor + share}
		cursor += share
	}
	// Floating-point accumulation can leave the last word's End a hair off
	// chunk.End — pin it exactly so consecutive chunks never gap or overlap.
	words[len(words)-1].End = chunk.End
	return words
}

// InterpolateAll runs InterpolateChunk over every chunk in order,
// producing one flat word timeline for the whole script.
func InterpolateAll(chunks []manifest.ChunkTiming) []Word {
	var out []Word
	for _, c := range chunks {
		out = append(out, InterpolateChunk(c)...)
	}
	return out
}
