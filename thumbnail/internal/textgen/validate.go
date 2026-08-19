package textgen

import (
	"fmt"
	"strings"
)

var allowedColors = map[string]bool{
	"white": true, "yellow": true, "green": true, "magenta": true, "cyan": true, "red": true,
}

// Structural bounds for the competitor-style dense mini-story. This is a
// deliberately text-heavy block (the auto-fit renderer shrinks it to fit),
// so the caps are generous — the point is a readable little STORY with a
// verbatim quote and a cut-off cliffhanger, not a few big words.
const (
	minTotalLines = 6  // a real story arc: setup, cruelty, restraint, turn, cliffhanger
	maxTotalLines = 10 // including the final cliffhanger line
	MinTotalWords = 30 // dense enough to read as a story, not a headline
	// MaxTotalWords keeps the block from overflowing even at the smallest
	// legible font. Exported so the mobile-check can reuse the threshold.
	MaxTotalWords = 68
	// maxNonWhiteColors: the competitor thumbnails run up to four loud accent
	// colors over white (e.g. yellow + magenta + green + red), so allow four
	// — three was too tight for the dense multi-beat story and forced needless
	// retries.
	maxNonWhiteColors = 4
)

// Validate checks t against prompts/thumbnail.tmpl's own rules — the same
// rules the prompt states, checked mechanically rather than trusted,
// exactly like scenario's chapter/bible validators. A violation's message
// is meant to be fed back into a retry prompt, not just logged.
func Validate(t ThumbnailText) []string {
	var violations []string

	total := len(t.Lines) + 1 // +1 for FinalLine, always present in a valid response
	if strings.TrimSpace(t.FinalLine) == "" {
		violations = append(violations, "final_line is empty")
		total = len(t.Lines)
	}
	if total < minTotalLines || total > maxTotalLines {
		violations = append(violations, fmt.Sprintf("expected %d to %d total lines (including final_line), got %d — this is a dense mini-story, not a headline", minTotalLines, maxTotalLines, total))
	}

	wordCount := len(strings.Fields(t.FinalLine))
	nonWhiteColors := map[string]bool{}
	hasQuote := strings.Contains(t.FinalLine, "\"")
	for i, l := range t.Lines {
		if strings.TrimSpace(l.Text) == "" {
			violations = append(violations, fmt.Sprintf("line %d has empty text", i+1))
		}
		if !allowedColors[l.Color] {
			violations = append(violations, fmt.Sprintf("line %d has invalid color %q (must be one of white, yellow, green, magenta, cyan, red)", i+1, l.Color))
		} else if l.Color != "white" {
			nonWhiteColors[l.Color] = true
		}
		if strings.Contains(l.Text, "\"") {
			hasQuote = true
		}
		wordCount += len(strings.Fields(l.Text))
	}
	if len(nonWhiteColors) > maxNonWhiteColors {
		violations = append(violations, fmt.Sprintf("uses %d distinct non-white colors, must be at most %d", len(nonWhiteColors), maxNonWhiteColors))
	}
	if wordCount < MinTotalWords {
		violations = append(violations, fmt.Sprintf("total word count %d is too sparse, must be at least %d — make it a dense story", wordCount, MinTotalWords))
	}
	if wordCount > MaxTotalWords {
		violations = append(violations, fmt.Sprintf("total word count %d must be %d or fewer", wordCount, MaxTotalWords))
	}
	if !hasQuote {
		violations = append(violations, "must include at least one verbatim quote in real double quotes (the antagonist's cruel words)")
	}

	return violations
}
