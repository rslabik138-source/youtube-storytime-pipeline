package subtitles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTrackRendersCardsAndConcatList(t *testing.T) {
	dir := t.TempDir()
	lines := []Line{
		{Text: "first line", Start: 0, End: 2},
		{Text: "second line", Start: 2.5, End: 4}, // a 0.5s gap before it
	}
	res, err := WriteTrack(lines, testCardStyle(), 1920, 1080, 10, dir)
	if err != nil {
		t.Fatalf("WriteTrack: %v", err)
	}
	if res.CardCount != 2 {
		t.Fatalf("expected 2 cards, got %d", res.CardCount)
	}
	for _, name := range []string{"gap.png", "card_0.png", "card_1.png", "captions.ffconcat"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to be written: %v", name, err)
		}
	}

	list, err := os.ReadFile(res.ConcatPath)
	if err != nil {
		t.Fatalf("read concat list: %v", err)
	}
	s := string(list)
	if !strings.HasPrefix(s, "ffconcat version 1.0") {
		t.Fatalf("expected an ffconcat header, got:\n%s", s)
	}
	// Timeline: gap(0->0? none) card_0(0-2) gap(2-2.5) card_1(2.5-4) gap(4-10).
	for _, want := range []string{
		"file 'card_0.png'\nduration 2.000",
		"file 'gap.png'\nduration 0.500",
		"file 'card_1.png'\nduration 1.500",
		"file 'gap.png'\nduration 6.000", // 10 - 4
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected concat list to contain %q, got:\n%s", want, s)
		}
	}
	// The concat quirk fix: a final bare file line so the last duration is honored.
	if !strings.HasSuffix(strings.TrimSpace(s), "file 'gap.png'") {
		t.Fatalf("expected a trailing bare file line, got:\n%s", s)
	}
}

func TestWriteTrackClampsToDurationWindow(t *testing.T) {
	dir := t.TempDir()
	lines := []Line{
		{Text: "in window", Start: 0, End: 5},
		{Text: "straddles the cap", Start: 5, End: 12}, // clamped to end at 10
		{Text: "entirely past", Start: 15, End: 20},    // dropped
	}
	res, err := WriteTrack(lines, testCardStyle(), 1920, 1080, 10, dir)
	if err != nil {
		t.Fatalf("WriteTrack: %v", err)
	}
	if res.CardCount != 2 {
		t.Fatalf("expected 2 cards (the past-the-window one dropped), got %d", res.CardCount)
	}
	list, _ := os.ReadFile(res.ConcatPath)
	if !strings.Contains(string(list), "file 'card_1.png'\nduration 5.000") { // 10 - 5
		t.Fatalf("expected the straddling card clamped to 5s, got:\n%s", string(list))
	}
	if strings.Contains(string(list), "card_2") {
		t.Fatalf("expected no card for the entirely-past-window line, got:\n%s", string(list))
	}
}

func TestWriteTrackNoLinesStillCoversTheTimeline(t *testing.T) {
	dir := t.TempDir()
	res, err := WriteTrack(nil, testCardStyle(), 1920, 1080, 8, dir)
	if err != nil {
		t.Fatalf("WriteTrack: %v", err)
	}
	if res.CardCount != 0 {
		t.Fatalf("expected 0 cards, got %d", res.CardCount)
	}
	list, _ := os.ReadFile(res.ConcatPath)
	if !strings.Contains(string(list), "file 'gap.png'\nduration 8.000") {
		t.Fatalf("expected a single full-length gap covering the whole timeline, got:\n%s", string(list))
	}
}
