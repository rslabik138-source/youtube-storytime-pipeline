package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// maxViolationsForPointFix caps how many violations in one chapter get the
// cheap per-sentence treatment before falling back to a full chapter
// regeneration — past this many, the chapter has enough wrong with it that
// stitching in one-sentence patches risks leaving it incoherent, and a
// fresh pass is more reliable than that many surgical edits.
const maxViolationsForPointFix = 8

// dedupeExactRepeatedSentences removes any sentence in a chapter's Text
// that exactly repeats (case/whitespace-insensitively) one already seen
// earlier in the script. This is the deterministic, LLM-free first pass
// over the whole-script repetition guard: an exact repeated sentence trips
// repeated_ngram (and often repeated_sentence_opening) for free, and
// removing it costs nothing — no reason to spend a model call on it.
// DisplayText is kept in sync by dropping the sentence at the same
// position, when its sentence count matches Text's. Returns how many
// sentences were removed.
func dedupeExactRepeatedSentences(script *story.Script) int {
	seen := map[string]bool{}
	removed := 0

	for i := range script.Chapters {
		ch := &script.Chapters[i]
		textSentences := story.SplitSentences(ch.Text)
		displaySentences := story.SplitSentences(ch.DisplayText)

		var keepIdx []int
		changed := false
		for j, s := range textSentences {
			key := normalizeSentence(s)
			if key != "" && seen[key] {
				changed = true
				removed++
				continue
			}
			seen[key] = true
			keepIdx = append(keepIdx, j)
		}
		if !changed {
			continue
		}

		if newText, ok := keepSentencesAt(ch.Text, textSentences, keepIdx); ok {
			ch.Text = newText
		}
		if len(displaySentences) == len(textSentences) {
			if newDisplay, ok := keepSentencesAt(ch.DisplayText, displaySentences, keepIdx); ok {
				ch.DisplayText = newDisplay
			}
		}
	}
	return removed
}

func normalizeSentence(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// fixMissingPunctuation mechanically inserts a period wherever
// story.MissingPunctuationPattern matches — a lowercase word running
// straight into a capitalized pronoun/determiner with no mark between them.
// This is a pure text substitution, not a rewrite: the fix is unambiguous
// (a dropped period, nothing else could be meant), so there's no reason to
// spend a model call asking an LLM to paraphrase around it the way
// pointFixChapter does for every other rule. Returns the fixed text and how
// many insertions were made.
func fixMissingPunctuation(text string) (string, int) {
	n := len(story.MissingPunctuationPattern.FindAllStringIndex(text, -1))
	if n == 0 {
		return text, 0
	}
	return story.MissingPunctuationPattern.ReplaceAllString(text, "$1. $3$4"), n
}

// fixMissingPunctuationInScript applies fixMissingPunctuation to every
// chapter's Text and DisplayText, mirroring
// dedupeExactRepeatedSentences — a free, deterministic pre-pass run before
// any LLM-based validation round so the missing_punctuation rule almost
// never actually has to trigger a regeneration or point-fix. Returns the
// total number of insertions made (counted from Text only, so a run
// touching just DisplayText — Text and DisplayText occasionally drift in
// exact wording — still gets applied but doesn't double-count).
func fixMissingPunctuationInScript(script *story.Script) int {
	total := 0
	for i := range script.Chapters {
		ch := &script.Chapters[i]
		if fixed, n := fixMissingPunctuation(ch.Text); n > 0 {
			ch.Text = fixed
			total += n
		}
		if fixed, n := fixMissingPunctuation(ch.DisplayText); n > 0 {
			ch.DisplayText = fixed
		}
	}
	return total
}

// sentenceSpan locates sentences[idx] as it actually appears in text,
// starting the search at byte offset from — not at 0 — so a sentence whose
// text happens to duplicate an earlier one is still matched at its own,
// correct position rather than re-matching the first occurrence. end
// includes the sentence's own trailing punctuation run (SplitSentences
// strips it, so the caller doesn't drop it on a replace/delete).
func sentenceSpan(text string, sentence string, from int) (start, end int, ok bool) {
	rel := strings.Index(text[from:], sentence)
	if rel < 0 {
		return 0, 0, false
	}
	start = from + rel
	end = start + len(sentence)
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if r != '.' && r != '!' && r != '?' && r != '…' {
			break
		}
		end += size
	}
	return start, end, true
}

// keepSentencesAt rebuilds text keeping only the sentences at keepIdx (in
// order), dropping every other sentence along with its own trailing
// punctuation — the gap before a kept sentence (including a paragraph
// break) is preserved untouched, only a dropped sentence's own span
// disappears.
func keepSentencesAt(text string, sentences []string, keepIdx []int) (string, bool) {
	keep := make(map[int]bool, len(keepIdx))
	for _, i := range keepIdx {
		keep[i] = true
	}

	var sb strings.Builder
	pos := 0
	for i, s := range sentences {
		_, end, ok := sentenceSpan(text, s, pos)
		if !ok {
			return text, false
		}
		if keep[i] {
			sb.WriteString(text[pos:end])
		}
		pos = end
	}
	sb.WriteString(text[pos:])
	return strings.TrimSpace(sb.String()), true
}

// replaceSentenceAt rewrites sentences[idx]'s exact span in text (its own
// trailing punctuation included) with replacement, leaving everything
// before and after — including paragraph breaks — untouched.
func replaceSentenceAt(text string, sentences []string, idx int, replacement string) (string, bool) {
	if idx < 0 || idx >= len(sentences) {
		return text, false
	}
	pos := 0
	for i := 0; i <= idx; i++ {
		start, end, ok := sentenceSpan(text, sentences[i], pos)
		if !ok {
			return text, false
		}
		if i == idx {
			return text[:start] + strings.TrimSpace(replacement) + text[end:], true
		}
		pos = end
	}
	return text, false
}

// locateSentenceIndex returns the index of the first sentence whose
// lowercased form contains lowercased phrase, or -1. Trailing punctuation
// on phrase is ignored: story.SplitSentences strips a sentence's own
// trailing mark before returning it, but a phrase drawn from a raw
// word-window (e.g. a six-gram from story.SixGramOccurrences that happens
// to end exactly at a sentence boundary — the common case for a whole
// repeated sentence, not just a repeated fragment) still carries whatever
// mark its source text had there, which would otherwise never match.
func locateSentenceIndex(sentences []string, phrase string) int {
	lower := strings.ToLower(phrase)
	lower = strings.TrimRight(lower, ".!?…,;:")
	if lower == "" {
		return -1
	}
	for i, s := range sentences {
		if strings.Contains(strings.ToLower(s), lower) {
			return i
		}
	}
	return -1
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// pointFixChapter tries to resolve every violation in vs (all already
// known to belong to chapter chapterIndex) by rewriting just the one
// sentence each is about — a small call per violation instead of a full
// chapter regeneration. It gives up (returns false, nil) and leaves
// the chapter untouched the moment any violation can't be handled this
// way: no Phrase to locate, the phrase isn't found in the chapter's text,
// the rewrite still contains the flagged phrase, or the LLM call itself
// fails — in every one of those cases the caller falls back to
// regenerating the whole chapter, which is what always used to happen.
func (o *Orchestrator) pointFixChapter(ctx context.Context, script *story.Script, chapterIndex int, vs []story.Violation) (bool, error) {
	ci := -1
	for i, c := range script.Chapters {
		if c.Index == chapterIndex {
			ci = i
			break
		}
	}
	if ci < 0 {
		return false, nil
	}
	ch := &script.Chapters[ci]

	spec, _ := o.chapterSpec(chapterIndex)
	model, forceModel := chapterModel(spec, o.settings.GenerateModel)

	for _, v := range vs {
		if v.Phrase == "" {
			o.logger.Warn("violation has no locatable phrase, falling back to full chapter regeneration",
				"chapter", chapterIndex, "rule", v.Rule)
			return false, nil
		}

		useDisplay := v.Rule == "money_amount_repetition"
		haystack := ch.Text
		if useDisplay {
			haystack = ch.DisplayText
		}

		sentences := story.SplitSentences(haystack)
		idx := locateSentenceIndex(sentences, v.Phrase)
		if idx < 0 {
			o.logger.Warn("could not locate the flagged phrase in a single sentence, falling back to full chapter regeneration",
				"chapter", chapterIndex, "rule", v.Rule)
			return false, nil
		}

		newText, newDisplay, err := o.pointFixSentence(ctx, script, sentences[idx], []string{v.Phrase}, model, forceModel, chapterIndex)
		if err != nil {
			o.logger.Warn("point-fix call failed, falling back to full chapter regeneration",
				"chapter", chapterIndex, "rule", v.Rule, "error", err)
			return false, nil
		}
		if containsFold(newText, v.Phrase) || containsFold(newDisplay, v.Phrase) {
			o.logger.Warn("point-fix rewrite still contains the flagged phrase, falling back to full chapter regeneration",
				"chapter", chapterIndex, "rule", v.Rule)
			return false, nil
		}

		if useDisplay {
			updated, ok := replaceSentenceAt(ch.DisplayText, story.SplitSentences(ch.DisplayText), idx, newDisplay)
			if !ok {
				return false, nil
			}
			ch.DisplayText = updated
			if textSentences := story.SplitSentences(ch.Text); idx < len(textSentences) {
				if updatedText, ok := replaceSentenceAt(ch.Text, textSentences, idx, newText); ok {
					ch.Text = updatedText
				}
			}
		} else {
			updated, ok := replaceSentenceAt(ch.Text, story.SplitSentences(ch.Text), idx, newText)
			if !ok {
				return false, nil
			}
			ch.Text = updated
			if displaySentences := story.SplitSentences(ch.DisplayText); idx < len(displaySentences) {
				if updatedDisplay, ok := replaceSentenceAt(ch.DisplayText, displaySentences, idx, newDisplay); ok {
					ch.DisplayText = updatedDisplay
				}
			}
		}

		o.logger.Info("point-fixed one sentence instead of regenerating the chapter",
			"chapter", chapterIndex, "rule", v.Rule)
	}

	return true, nil
}

type pointFixTemplateData struct {
	Sentence string
	Avoid    []string
}

// pointFixInitialMaxTokens/pointFixMaxAttempts bound pointFixSentence's own
// truncation retry. A real run showed a 300-token budget wasn't enough —
// a thinking-heavy model (~285 tokens observed) left almost no room for
// the actual short rewrite, so nearly every point-fix truncated and fell
// back to a full chapter regeneration, defeating the whole cost point.
// 700 leaves real headroom for that floor plus the rewrite itself.
const (
	pointFixInitialMaxTokens = 700
	pointFixMaxAttempts      = 2
)

// pointFixSentence asks the model to rewrite exactly one sentence so it no
// longer contains the given phrase(s) — a small, cheap call used in place
// of a full chapter regeneration (~12,000 tokens) whenever a whole-script
// repetition violation can be localized to one sentence. model/forceModel
// come from the chapter's own spec (chapterModel) so a point-fix on a beat
// pinned to a specific model doesn't quietly drift onto the generate
// role's default. Retries once with a larger budget on truncation — same
// reasoning as continuity.Checker's retry — before giving up and letting
// the caller fall back to a full chapter regeneration.
func (o *Orchestrator) pointFixSentence(ctx context.Context, script *story.Script, sentence string, avoid []string, model, forceModel string, chapterIndex int) (string, string, error) {
	var buf bytes.Buffer
	data := pointFixTemplateData{Sentence: sentence, Avoid: avoid}
	if err := o.tmpls.PointFix.Execute(&buf, data); err != nil {
		return "", "", fmt.Errorf("render point-fix prompt: %w", err)
	}
	prompt := buf.String()

	maxTokens := pointFixInitialMaxTokens
	var lastErr error
	for attempt := 1; attempt <= pointFixMaxAttempts; attempt++ {
		if err := o.checkCostLimit(script); err != nil {
			return "", "", err
		}

		resp, err := o.client.Complete(ctx, prompt, llm.Options{
			Model: model, ForceModel: forceModel, Role: llm.RoleGenerate, MaxTokens: maxTokens,
		})
		if err != nil {
			return "", "", fmt.Errorf("complete: %w", err)
		}
		// Point-fixes only ever happen in response to an already-flagged
		// violation — unconditionally a repair, never an initial pass.
		o.accumulate(script, resp, llm.RoleGenerate, story.CauseRepair, fmt.Sprintf("ch-%d-pointfix", chapterIndex))

		raw, err := llm.ExtractJSON(resp.Text)
		if err != nil {
			lastErr = fmt.Errorf("extract json: %w", err)
			maxTokens = maxTokens*3/2 + 1
			continue
		}
		var cr chapterResponse
		if err := json.Unmarshal([]byte(raw), &cr); err != nil {
			return "", "", fmt.Errorf("parse json: %w", err)
		}
		return cr.Text, cr.DisplayText, nil
	}
	return "", "", lastErr
}
