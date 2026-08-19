package chunk

import (
	"strings"
	"testing"
	"time"

	"github.com/placeholder/voiceover/internal/manifest"
)

// bundleOf builds a manifest.Bundle from chapter texts, computing char
// ranges the same way scenario's Bundle() does (chapters joined by "\n\n\n").
func bundleOf(chapterTexts ...string) *manifest.Bundle {
	var sb strings.Builder
	var chapters []manifest.Chapter
	offset := 0
	for i, text := range chapterTexts {
		start := offset
		sb.WriteString(text)
		offset += len(text)
		chapters = append(chapters, manifest.Chapter{Index: i + 1, Beat: "beat", CharStart: start, CharEnd: offset})
		if i < len(chapterTexts)-1 {
			sb.WriteString("\n\n\n")
			offset += len("\n\n\n")
		}
	}
	return &manifest.Bundle{
		Text:     sb.String(),
		Manifest: manifest.Manifest{ID: "s1", WordCount: 1, Narrator: manifest.Narrator{Name: "N"}, Chapters: chapters},
	}
}

func TestSplitNeverBreaksMidSentence(t *testing.T) {
	b := bundleOf("The first sentence is here. The second sentence follows it. And a third one closes the paragraph.")
	chunks, err := Split(b, Options{MaxChars: 40})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatalf("expected at least one chunk")
	}
	for _, c := range chunks {
		trimmed := strings.TrimSpace(c.Text)
		last := trimmed[len(trimmed)-1]
		if last != '.' && last != '!' && last != '?' {
			t.Fatalf("chunk %d doesn't end on a sentence boundary: %q", c.Index, c.Text)
		}
	}
	// Reassembling every chunk's text must reproduce every original
	// sentence intact — nothing lost, nothing split.
	joined := strings.Join(chunkTexts(chunks), " ")
	for _, want := range []string{"The first sentence is here.", "The second sentence follows it.", "And a third one closes the paragraph."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected reassembled text to contain %q, got: %s", want, joined)
		}
	}
}

func chunkTexts(chunks []Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Text
	}
	return out
}

func TestSplitPacksSentencesUpToMaxChars(t *testing.T) {
	// Each sentence is ~20 chars; MaxChars 45 should fit exactly 2 per chunk.
	b := bundleOf("Short sentence one. Short sentence two. Short sentence three.")
	chunks, err := Split(b, Options{MaxChars: 45})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (2 sentences + 1 sentence), got %d: %+v", len(chunks), chunks)
	}
	if strings.Count(chunks[0].Text, ".") != 2 {
		t.Fatalf("expected the first chunk to hold 2 sentences, got: %q", chunks[0].Text)
	}
	if strings.Count(chunks[1].Text, ".") != 1 {
		t.Fatalf("expected the second chunk to hold 1 sentence, got: %q", chunks[1].Text)
	}
}

func TestSplitOversizedSingleSentenceBecomesOwnChunk(t *testing.T) {
	longSentence := "This single sentence is deliberately much longer than the configured max character budget for one chunk."
	b := bundleOf(longSentence)
	chunks, err := Split(b, Options{MaxChars: 20})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk (never split mid-sentence even when oversized), got %d", len(chunks))
	}
	if chunks[0].Text != longSentence {
		t.Fatalf("expected the oversized sentence untouched, got: %q", chunks[0].Text)
	}
}

func TestSplitPauseAfterAtParagraphAndChapterBoundaries(t *testing.T) {
	chapter1 := "First paragraph, first sentence.\n\nSecond paragraph, first sentence."
	chapter2 := "Third paragraph, only sentence."
	b := bundleOf(chapter1, chapter2)

	chunks, err := Split(b, Options{MaxChars: 400, ParagraphPauseMs: 350, ChapterPauseMs: 700})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 paragraphs in chapter 1 + 1 in chapter 2), got %d: %+v", len(chunks), chunks)
	}

	if chunks[0].PauseAfter != 350*time.Millisecond {
		t.Fatalf("expected 350ms after chapter 1's first (non-last) paragraph, got %v", chunks[0].PauseAfter)
	}
	if chunks[0].ChapterIdx != 1 {
		t.Fatalf("expected chunk 0 tied to chapter 1, got %d", chunks[0].ChapterIdx)
	}
	if chunks[1].PauseAfter != 700*time.Millisecond {
		t.Fatalf("expected 700ms after chapter 1's last paragraph (chapter boundary), got %v", chunks[1].PauseAfter)
	}
	if chunks[2].PauseAfter != 0 {
		t.Fatalf("expected 0 after the script's very last chunk, got %v", chunks[2].PauseAfter)
	}
	if chunks[2].ChapterIdx != 2 {
		t.Fatalf("expected the last chunk tied to chapter 2, got %d", chunks[2].ChapterIdx)
	}
}

func TestSplitZeroPauseWithinAParagraphSplitAcrossChunks(t *testing.T) {
	// One paragraph, several sentences, forced into multiple chunks by a
	// tight MaxChars — none of the internal splits should carry a pause.
	b := bundleOf("Sentence number one here. Sentence number two here. Sentence number three here. Sentence number four here.")
	chunks, err := Split(b, Options{MaxChars: 30, ParagraphPauseMs: 350, ChapterPauseMs: 700})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected the tight MaxChars to force multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks[:len(chunks)-1] {
		if c.PauseAfter != 0 {
			t.Fatalf("expected 0 pause between chunks inside the same paragraph, got %v for chunk %d", c.PauseAfter, c.Index)
		}
	}
	last := chunks[len(chunks)-1]
	if last.PauseAfter != 0 {
		t.Fatalf("expected 0 pause after the script's last chunk, got %v", last.PauseAfter)
	}
}

func TestSplitDefaultsWhenOptionsZero(t *testing.T) {
	b := bundleOf("One sentence here.")
	chunks, err := Split(b, Options{})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestSplitInvalidChapterRangeReturnsError(t *testing.T) {
	b := &manifest.Bundle{
		Text: "short",
		Manifest: manifest.Manifest{
			Chapters: []manifest.Chapter{{Index: 1, CharStart: 0, CharEnd: 999}},
		},
	}
	if _, err := Split(b, Options{}); err == nil {
		t.Fatalf("expected an error for a chapter range outside the text")
	}
}
