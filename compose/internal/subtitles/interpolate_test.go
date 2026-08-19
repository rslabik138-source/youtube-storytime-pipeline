package subtitles

import (
	"testing"

	"github.com/placeholder/compose/internal/manifest"
)

func TestInterpolateChunkDistributesProportionally(t *testing.T) {
	chunk := manifest.ChunkTiming{Start: 10, End: 20, Text: "a bb ccc"}
	words := InterpolateChunk(chunk)
	if len(words) != 3 {
		t.Fatalf("expected 3 words, got %d", len(words))
	}
	// total len = 1+2+3 = 6, duration = 10 -> "a"=10/6, "bb"=20/6, "ccc"=30/6
	if words[0].Start != 10 {
		t.Fatalf("expected first word to start at chunk start, got %v", words[0].Start)
	}
	wantFirstEnd := 10 + 10.0/6.0
	if diff := words[0].End - wantFirstEnd; diff > 0.001 || diff < -0.001 {
		t.Fatalf("expected first word to end around %v, got %v", wantFirstEnd, words[0].End)
	}
	if words[len(words)-1].End != 20 {
		t.Fatalf("expected the last word's End pinned exactly to chunk.End, got %v", words[len(words)-1].End)
	}
}

func TestInterpolateChunkWordsAreContiguous(t *testing.T) {
	chunk := manifest.ChunkTiming{Start: 0, End: 5, Text: "one two three four"}
	words := InterpolateChunk(chunk)
	for i := 1; i < len(words); i++ {
		if words[i].Start != words[i-1].End {
			t.Fatalf("expected word %d to start exactly where word %d ends, got %v vs %v",
				i, i-1, words[i].Start, words[i-1].End)
		}
	}
}

func TestInterpolateChunkEmptyTextReturnsNil(t *testing.T) {
	if words := InterpolateChunk(manifest.ChunkTiming{Start: 0, End: 5, Text: "   "}); words != nil {
		t.Fatalf("expected nil for whitespace-only text, got %v", words)
	}
}

func TestInterpolateChunkZeroDurationReturnsNil(t *testing.T) {
	if words := InterpolateChunk(manifest.ChunkTiming{Start: 5, End: 5, Text: "hello"}); words != nil {
		t.Fatalf("expected nil for zero-duration chunk, got %v", words)
	}
}

func TestInterpolateAllConcatenatesEveryChunkInOrder(t *testing.T) {
	chunks := []manifest.ChunkTiming{
		{Start: 0, End: 2, Text: "first chunk"},
		{Start: 2, End: 4, Text: "second chunk"},
	}
	words := InterpolateAll(chunks)
	if len(words) != 4 {
		t.Fatalf("expected 4 words total, got %d", len(words))
	}
	if words[0].Text != "first" || words[3].Text != "chunk" {
		t.Fatalf("unexpected word order: %+v", words)
	}
}
