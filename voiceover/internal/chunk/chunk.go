// Package chunk breaks a scenario bundle's script text into TTS-sized
// pieces. Kokoro's hard limit is 510 tokens per call; chunk_max_chars
// (~400, never more than 500) is a character-count proxy comfortably under
// that, chosen so a chunk boundary can always land on a sentence break
// instead of relying on the model to truncate mid-thought.
package chunk

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/placeholder/voiceover/internal/manifest"
)

// Chunk is one TTS call's worth of text. PauseAfter is the silence to
// insert after this chunk during assembly: 0 inside a paragraph, the
// configured paragraph pause between paragraphs, the configured chapter
// pause between chapters, and 0 after the script's very last chunk.
type Chunk struct {
	Index      int
	Text       string
	ChapterIdx int
	PauseAfter time.Duration
}

// Options controls chunking. The zero value is invalid input to Split's
// internal math but withDefaults() below fills in the same defaults
// settings.yaml documents (400/350/700), so a caller that only sets one
// field doesn't have to know the other two.
type Options struct {
	MaxChars         int
	ParagraphPauseMs int
	ChapterPauseMs   int
}

func (o Options) withDefaults() Options {
	if o.MaxChars <= 0 {
		o.MaxChars = 400
	}
	if o.ParagraphPauseMs <= 0 {
		o.ParagraphPauseMs = 350
	}
	if o.ChapterPauseMs <= 0 {
		o.ChapterPauseMs = 700
	}
	return o
}

// sentenceEnd matches a run of sentence-terminating punctuation followed by
// whitespace or end-of-string — same window scenario's own sentence
// splitting uses, except here the terminator stays attached to the
// sentence it belongs to (chunk reassembles sentences into TTS-ready text;
// it doesn't need them punctuation-stripped for validator matching the way
// scenario's story.SplitSentences does).
var sentenceEnd = regexp.MustCompile(`[.!?…]+(\s+|$)`)

func splitSentences(text string) []string {
	var out []string
	last := 0
	for _, loc := range sentenceEnd.FindAllStringIndex(text, -1) {
		if sentence := strings.TrimSpace(text[last:loc[1]]); sentence != "" {
			out = append(out, sentence)
		}
		last = loc[1]
	}
	if rest := strings.TrimSpace(text[last:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

func splitParagraphs(text string) []string {
	raw := strings.Split(text, "\n\n")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Split breaks bundle's script text into Chunks, chapter by chapter (in
// Manifest.Chapters order — scenario always emits that in increasing
// char-offset order, one entry per chapter, so ChapterIdx and PauseAfter's
// "is this the last chapter" check can rely on slice position directly).
// A chunk never crosses a sentence boundary: sentences are packed
// greedily up to opts.MaxChars, and a single sentence longer than
// MaxChars still becomes its own (oversized) chunk rather than being cut
// — Kokoro's real limit is 510 tokens, comfortably above what a ~400-char
// English sentence needs, so this is a rare, tolerable overflow, not a
// silent truncation.
func Split(b *manifest.Bundle, opts Options) ([]Chunk, error) {
	opts = opts.withDefaults()

	var chunks []Chunk
	idx := 0
	chapters := b.Manifest.Chapters

	for ci, ch := range chapters {
		if ch.CharStart < 0 || ch.CharEnd > len(b.Text) || ch.CharStart > ch.CharEnd {
			return nil, fmt.Errorf("chunk: chapter %d has an invalid char range [%d,%d) for a %d-byte script",
				ch.Index, ch.CharStart, ch.CharEnd, len(b.Text))
		}
		isLastChapter := ci == len(chapters)-1
		paragraphs := splitParagraphs(b.Text[ch.CharStart:ch.CharEnd])

		for pi, para := range paragraphs {
			isLastParagraph := pi == len(paragraphs)-1
			sentences := splitSentences(para)
			if len(sentences) == 0 {
				continue
			}

			var group []string
			flush := func(isEndOfParagraph bool) {
				if len(group) == 0 {
					return
				}
				var pause time.Duration
				if isEndOfParagraph {
					switch {
					case isLastParagraph && isLastChapter:
						pause = 0
					case isLastParagraph:
						pause = time.Duration(opts.ChapterPauseMs) * time.Millisecond
					default:
						pause = time.Duration(opts.ParagraphPauseMs) * time.Millisecond
					}
				}
				chunks = append(chunks, Chunk{
					Index: idx, Text: strings.Join(group, " "), ChapterIdx: ch.Index, PauseAfter: pause,
				})
				idx++
				group = nil
			}

			for si, sent := range sentences {
				if len(group) > 0 {
					candidateLen := len(strings.Join(group, " ")) + 1 + len(sent)
					if candidateLen > opts.MaxChars {
						flush(false)
					}
				}
				group = append(group, sent)

				if si == len(sentences)-1 {
					flush(true)
				}
			}
		}
	}

	return chunks, nil
}
