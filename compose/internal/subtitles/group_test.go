package subtitles

import (
	"strings"
	"testing"
)

func wordsFromText(text string, wordDur float64) []Word {
	var words []Word
	start := 0.0
	for _, f := range strings.Fields(text) {
		words = append(words, Word{Text: f, Start: start, End: start + wordDur})
		start += wordDur
	}
	return words
}

func TestGroupLinesBreaksAtSentenceBoundaryEvenBelowMin(t *testing.T) {
	words := wordsFromText("Stop now. Then continue on with more words here after that.", 1)
	lines := GroupLines(words, 4, 7)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (sentence boundary forces a break), got %+v", lines)
	}
	if lines[0].Text != "Stop now." {
		t.Fatalf("expected the first line to end exactly at the sentence boundary, got %q", lines[0].Text)
	}
}

func TestGroupLinesBalancedSplitAvoidsOrphanLastWord(t *testing.T) {
	// 10 words, max 7: greedy would give 7+3; balanced gives 5+5, and an
	// 8-word sentence gives 4+4 (never 7+1, the orphaned-word case).
	lines := GroupLines(wordsFromText("one two three four five six seven eight nine ten", 1), 4, 7)
	if len(lines) != 2 {
		t.Fatalf("expected 2 balanced lines, got %d: %+v", len(lines), lines)
	}
	if lines[0].Text != "one two three four five" || lines[1].Text != "six seven eight nine ten" {
		t.Fatalf("expected a balanced 5+5 split, got %q / %q", lines[0].Text, lines[1].Text)
	}

	eight := GroupLines(wordsFromText("a b c d e f g h", 1), 4, 7)
	if len(eight) != 2 || eight[0].Text != "a b c d" || eight[1].Text != "e f g h" {
		t.Fatalf("expected an 8-word sentence to split 4+4, got %+v", eight)
	}
}

func TestGroupLinesNeverExceedsMax(t *testing.T) {
	lines := GroupLines(wordsFromText("one two three four five six seven eight nine ten eleven twelve thirteen", 1), 4, 9)
	for _, l := range lines {
		if n := len(strings.Fields(l.Text)); n > 9 {
			t.Fatalf("expected no card over 9 words, got %d: %q", n, l.Text)
		}
	}
}

func TestGroupLinesLineSpansItsFirstAndLastWord(t *testing.T) {
	words := []Word{
		{Text: "hi", Start: 0, End: 1},
		{Text: "there.", Start: 1, End: 2.5},
	}
	lines := GroupLines(words, 4, 7)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Start != 0 || lines[0].End != 2.5 {
		t.Fatalf("expected line span [0, 2.5], got [%v, %v]", lines[0].Start, lines[0].End)
	}
}

func TestGroupLinesHandlesTrailingWordsWithNoTerminalPunctuation(t *testing.T) {
	words := wordsFromText("no ending punctuation at all", 1)
	lines := GroupLines(words, 4, 7)
	if len(lines) != 1 || lines[0].Text != "no ending punctuation at all" {
		t.Fatalf("expected the trailing partial line to still be flushed, got %+v", lines)
	}
}

func TestGroupLinesEmptyInputReturnsNoLines(t *testing.T) {
	if lines := GroupLines(nil, 4, 7); len(lines) != 0 {
		t.Fatalf("expected no lines for empty input, got %+v", lines)
	}
}

func TestEndsSentenceHandlesTrailingQuotesAndEllipsis(t *testing.T) {
	if !endsSentence("stop.") {
		t.Fatalf("expected 'stop.' to end a sentence")
	}
	if !endsSentence(`"stop."`) {
		t.Fatalf("expected a closing-quoted sentence end to still be detected")
	}
	if !endsSentence("wait…") {
		t.Fatalf("expected an ellipsis to end a sentence")
	}
	if endsSentence("comma,") {
		t.Fatalf("expected a comma to NOT end a sentence")
	}
	if endsSentence("word") {
		t.Fatalf("expected a bare word to NOT end a sentence")
	}
}
