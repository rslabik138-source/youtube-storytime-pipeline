// Package manifest reads the ONLY thing compose is allowed to know about
// voiceover's output: timing.json, the file pair (alongside voice.wav)
// voiceover's assembly step writes. No shared database, no direct
// dependency on the voiceover module — the file is the entire contract.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ChapterTiming is one chapter's span in the finished voiceover.
type ChapterTiming struct {
	Index int     `json:"index"`
	Beat  string  `json:"beat"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// ChunkTiming is one chunk's span in the finished voiceover, plus its own
// text. This is the FINEST granularity voiceover's pipeline actually
// produces — a chunk is one TTS-request-sized block (commonly several
// sentences, tens of seconds), not a word or a sentence. Kokoro reports no
// word-level alignment; voiceover's own Start/End come from ffprobe on
// real rendered audio, never a word-count estimate (see
// voiceover/internal/assemble.ChunkTiming's own doc comment). Anything
// finer than this — subtitles/internal/interpolate builds by splitting
// Text proportionally across [Start, End] — is an approximation, not
// measured timing.
type ChunkTiming struct {
	Index   int     `json:"index"`
	Chapter int     `json:"chapter"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
}

// Timing mirrors voiceover's internal/assemble.Timing — timing.json's
// shape. Changing this struct's JSON tags is a cross-module contract
// change, not a local refactor.
type Timing struct {
	ID           string          `json:"id"`
	Voice        string          `json:"voice"`
	TotalSeconds float64         `json:"total_seconds"`
	Chapters     []ChapterTiming `json:"chapters"`
	Chunks       []ChunkTiming   `json:"chunks"`
}

var (
	// ErrNotFound means timing.json is missing — voiceover was never run
	// for this ID, or the path points somewhere else.
	ErrNotFound = errors.New("manifest: timing.json not found")
	// ErrInvalid means timing.json exists but doesn't parse, or fails a
	// basic sanity check (no chunks — nothing to build subtitles from).
	ErrInvalid = errors.New("manifest: invalid timing.json")
)

// LoadTiming reads and validates path (voiceover/output/<id>/timing.json).
func LoadTiming(path string) (*Timing, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var t Timing
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w: %w", path, ErrInvalid, err)
	}
	if t.TotalSeconds <= 0 {
		return nil, fmt.Errorf("%s: %w: total_seconds must be > 0", path, ErrInvalid)
	}
	if len(t.Chunks) == 0 {
		return nil, fmt.Errorf("%s: %w: no chunks — nothing to build subtitles from", path, ErrInvalid)
	}
	return &t, nil
}
