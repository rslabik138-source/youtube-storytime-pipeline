package story

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Violation struct {
	Rule     string
	Severity Severity
	Chapter  int // 0 if not tied to one chapter (e.g. a bible-level violation)
	Message  string

	// Phrase is the exact repeated text this violation is about (a 6-word
	// gram, a sentence opening, the refrain phrase, an exact dollar
	// amount, a paragraph opening) — whatever a caller needs to locate the
	// offending sentence in Chapter's text and fix just that sentence
	// instead of regenerating the whole chapter. Empty means this
	// violation can't be localized to one sentence (e.g. "refrain phrase
	// missing entirely" — there's nothing to find and replace).
	Phrase string
}

func wordCount(s string) int { return len(strings.Fields(s)) }

var sentenceSplit = regexp.MustCompile(`[.!?…]+(\s+|$)`)

// SplitSentences splits text into trimmed sentences on ., !, ?, or … —
// exported so callers outside this package (a point-fix repair step, for
// instance) can locate a specific sentence the same way the validators do,
// without duplicating the regex.
func SplitSentences(text string) []string { return splitSentences(text) }

func splitSentences(text string) []string {
	parts := sentenceSplit.Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
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

// ==================== Bible validators ====================

// BibleValidatorConfig.UsedNames is the last N character names pulled from
// store.Store — a name on this list forces the bible to regenerate.
type BibleValidatorConfig struct {
	UsedNames []string
}

func ValidateBibleNamesUnique(b Bible, cfg BibleValidatorConfig) []Violation {
	seen := map[string]bool{}
	var out []Violation
	check := func(name string) {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			return
		}
		if seen[key] {
			out = append(out, Violation{Rule: "bible_names_unique", Severity: SeverityError,
				Message: fmt.Sprintf("name %q appears more than once in the bible", name)})
		}
		seen[key] = true
	}
	check(b.Narrator.Name)
	for _, p := range b.Cast {
		check(p.Name)
	}
	return out
}

func ValidateBibleTimelineMonotonic(b Bible, cfg BibleValidatorConfig) []Violation {
	for i := 1; i < len(b.Timeline); i++ {
		if b.Timeline[i].Year < b.Timeline[i-1].Year {
			return []Violation{{
				Rule:     "bible_timeline_monotonic",
				Severity: SeverityError,
				Message: fmt.Sprintf("timeline year %d at position %d precedes %d at position %d",
					b.Timeline[i].Year, i, b.Timeline[i-1].Year, i-1),
			}}
		}
	}
	return nil
}

func ValidateBibleRefrainPhraseNonEmpty(b Bible, cfg BibleValidatorConfig) []Violation {
	if strings.TrimSpace(b.RefrainPhrase) == "" {
		return []Violation{{Rule: "bible_refrain_phrase", Severity: SeverityError, Message: "refrain phrase is empty"}}
	}
	return nil
}

// nameKeys lowercases a name into its dedup keys: the whole name plus each
// whitespace-separated part (first, last, ...), so a reused surname or first
// name is caught even when the full names differ. Single-token or empty
// names return just themselves / nothing.
func nameKeys(name string) []string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	parts := strings.Fields(name)
	if len(parts) <= 1 {
		return []string{name}
	}
	return append([]string{name}, parts...)
}

// ValidateBibleNamesNotReused is the mechanical enforcement point for the
// content-level dedup requirement: any name matching store's used_names
// forces the bible to regenerate before a single chapter gets written.
func ValidateBibleNamesNotReused(b Bible, cfg BibleValidatorConfig) []Violation {
	if len(cfg.UsedNames) == 0 {
		return nil
	}
	used := make(map[string]bool, len(cfg.UsedNames))
	for _, n := range cfg.UsedNames {
		for _, key := range nameKeys(n) {
			used[key] = true
		}
	}

	var out []Violation
	check := func(name string) {
		// Match on the full name AND each part, so a reused SURNAME (or first
		// name) trips the guard even when the full names differ — e.g. a new
		// "Julian Vance" against a prior "Caleb Vance".
		for _, key := range nameKeys(name) {
			if used[key] {
				out = append(out, Violation{Rule: "bible_names_not_reused", Severity: SeverityError,
					Message: fmt.Sprintf("name %q reuses a name (or its surname/first name) from a previous produced script", name)})
				return
			}
		}
	}
	check(b.Narrator.Name)
	for _, p := range b.Cast {
		check(p.Name)
	}
	return out
}

var bibleValidators = []func(Bible, BibleValidatorConfig) []Violation{
	ValidateBibleNamesUnique,
	ValidateBibleTimelineMonotonic,
	ValidateBibleRefrainPhraseNonEmpty,
	ValidateBibleNamesNotReused,
}

func ValidateBible(b Bible, cfg BibleValidatorConfig) []Violation {
	var out []Violation
	for _, v := range bibleValidators {
		out = append(out, v(b, cfg)...)
	}
	return out
}

// ==================== Chapter validators ====================

type ChapterValidatorConfig struct {
	BannedPhrases     []string
	LengthTolerance   float64 // 0.20
	MaxSentenceWords  int     // 22
	WarnSentenceWords int     // 18
	LongSentenceWords int     // 20 — a sentence at or above this length counts toward MinLongSentences
	MinLongSentences  int     // 2 — below this many long sentences, the chapter reads as monotonous

	// Bible backs ValidateChapterHookIdentity (the narrator's name and age
	// must be planted in the hook) — zero-value Bible (narrator name
	// empty) makes that check a no-op, so callers that don't care (most
	// unit tests) can leave this unset.
	Bible Bible

	// SkipDialogueRequirement silences ValidateChapterDirectSpeechCount —
	// set this true when validating one half of a split chapter (the
	// dialogue-line minimum is checked once, against the combined text,
	// not doubled up against each half).
	SkipDialogueRequirement bool
}

func DefaultChapterValidatorConfig(banned []string) ChapterValidatorConfig {
	return ChapterValidatorConfig{
		BannedPhrases:   banned,
		LengthTolerance: 0.20,
		// The prompt asks for an occasional 20-25 word sentence (see
		// ValidateChapterSentenceLengthVariance) — a hard error cap at 22
		// directly contradicted that instruction and burned real retries on
		// sentences the prompt itself asked for. 28/25 gives the requested
		// range genuine room: no violation at all up to 25 words, a warning
		// from 26-28, and only a genuine runaway sentence (29+) blocks.
		MaxSentenceWords:  28,
		WarnSentenceWords: 25,
		LongSentenceWords: 20,
		MinLongSentences:  2,
	}
}

// ValidateChapterLength is a style signal, not a correctness one — a
// chapter outside its word-count band still needs a human (or the review
// pass) to judge whether it's actually a problem, so it's a warning: it
// gets logged but never forces an expensive regeneration on its own.
func ValidateChapterLength(c Chapter, cfg ChapterValidatorConfig) []Violation {
	if c.TargetWords <= 0 {
		return nil
	}
	tolerance := cfg.LengthTolerance
	if tolerance <= 0 {
		tolerance = 0.20
	}
	actual := wordCount(c.Text)
	lo := float64(c.TargetWords) * (1 - tolerance)
	hi := float64(c.TargetWords) * (1 + tolerance)
	if float64(actual) < lo || float64(actual) > hi {
		return []Violation{{
			Rule: "chapter_length", Severity: SeverityWarning, Chapter: c.Index,
			Message: fmt.Sprintf("chapter %d (%s) has %d words, target %d ±%.0f%% (expected %.0f-%.0f)",
				c.Index, c.Beat, actual, c.TargetWords, tolerance*100, lo, hi),
		}}
	}
	return nil
}

func ValidateChapterSentenceLength(c Chapter, cfg ChapterValidatorConfig) []Violation {
	maxWords := cfg.MaxSentenceWords
	if maxWords <= 0 {
		maxWords = 28
	}
	warnWords := cfg.WarnSentenceWords
	if warnWords <= 0 {
		warnWords = 25
	}

	var out []Violation
	for _, sent := range splitSentences(c.Text) {
		n := wordCount(sent)
		switch {
		case n > maxWords:
			out = append(out, Violation{Rule: "sentence_length", Severity: SeverityError, Chapter: c.Index,
				Message: fmt.Sprintf("chapter %d: sentence at %d words (max %d): %q", c.Index, n, maxWords, sent)})
		case n > warnWords:
			out = append(out, Violation{Rule: "sentence_length", Severity: SeverityWarning, Chapter: c.Index,
				Message: fmt.Sprintf("chapter %d: sentence at %d words (warn from %d): %q", c.Index, n, warnWords, sent)})
		}
	}
	return out
}

func ValidateChapterBannedPhrases(c Chapter, cfg ChapterValidatorConfig) []Violation {
	var out []Violation
	lower := strings.ToLower(c.Text)
	for _, phrase := range cfg.BannedPhrases {
		if phrase == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(phrase)) {
			out = append(out, Violation{Rule: "banned_phrase", Severity: SeverityError, Chapter: c.Index,
				Message: fmt.Sprintf("chapter %d contains banned phrase %q", c.Index, phrase)})
		}
	}
	return out
}

var digitOrDollar = regexp.MustCompile(`[0-9$]`)

// ValidateChapterNoDigitsInTTSText checks Text (never DisplayText, which is
// allowed normal formatting) — every number must already be spelled out as
// words, because Text is what goes straight to the TTS engine.
func ValidateChapterNoDigitsInTTSText(c Chapter, cfg ChapterValidatorConfig) []Violation {
	loc := digitOrDollar.FindStringIndex(c.Text)
	if loc == nil {
		return nil
	}
	start := max(0, loc[0]-15)
	end := min(len(c.Text), loc[1]+15)
	return []Violation{{
		Rule: "tts_digits", Severity: SeverityError, Chapter: c.Index,
		Message: fmt.Sprintf("chapter %d TTS text contains a digit or \"$\" near %q — spell all numbers as words",
			c.Index, c.Text[start:end]),
	}}
}

// speechVerbs is a heuristic, not an exhaustive parser: the brief requires
// dialogue WITHOUT quotation marks (for cleaner TTS delivery), so detecting
// "is there spoken dialogue here" mechanically means looking for common
// speech-attribution verbs rather than punctuation. It will miss some real
// dialogue and can be fooled, but it catches the common case cheaply
// without an LLM call.
var speechVerbs = []string{
	" said", " told me", " told him", " told her", " asked", " snapped",
	" muttered", " replied", " shouted", " whispered", " sneered", " scoffed",
}

func countSpeechVerbs(text string) int {
	lower := strings.ToLower(text)
	count := 0
	for _, v := range speechVerbs {
		count += strings.Count(lower, v)
	}
	return count
}

// directSpeechRequiredBeats maps a beat to how many detected lines of
// spoken dialogue it needs. A real run showed a whole script with 0
// quotation marks and only 4 speech-attribution verbs total — a presence
// check (>= 1) was satisfied by a single incidental "said" while the beat
// still read as entirely reported summary, not scene.
var directSpeechRequiredBeats = map[string]int{
	"the_cut":       3,
	"the_demand":    3,
	"the_reckoning": 3,
}

// ValidateChapterDirectSpeechCount requires at least directSpeechRequiredBeats[c.Beat]
// detected lines of spoken dialogue — skipped (via
// cfg.SkipDialogueRequirement) when c is one half of a split chapter, so
// the requirement is checked once against the combined text, not doubled
// up against each half.
func ValidateChapterDirectSpeechCount(c Chapter, cfg ChapterValidatorConfig) []Violation {
	if cfg.SkipDialogueRequirement {
		return nil
	}
	minLines, ok := directSpeechRequiredBeats[c.Beat]
	if !ok {
		return nil
	}
	got := countSpeechVerbs(c.Text)
	if got >= minLines {
		return nil
	}
	return []Violation{{
		Rule: c.Beat + "_direct_speech_count", Severity: SeverityError, Chapter: c.Index,
		Message: fmt.Sprintf(
			"chapter %d (%s) has only %d detected line(s) of direct speech (need >= %d) — render the scene as spoken dialogue with attribution (e.g. \"..., she said\"), not reported/summarized speech",
			c.Index, c.Beat, got, minLines),
	}}
}

// numberToWords converts n to its spelled-out English form (e.g. 47 ->
// "forty-seven"), matching how Text spells out every number — used to
// check the hook plants the narrator's age even though Text never
// contains a literal digit.
func numberToWords(n int) string {
	ones := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
		"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tens := []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

	switch {
	case n < 0:
		return ""
	case n < 20:
		return ones[n]
	case n < 100:
		if n%10 == 0 {
			return tens[n/10]
		}
		return tens[n/10] + "-" + ones[n%10]
	case n < 1000:
		rest := n % 100
		hundreds := ones[n/100] + " hundred"
		if rest == 0 {
			return hundreds
		}
		return hundreds + " " + numberToWords(rest)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// containsNumberWord reports whether text contains n's spelled-out form,
// accepting both the hyphenated ("thirty-four") and spaced ("thirty
// four") variants a model might produce.
func containsNumberWord(text string, n int) bool {
	word := numberToWords(n)
	if word == "" {
		return false
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, word) {
		return true
	}
	return strings.Contains(lower, strings.ReplaceAll(word, "-", " "))
}

// ValidateChapterHookIdentity requires the hook to explicitly plant the
// narrator's identity — name, age, and at least one line of direct speech
// (the humiliation, spoken) — since a real run showed the hook otherwise
// reads as generic scene-setting with no anchor for who's telling the
// story. A no-op when cfg.Bible isn't populated (the caller sets
// ChapterValidatorConfig.Bible = script.Bible; most unit tests don't need
// to) or when c isn't the hook.
func ValidateChapterHookIdentity(c Chapter, cfg ChapterValidatorConfig) []Violation {
	if c.Beat != "hook" {
		return nil
	}
	name := strings.TrimSpace(cfg.Bible.Narrator.Name)
	if name == "" {
		return nil
	}

	var out []Violation
	lower := strings.ToLower(c.Text)

	firstName := strings.Fields(name)[0]
	if !strings.Contains(lower, strings.ToLower(firstName)) {
		out = append(out, Violation{
			Rule: "hook_missing_identity", Severity: SeverityError, Chapter: c.Index,
			Message: fmt.Sprintf("hook chapter never states the narrator's name (%q) — plant identity in the first lines", name),
		})
	}

	// Channel signature: the hook must use the literal "My name is ..."
	// opening — a real run drifted to "At forty-two, I go by the name of
	// Cyrus Mercer", which has no "my name is" anywhere and breaks the
	// recognizable format every other script uses. The chapter prompt
	// mandates it as the FIRST sentence; this validator enforces that the
	// phrase is at least present.
	if !strings.Contains(lower, "my name is") {
		out = append(out, Violation{
			Rule: "hook_missing_signature_opening", Severity: SeverityError, Chapter: c.Index,
			Message: `hook must use the signature opening "My name is <name> and I am <age> years old." as its first sentence`,
		})
	}

	if cfg.Bible.Narrator.Age > 0 && !containsNumberWord(c.Text, cfg.Bible.Narrator.Age) {
		out = append(out, Violation{
			Rule: "hook_missing_age", Severity: SeverityError, Chapter: c.Index,
			Message: fmt.Sprintf("hook chapter never states the narrator's age (%s) — plant it in the first lines", numberToWords(cfg.Bible.Narrator.Age)),
		})
	}

	if countSpeechVerbs(c.Text) == 0 {
		out = append(out, Violation{
			Rule: "hook_missing_direct_speech", Severity: SeverityError, Chapter: c.Index,
			Message: "hook chapter has no detectable direct speech — the humiliation line must be rendered as spoken dialogue, not summarized",
		})
	}

	return out
}

// MissingPunctuationPattern matches a lowercase word running directly into
// a capitalized pronoun/determiner with only whitespace between them — no
// period, comma, or other mark — the tell of a dropped sentence-ending
// punctuation mark. A real run showed this exact shape ("...on a Tuesday
// morning She set her heavy glass jar...") reach the TTS engine, where two
// sentences read as one because the model silently omitted the mark.
// Exported so internal/generate's deterministic auto-fixer (cheaper and
// more reliable than an LLM rewrite for this one mechanical case) can use
// the identical pattern instead of duplicating it.
var MissingPunctuationPattern = regexp.MustCompile(`([a-z])(\s+)(She|He|It|They|The|Her|His)(\s)`)

// ValidateChapterMissingPunctuation flags every place in c.Text where a
// sentence-ending mark appears to have been dropped — see
// MissingPunctuationPattern. Blocking: a missing period isn't a style
// nit, it corrupts TTS delivery (two sentences read as one, wrong
// intonation, no pause).
func ValidateChapterMissingPunctuation(c Chapter, cfg ChapterValidatorConfig) []Violation {
	locs := MissingPunctuationPattern.FindAllStringIndex(c.Text, -1)
	if locs == nil {
		return nil
	}
	var out []Violation
	for _, loc := range locs {
		start := max(0, loc[0]-20)
		end := min(len(c.Text), loc[1]+20)
		out = append(out, Violation{
			Rule: "missing_punctuation", Severity: SeverityError, Chapter: c.Index,
			Phrase: strings.TrimSpace(c.Text[loc[0]:loc[1]]),
			Message: fmt.Sprintf(
				"chapter %d is missing sentence-ending punctuation near %q — insert a period (or ?/!) before the capitalized word",
				c.Index, c.Text[start:end]),
		})
	}
	return out
}

var writtenDocumentKeywords = []string{
	"letter", "document", "paper", "signed", "contract", "portfolio",
	"email", "invoice", "printed", "typed", "in writing", "on paper",
}

func ValidateChapterMentionsWrittenDocument(c Chapter, cfg ChapterValidatorConfig) []Violation {
	if c.Beat != "the_demand" {
		return nil
	}
	lower := strings.ToLower(c.Text)
	for _, kw := range writtenDocumentKeywords {
		if strings.Contains(lower, kw) {
			return nil
		}
	}
	return []Violation{{
		Rule: "the_demand_written_document", Severity: SeverityError, Chapter: c.Index,
		Message: "the_demand chapter doesn't mention a written document (letter, contract, email, signed paper, ...) — the antagonist must self-incriminate in writing here",
	}}
}

// ValidateChapterSentenceLengthVariance flags a chapter with too few long
// sentences mixed among its short ones — the tell of an LLM settling into
// one rhythm instead of following the "short fragments, then a 20-25 word
// sentence" style rule. This checks the concrete thing the prompt asks for
// (at least a couple of genuinely long sentences) rather than a statistical
// proxy like stddev, which can reject a chapter that's actually fine by a
// fraction of a point. It's a style signal, not a correctness one, so it's
// a warning: logged, never a reason by itself to burn a regeneration.
func ValidateChapterSentenceLengthVariance(c Chapter, cfg ChapterValidatorConfig) []Violation {
	sentences := splitSentences(c.Text)
	if len(sentences) < 2 {
		return nil
	}

	longWords := cfg.LongSentenceWords
	if longWords <= 0 {
		longWords = 20
	}
	minLong := cfg.MinLongSentences
	if minLong <= 0 {
		minLong = 2
	}

	long := 0
	for _, sent := range sentences {
		if wordCount(sent) >= longWords {
			long++
		}
	}

	if long < minLong {
		return []Violation{{
			Rule: "sentence_length_variance", Severity: SeverityWarning, Chapter: c.Index,
			Message: fmt.Sprintf(
				"chapter %d has only %d sentence(s) of %d+ words (need >= %d) — vary sentence length more: mix short fragments with an occasional 20-25 word sentence",
				c.Index, long, longWords, minLong),
		}}
	}
	return nil
}

// ValidateChapter dispatches every check that applies to c, including the
// beat-specific ones (the_cut, the_demand self-select via c.Beat).
func ValidateChapter(c Chapter, cfg ChapterValidatorConfig) []Violation {
	var out []Violation
	out = append(out, ValidateChapterLength(c, cfg)...)
	out = append(out, ValidateChapterSentenceLength(c, cfg)...)
	out = append(out, ValidateChapterSentenceLengthVariance(c, cfg)...)
	out = append(out, ValidateChapterBannedPhrases(c, cfg)...)
	out = append(out, ValidateChapterNoDigitsInTTSText(c, cfg)...)
	out = append(out, ValidateChapterDirectSpeechCount(c, cfg)...)
	out = append(out, ValidateChapterMentionsWrittenDocument(c, cfg)...)
	out = append(out, ValidateChapterHookIdentity(c, cfg)...)
	out = append(out, ValidateChapterMissingPunctuation(c, cfg)...)
	return out
}

// ==================== Cross-chapter validators ====================

// ValidateAntiRepetitionParagraphStarts collects the first five words of
// every paragraph (chapters are joined by blank lines; paragraphs within a
// chapter are too) across the whole script. The same opening three or more
// times is the tell of an LLM falling into a template.
func ValidateAntiRepetitionParagraphStarts(s *Script) []Violation {
	counts := map[string]int{}
	firstSeenIn := map[string]int{}
	for _, c := range s.Chapters {
		for _, para := range splitParagraphs(c.Text) {
			words := strings.Fields(para)
			if len(words) < 5 {
				continue
			}
			start := strings.ToLower(strings.Join(words[:5], " "))
			counts[start]++
			if _, ok := firstSeenIn[start]; !ok {
				firstSeenIn[start] = c.Index
			}
		}
	}

	var out []Violation
	for start, n := range counts {
		if n >= 3 {
			out = append(out, Violation{
				Rule: "repeated_paragraph_start", Severity: SeverityError, Chapter: firstSeenIn[start], Phrase: start,
				Message: fmt.Sprintf("paragraph opening %q repeats %d times across the script (first seen in chapter %d)",
					start, n, firstSeenIn[start]),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Message < out[j].Message })
	return out
}

// SixGramOccurrences maps every distinct six-word phrase across s's
// chapters to every chapter index it appears in, in order. Exported so
// callers outside this package can reuse the exact same windowing:
// internal/store records every distinct phrase from an accepted script
// for cross-script dedup, and internal/generate's cross-script repetition
// check needs to know which of the current script's own chapters a
// flagged phrase came from.
func SixGramOccurrences(s *Script) map[string][]int {
	const n = 6
	hits := map[string][]int{}
	for _, c := range s.Chapters {
		words := strings.Fields(strings.ToLower(c.Text))
		for i := 0; i+n <= len(words); i++ {
			gram := strings.Join(words[i:i+n], " ")
			hits[gram] = append(hits[gram], c.Index)
		}
	}
	return hits
}

// ValidateAntiRepetitionNGrams flags every occurrence of a 6-word phrase
// beyond the first 2 anywhere in the script — the first 2 uses are allowed
// (a deliberate echo can be intentional), the 3rd and later ones are
// flagged against whichever chapter they actually appear in, so that
// chapter — not the one that used the phrase first — is what gets
// regenerated.
func ValidateAntiRepetitionNGrams(s *Script) []Violation {
	const maxOccurrences = 2

	hits := SixGramOccurrences(s)

	var out []Violation
	for gram, chapters := range hits {
		if len(chapters) <= maxOccurrences {
			continue
		}
		for _, idx := range chapters[maxOccurrences:] {
			out = append(out, Violation{
				Rule: "repeated_ngram", Severity: SeverityError, Chapter: idx, Phrase: gram,
				Message: fmt.Sprintf("six-word phrase %q appears %d times across the script (max %d) — chapter %d has an excess occurrence",
					gram, len(chapters), maxOccurrences, idx),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out
}

// ValidateSentenceOpeningRepetition flags every occurrence of a sentence's
// first two words beyond the first 8 uses anywhere in the script.
func ValidateSentenceOpeningRepetition(s *Script) []Violation {
	const maxOccurrences = 8

	hits := map[string][]int{}
	for _, c := range s.Chapters {
		for _, sent := range splitSentences(c.Text) {
			words := strings.Fields(sent)
			if len(words) < 2 {
				continue
			}
			opening := strings.ToLower(strings.Join(words[:2], " "))
			hits[opening] = append(hits[opening], c.Index)
		}
	}

	var out []Violation
	for opening, chapters := range hits {
		if len(chapters) <= maxOccurrences {
			continue
		}
		for _, idx := range chapters[maxOccurrences:] {
			out = append(out, Violation{
				Rule: "repeated_sentence_opening", Severity: SeverityError, Chapter: idx, Phrase: opening,
				Message: fmt.Sprintf("sentence opening %q is used %d times across the script (max %d) — vary how sentences begin in chapter %d",
					opening, len(chapters), maxOccurrences, idx),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out
}

// ValidateRefrainPhrasePlacement enforces the genre rule that the bible's
// refrain phrase appears exactly twice: once in chapter 6 (the_cut, where
// it's planted) and once in chapter 13 (the_reckoning, where it returns).
// Anywhere else, or missing/duplicated in either of those two chapters, is
// a violation.
func ValidateRefrainPhrasePlacement(s *Script) []Violation {
	phrase := strings.ToLower(strings.TrimSpace(s.Bible.RefrainPhrase))
	if phrase == "" {
		return nil
	}

	const plantChapter = 6
	const returnChapter = 13

	present := map[int]bool{}
	countByChapter := map[int]int{}
	for _, c := range s.Chapters {
		present[c.Index] = true
		if n := strings.Count(strings.ToLower(c.Text), phrase); n > 0 {
			countByChapter[c.Index] = n
		}
	}

	var out []Violation
	for idx, n := range countByChapter {
		if idx != plantChapter && idx != returnChapter {
			out = append(out, Violation{
				Rule: "refrain_phrase_placement", Severity: SeverityError, Chapter: idx, Phrase: phrase,
				Message: fmt.Sprintf("the refrain phrase appears in chapter %d %d time(s), but it is only allowed in chapters %d and %d",
					idx, n, plantChapter, returnChapter),
			})
		}
	}
	for _, idx := range []int{plantChapter, returnChapter} {
		if !present[idx] {
			continue // this script doesn't include that chapter at all (e.g. a partial script) — nothing to enforce
		}
		switch countByChapter[idx] {
		case 0:
			// Phrase intentionally left empty: there's no sentence to
			// locate and rewrite here, the phrase needs to be ADDED —
			// only a full chapter regeneration can do that.
			out = append(out, Violation{
				Rule: "refrain_phrase_placement", Severity: SeverityError, Chapter: idx,
				Message: fmt.Sprintf("the refrain phrase must appear exactly once in chapter %d but doesn't appear at all", idx),
			})
		case 1:
			// exactly right
		default:
			out = append(out, Violation{
				Rule: "refrain_phrase_placement", Severity: SeverityError, Chapter: idx, Phrase: phrase,
				Message: fmt.Sprintf("the refrain phrase appears %d times in chapter %d but should appear exactly once", countByChapter[idx], idx),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out
}

var moneyAmountPattern = regexp.MustCompile(`\$[0-9][0-9,]*(?:\.[0-9]+)?`)

// ValidateMoneyAmountRepetition flags every mention of an exact dollar
// amount beyond the first 5 anywhere in the script. It scans DisplayText,
// not Text — Text has every number spelled out as words (no "$" or
// digits, per the TTS formatting rule), so the exact-amount signal only
// exists in DisplayText.
func ValidateMoneyAmountRepetition(s *Script) []Violation {
	const maxOccurrences = 5

	hits := map[string][]int{}
	for _, c := range s.Chapters {
		for _, amount := range moneyAmountPattern.FindAllString(c.DisplayText, -1) {
			hits[amount] = append(hits[amount], c.Index)
		}
	}

	var out []Violation
	for amount, chapters := range hits {
		if len(chapters) <= maxOccurrences {
			continue
		}
		for _, idx := range chapters[maxOccurrences:] {
			out = append(out, Violation{
				Rule: "money_amount_repetition", Severity: SeverityError, Chapter: idx, Phrase: amount,
				Message: fmt.Sprintf("exact amount %q is mentioned %d times across the script (max %d) — chapter %d has an excess mention",
					amount, len(chapters), maxOccurrences, idx),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out
}

// numberContextFillers are skipped when looking for the word right after a
// number mention, so "forty-seven of the missing entries" resolves to
// "entries", not "of".
var numberContextFillers = map[string]bool{"of": true, "in": true, "a": true, "the": true, "an": true, "for": true}

// nextWordAfter returns the first non-filler word starting at byte offset
// pos in text, lowercased and stripped of punctuation — a crude proxy for
// "what entity is this number attached to".
func nextWordAfter(text string, pos int) string {
	if pos < 0 || pos > len(text) {
		return ""
	}
	for _, w := range strings.Fields(text[pos:]) {
		clean := strings.ToLower(strings.Trim(w, ".,!?;:\"'"))
		if clean == "" || numberContextFillers[clean] {
			continue
		}
		return clean
	}
	return ""
}

// ValidateNumberReuseAcrossEntities flags a bible-seeded number (e.g. the
// stolen amount, a duration) used more than 4 times across the script, or
// applied to more than one distinct kind of thing — the tell of the model
// reaching for the same figure as a verbal tic instead of tracking
// distinct facts. A real run showed "forty-seven" recurring as a record
// count, a number of hours, a dollar amount, a number of months, minutes,
// a page, and a code — all the same spelled-out number, none of them the
// same fact.
func ValidateNumberReuseAcrossEntities(s *Script) []Violation {
	const maxOccurrences = 4

	type occurrence struct {
		chapter int
		unit    string
	}

	var out []Violation
	for _, value := range s.Bible.Numbers {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		lowerValue := strings.ToLower(value)

		var occurrences []occurrence
		for _, c := range s.Chapters {
			lower := strings.ToLower(c.Text)
			from := 0
			for {
				rel := strings.Index(lower[from:], lowerValue)
				if rel < 0 {
					break
				}
				abs := from + rel
				occurrences = append(occurrences, occurrence{
					chapter: c.Index,
					unit:    nextWordAfter(c.Text, abs+len(value)),
				})
				from = abs + len(lowerValue)
			}
		}
		if len(occurrences) == 0 {
			continue
		}

		if len(occurrences) > maxOccurrences {
			for _, occ := range occurrences[maxOccurrences:] {
				out = append(out, Violation{
					Rule: "number_reuse_limit", Severity: SeverityError, Chapter: occ.chapter, Phrase: value,
					Message: fmt.Sprintf("%q (a bible number) is used %d times across the script (max %d) — chapter %d has an excess occurrence",
						value, len(occurrences), maxOccurrences, occ.chapter),
				})
			}
		}

		// Warning, not Error: nextWordAfter is a crude proxy (literally just
		// the next word after the number) for "is this the same fact" — two
		// occurrences with different next-words are routinely the SAME fact
		// phrased differently ("forty thousand dollars, which..." vs "forty
		// thousand dollars in savings"), not an actual reused number. As an
		// Error this was a real source of wasted spend: a violation the
		// model can't reliably fix on purpose (it isn't told to preserve an
		// exact next-word) falls back to a full chapter regeneration, whose
		// fresh text then passes or fails this same crude check close to at
		// random — real money spent chasing a coin flip, not a real defect.
		firstUnit := occurrences[0].unit
		seenAlternate := map[string]bool{}
		for _, occ := range occurrences[1:] {
			if occ.unit == "" || occ.unit == firstUnit || seenAlternate[occ.unit] {
				continue
			}
			seenAlternate[occ.unit] = true
			out = append(out, Violation{
				Rule: "number_reuse_different_entities", Severity: SeverityWarning, Chapter: occ.chapter, Phrase: value,
				Message: fmt.Sprintf("%q (a bible number) is applied to different things across the script (%q in chapter %d, %q in chapter %d) — reuse the same number only for the same fact",
					value, firstUnit, occurrences[0].chapter, occ.unit, occ.chapter),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out
}

// ValidateScript runs every chapter-level check on every chapter, plus every
// whole-script check: paragraph-opening repetition, 6-word phrase
// repetition, sentence-opening repetition, refrain phrase placement,
// exact money amount repetition, and bible-number reuse.
func ValidateScript(s *Script, cfg ChapterValidatorConfig) []Violation {
	var out []Violation
	for _, c := range s.Chapters {
		out = append(out, ValidateChapter(c, cfg)...)
	}
	out = append(out, ValidateAntiRepetitionParagraphStarts(s)...)
	out = append(out, ValidateAntiRepetitionNGrams(s)...)
	out = append(out, ValidateSentenceOpeningRepetition(s)...)
	out = append(out, ValidateRefrainPhrasePlacement(s)...)
	out = append(out, ValidateMoneyAmountRepetition(s)...)
	out = append(out, ValidateNumberReuseAcrossEntities(s)...)
	return out
}
