package generate

import (
	"reflect"
	"strings"
	"testing"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/story"
)

func TestChapterModelUsesDefaultWhenSpecModelUnset(t *testing.T) {
	model, forceModel := chapterModel(config.ChapterSpec{Index: 1, Beat: "pivot"}, "gemini-3.5-flash-lite")
	if model != "gemini-3.5-flash-lite" {
		t.Fatalf("expected default model, got %q", model)
	}
	if forceModel != "" {
		t.Fatalf("expected no ForceModel when spec.Model is unset, got %q", forceModel)
	}
}

func TestChapterModelForcesSpecModelWhenSet(t *testing.T) {
	model, forceModel := chapterModel(config.ChapterSpec{Index: 1, Beat: "hook", Model: "gemini-3.6-flash"}, "gemini-3.5-flash-lite")
	if model != "gemini-3.6-flash" {
		t.Fatalf("expected spec.Model to win, got %q", model)
	}
	if forceModel != "gemini-3.6-flash" {
		t.Fatalf("expected ForceModel to match spec.Model so it survives WithRoleModelOverride, got %q", forceModel)
	}
}

func TestParagraphStartsUsedCollectsEveryDistinctOpening(t *testing.T) {
	prior := []story.Chapter{
		{Index: 1, Text: "The rain fell all night long that year.\n\nSomething else happened after that."},
		{Index: 2, Text: "The rain fell all night long again.\n\nA completely different paragraph here now."},
	}

	got := paragraphStartsUsed(prior)
	want := []string{"the rain fell all night", "something else happened after that.", "a completely different paragraph here"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParagraphStartsUsedSkipsShortParagraphs(t *testing.T) {
	prior := []story.Chapter{{Index: 1, Text: "Too short.\n\nThis one has enough words in it though."}}
	got := paragraphStartsUsed(prior)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 paragraph opening (the short one skipped), got %v", got)
	}
}

func TestPhrasesAtRepetitionLimitOnlyIncludesPhrasesUsedTwice(t *testing.T) {
	prior := []story.Chapter{
		{Index: 1, Text: "the ledger never once balanced correctly that year"},
		{Index: 2, Text: "she said the ledger never once balanced correctly again"},
	}
	got := phrasesAtRepetitionLimit(prior)
	found := false
	for _, g := range got {
		if g == "the ledger never once balanced correctly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the twice-used phrase to be included, got %v", got)
	}
}

func TestPhrasesAtRepetitionLimitExcludesPhraseUsedOnlyOnce(t *testing.T) {
	prior := []story.Chapter{{Index: 1, Text: "the ledger never once balanced correctly that year"}}
	got := phrasesAtRepetitionLimit(prior)
	if len(got) != 0 {
		t.Fatalf("expected no phrases at the limit after only 1 use, got %v", got)
	}
}

func TestSentenceOpeningsAtLimitOnlyIncludesOpeningsUsedEightTimes(t *testing.T) {
	// "She said" eight times across two chapters; a distinct opening once.
	text := strings.Repeat("She said nothing at all. ", 4)
	prior := []story.Chapter{
		{Index: 1, Text: text + "He waited by the door."},
		{Index: 2, Text: text},
	}
	got := sentenceOpeningsAtLimit(prior)
	want := []string{"she said"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSentenceOpeningsAtLimitExcludesOpeningUsedSevenTimes(t *testing.T) {
	text := strings.Repeat("She said nothing at all. ", 3) + "She said we were done."
	prior := []story.Chapter{{Index: 1, Text: text}}                                                     // 4 uses
	prior = append(prior, story.Chapter{Index: 2, Text: strings.Repeat("She said it again today. ", 3)}) // +3 = 7 total
	got := sentenceOpeningsAtLimit(prior)
	if len(got) != 0 {
		t.Fatalf("expected no openings at the limit after only 7 uses, got %v", got)
	}
}

func TestSentenceOpeningsAtLimitSkipsOneWordSentences(t *testing.T) {
	prior := []story.Chapter{{Index: 1, Text: "Stop. She said nothing."}}
	got := sentenceOpeningsAtLimit(prior)
	if len(got) != 0 {
		t.Fatalf("expected no violations from a single one-word sentence, got %v", got)
	}
}

func TestMoneyMentionCountsUsedFormatsRunningCount(t *testing.T) {
	prior := []story.Chapter{
		{Index: 1, DisplayText: "She kept $1,200 hidden in the box."},
		{Index: 2, DisplayText: "The same $1,200 resurfaced a year later."},
	}
	got := moneyMentionCountsUsed(prior)
	want := []string{"$1,200 (mentioned 2 time(s) already)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMoneyMentionCountsUsedIgnoresTextField(t *testing.T) {
	// DisplayText has the exact digit amount; Text (TTS) never does — the
	// function must read DisplayText, matching
	// story.ValidateMoneyAmountRepetition's own field.
	prior := []story.Chapter{{Index: 1, Text: "She kept twelve hundred dollars hidden.", DisplayText: ""}}
	got := moneyMentionCountsUsed(prior)
	if len(got) != 0 {
		t.Fatalf("expected no money mentions found via Text alone, got %v", got)
	}
}

func TestAvoidContextEmptyForNoPriorChapters(t *testing.T) {
	if got := paragraphStartsUsed(nil); len(got) != 0 {
		t.Fatalf("expected no paragraph starts with no prior chapters, got %v", got)
	}
	if got := phrasesAtRepetitionLimit(nil); len(got) != 0 {
		t.Fatalf("expected no phrases with no prior chapters, got %v", got)
	}
	if got := moneyMentionCountsUsed(nil); len(got) != 0 {
		t.Fatalf("expected no money mentions with no prior chapters, got %v", got)
	}
	if got := sentenceOpeningsAtLimit(nil); len(got) != 0 {
		t.Fatalf("expected no sentence openings with no prior chapters, got %v", got)
	}
}
