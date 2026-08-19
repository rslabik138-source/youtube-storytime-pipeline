package generate

import (
	"context"
	"strings"
	"testing"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/store"
	"github.com/placeholder/scenario/internal/story"
)

func TestSentenceSpanFindsCorrectOccurrenceNotTheFirst(t *testing.T) {
	text := "The cat sat. The cat sat. The dog ran."
	sentences := story.SplitSentences(text)

	// Locate the SECOND "The cat sat" starting the search right after the
	// first one's end, not from byte 0 — otherwise both would resolve to
	// the same span.
	firstStart, firstEnd, ok := sentenceSpan(text, sentences[0], 0)
	if !ok {
		t.Fatalf("expected to find the first occurrence")
	}
	secondStart, secondEnd, ok := sentenceSpan(text, sentences[1], firstEnd)
	if !ok {
		t.Fatalf("expected to find the second occurrence")
	}
	if secondStart <= firstStart {
		t.Fatalf("expected the second occurrence to start after the first: first=[%d,%d) second=[%d,%d)", firstStart, firstEnd, secondStart, secondEnd)
	}
	if text[secondStart:secondEnd] != "The cat sat." {
		t.Fatalf("expected the located span to include the trailing period, got %q", text[secondStart:secondEnd])
	}
}

func TestReplaceSentenceAtReplacesOnlyTargetSentence(t *testing.T) {
	text := "First sentence here. Second sentence here. Third sentence here."
	sentences := story.SplitSentences(text)

	got, ok := replaceSentenceAt(text, sentences, 1, "Middle rewritten.")
	if !ok {
		t.Fatalf("expected replaceSentenceAt to succeed")
	}
	want := "First sentence here. Middle rewritten. Third sentence here."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceSentenceAtPreservesParagraphBreaks(t *testing.T) {
	text := "First paragraph sentence.\n\nSecond paragraph sentence."
	sentences := story.SplitSentences(text)

	got, ok := replaceSentenceAt(text, sentences, 1, "Rewritten second paragraph sentence.")
	if !ok {
		t.Fatalf("expected replaceSentenceAt to succeed")
	}
	want := "First paragraph sentence.\n\nRewritten second paragraph sentence."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceSentenceAtOutOfRangeFails(t *testing.T) {
	text := "Only one sentence here."
	sentences := story.SplitSentences(text)
	if _, ok := replaceSentenceAt(text, sentences, 5, "replacement"); ok {
		t.Fatalf("expected an out-of-range index to fail")
	}
}

func TestLocateSentenceIndexFindsFirstMatchCaseInsensitive(t *testing.T) {
	sentences := []string{"Nothing here", "The LEDGER never balanced", "Something else"}
	idx := locateSentenceIndex(sentences, "the ledger never balanced")
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}
}

func TestLocateSentenceIndexNoMatchReturnsNegativeOne(t *testing.T) {
	sentences := []string{"Nothing here", "Something else"}
	if idx := locateSentenceIndex(sentences, "not present anywhere"); idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}
}

func TestKeepSentencesAtDropsUnmarkedSentences(t *testing.T) {
	text := "Keep this one. Drop this one. Keep this too."
	sentences := story.SplitSentences(text)

	got, ok := keepSentencesAt(text, sentences, []int{0, 2})
	if !ok {
		t.Fatalf("expected keepSentencesAt to succeed")
	}
	want := "Keep this one. Keep this too."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDedupeExactRepeatedSentencesRemovesLaterDuplicateAcrossChapters(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1, Text: "A unique opening line. The exact same sentence appears here."},
		{Index: 2, Text: "Another unique line. The exact same sentence appears here. A closing unique line."},
	}}

	removed := dedupeExactRepeatedSentences(script)
	if removed != 1 {
		t.Fatalf("expected exactly 1 sentence removed, got %d", removed)
	}
	if strings.Contains(strings.ToLower(script.Chapters[1].Text), "the exact same sentence appears here") {
		t.Fatalf("expected chapter 2's duplicate to be removed, got: %s", script.Chapters[1].Text)
	}
	if !strings.Contains(script.Chapters[1].Text, "Another unique line.") || !strings.Contains(script.Chapters[1].Text, "A closing unique line.") {
		t.Fatalf("expected chapter 2's other sentences to survive untouched, got: %s", script.Chapters[1].Text)
	}
	if !strings.Contains(script.Chapters[0].Text, "The exact same sentence appears here.") {
		t.Fatalf("expected chapter 1 (the first occurrence) to keep its sentence, got: %s", script.Chapters[0].Text)
	}
}

func TestDedupeExactRepeatedSentencesIgnoresCaseAndWhitespaceDifferences(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1, Text: "An opening line. The Ledger  Never   Balanced."},
		{Index: 2, Text: "the ledger never balanced. A closing line."},
	}}

	removed := dedupeExactRepeatedSentences(script)
	if removed != 1 {
		t.Fatalf("expected 1 removal despite case/whitespace differences, got %d", removed)
	}
}

func TestDedupeExactRepeatedSentencesNoChangeWhenNothingRepeats(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1, Text: "First unique sentence. Second unique sentence."},
		{Index: 2, Text: "Third unique sentence. Fourth unique sentence."},
	}}
	original := script.Chapters[0].Text + script.Chapters[1].Text

	if removed := dedupeExactRepeatedSentences(script); removed != 0 {
		t.Fatalf("expected 0 removals, got %d", removed)
	}
	if script.Chapters[0].Text+script.Chapters[1].Text != original {
		t.Fatalf("expected chapters to be left untouched")
	}
}

func TestDedupeExactRepeatedSentencesKeepsDisplayTextAligned(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1, Text: "An opening line. A repeated sentence.", DisplayText: "An opening line. A repeated sentence."},
		{Index: 2, Text: "A repeated sentence. A closing line.", DisplayText: "A repeated sentence. A closing line."},
	}}

	dedupeExactRepeatedSentences(script)

	if script.Chapters[1].Text != script.Chapters[1].DisplayText {
		t.Fatalf("expected Text and DisplayText to stay aligned after dedup, got text=%q display=%q",
			script.Chapters[1].Text, script.Chapters[1].DisplayText)
	}
}

func TestFixMissingPunctuationInsertsPeriodBeforeCapitalizedPronoun(t *testing.T) {
	text := "She set the jar down on a Tuesday morning She paused to wave at the mailman."
	fixed, n := fixMissingPunctuation(text)
	if n != 1 {
		t.Fatalf("expected 1 insertion, got %d", n)
	}
	if !strings.Contains(fixed, "morning. She paused") {
		t.Fatalf("expected a period inserted before the capitalized pronoun, got: %s", fixed)
	}
}

func TestFixMissingPunctuationLeavesProperlyPunctuatedTextUntouched(t *testing.T) {
	text := "She set the jar down on a Tuesday morning. She paused to wave at the mailman."
	fixed, n := fixMissingPunctuation(text)
	if n != 0 {
		t.Fatalf("expected 0 insertions, got %d", n)
	}
	if fixed != text {
		t.Fatalf("expected text unchanged, got: %s", fixed)
	}
}

func TestFixMissingPunctuationInScriptFixesTextAndDisplayTextIndependently(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1,
			Text:        "It was a quiet morning She never expected the letter.",
			DisplayText: "It was a quiet morning She never expected the letter.",
		},
	}}

	total := fixMissingPunctuationInScript(script)
	if total != 1 {
		t.Fatalf("expected 1 total insertion, got %d", total)
	}
	if !strings.Contains(script.Chapters[0].Text, "morning. She never") {
		t.Fatalf("expected Text fixed, got: %s", script.Chapters[0].Text)
	}
	if !strings.Contains(script.Chapters[0].DisplayText, "morning. She never") {
		t.Fatalf("expected DisplayText fixed too, got: %s", script.Chapters[0].DisplayText)
	}
}

func oneChapterSpec() config.Chapters {
	return config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}
}

func TestPointFixChapterFallsBackWhenPhraseNotLocatable(t *testing.T) {
	mainClient := llm.NewFakeClient() // no calls expected: gives up before ever calling the LLM
	st := store.NewMemoryStore()
	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), oneChapterSpec())

	script := &story.Script{Chapters: []story.Chapter{{Index: 1, Text: "Some text here.", DisplayText: "Some text here."}}}
	violations := []story.Violation{{Rule: "refrain_phrase_placement", Chapter: 1, Phrase: ""}}

	fixed, err := o.pointFixChapter(context.Background(), script, 1, violations)
	if err != nil {
		t.Fatalf("pointFixChapter: %v", err)
	}
	if fixed {
		t.Fatalf("expected pointFixChapter to give up when a violation has no locatable phrase")
	}
	if len(mainClient.Calls) != 0 {
		t.Fatalf("expected no LLM calls when falling back immediately, got %d", len(mainClient.Calls))
	}
	if script.Chapters[0].Text != "Some text here." {
		t.Fatalf("expected the chapter to be left untouched, got %q", script.Chapters[0].Text)
	}
}

func TestPointFixChapterFallsBackWhenPhraseNotFoundInText(t *testing.T) {
	mainClient := llm.NewFakeClient()
	st := store.NewMemoryStore()
	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), oneChapterSpec())

	script := &story.Script{Chapters: []story.Chapter{{Index: 1, Text: "Some text here.", DisplayText: "Some text here."}}}
	violations := []story.Violation{{Rule: "repeated_ngram", Chapter: 1, Phrase: "phrase that does not appear"}}

	fixed, err := o.pointFixChapter(context.Background(), script, 1, violations)
	if err != nil {
		t.Fatalf("pointFixChapter: %v", err)
	}
	if fixed {
		t.Fatalf("expected pointFixChapter to give up when the phrase isn't found in the chapter's text")
	}
}

func TestPointFixChapterFallsBackWhenRewriteStillContainsPhrase(t *testing.T) {
	mainClient := llm.NewFakeClient(resp(chapterJSONFromText("Still has the flagged phrase in it.")))
	st := store.NewMemoryStore()
	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), oneChapterSpec())

	script := &story.Script{Chapters: []story.Chapter{{
		Index: 1, Text: "Sentence with the flagged phrase in it.", DisplayText: "Sentence with the flagged phrase in it.",
	}}}
	violations := []story.Violation{{Rule: "repeated_ngram", Chapter: 1, Phrase: "the flagged phrase"}}

	fixed, err := o.pointFixChapter(context.Background(), script, 1, violations)
	if err != nil {
		t.Fatalf("pointFixChapter: %v", err)
	}
	if fixed {
		t.Fatalf("expected pointFixChapter to reject a rewrite that still contains the flagged phrase")
	}
}

func TestPointFixChapterSucceedsAndKeepsTextAndDisplayTextAligned(t *testing.T) {
	mainClient := llm.NewFakeClient(resp(chapterJSONFromText("The rewritten sentence works fine.")))
	st := store.NewMemoryStore()
	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), oneChapterSpec())

	script := &story.Script{Chapters: []story.Chapter{{
		Index:       1,
		Text:        "Opening sentence. Sentence with the flagged phrase in it. Closing sentence.",
		DisplayText: "Opening sentence. Sentence with the flagged phrase in it. Closing sentence.",
	}}}
	violations := []story.Violation{{Rule: "repeated_ngram", Chapter: 1, Phrase: "the flagged phrase"}}

	fixed, err := o.pointFixChapter(context.Background(), script, 1, violations)
	if err != nil {
		t.Fatalf("pointFixChapter: %v", err)
	}
	if !fixed {
		t.Fatalf("expected pointFixChapter to succeed")
	}
	want := "Opening sentence. The rewritten sentence works fine. Closing sentence."
	if script.Chapters[0].Text != want {
		t.Fatalf("got text %q, want %q", script.Chapters[0].Text, want)
	}
	if script.Chapters[0].DisplayText != want {
		t.Fatalf("got display_text %q, want %q", script.Chapters[0].DisplayText, want)
	}
	if len(mainClient.Calls) != 1 {
		t.Fatalf("expected exactly 1 (cheap point-fix) call, got %d", len(mainClient.Calls))
	}
}

func TestPointFixChapterRetriesOncePastTruncatedPointFixCall(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(`{"text": "cut off mid-strin`), // truncated — a real run showed this at a 300-token budget
		resp(chapterJSONFromText("The rewritten sentence works fine.")),
	)
	st := store.NewMemoryStore()
	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), oneChapterSpec())

	script := &story.Script{Chapters: []story.Chapter{{
		Index:       1,
		Text:        "Opening sentence. Sentence with the flagged phrase in it. Closing sentence.",
		DisplayText: "Opening sentence. Sentence with the flagged phrase in it. Closing sentence.",
	}}}
	violations := []story.Violation{{Rule: "repeated_ngram", Chapter: 1, Phrase: "the flagged phrase"}}

	fixed, err := o.pointFixChapter(context.Background(), script, 1, violations)
	if err != nil {
		t.Fatalf("pointFixChapter: %v", err)
	}
	if !fixed {
		t.Fatalf("expected pointFixChapter to succeed after retrying the truncated attempt")
	}
	if len(mainClient.Calls) != 2 {
		t.Fatalf("expected exactly 2 calls (1 truncated, 1 retry), got %d", len(mainClient.Calls))
	}
	if mainClient.Calls[1].Opts.MaxTokens <= mainClient.Calls[0].Opts.MaxTokens {
		t.Fatalf("expected the retry's MaxTokens to be larger, got first=%d retry=%d",
			mainClient.Calls[0].Opts.MaxTokens, mainClient.Calls[1].Opts.MaxTokens)
	}
}
