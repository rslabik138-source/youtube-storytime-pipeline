package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// chapterResponse is the parsed shape of prompts/chapter.tmpl's response.
type chapterResponse struct {
	Text        string `json:"text"`
	DisplayText string `json:"display_text"`
}

// chapterMaxTokens gives the completion enough headroom for both "text"
// and "display_text" (together roughly double the target word count), the
// surrounding JSON structure, the model's tendency to overshoot the target
// by a real margin on the longest chapters (observed ~20% over in
// practice), and whatever thinking tokens it spends before writing the
// visible response. Undersized here is exactly what silently truncates a
// long chapter's JSON mid-string, turning into an "extract json" parse
// failure with no other symptom — so this errs generous. A real run still
// truncated a ~550-word half-chapter at the old *8 multiplier (gemini-3.6-
// flash's reasoning_effort="low" floor spends real, non-trivial thinking
// tokens even at its minimum — confirmed against the native API, see
// thinkingbudget.go), hence *12.
func chapterMaxTokens(targetWords int) int {
	return targetWords*12 + 3000
}

// chapterModel returns the model to use for spec's beat and, when spec.Model
// is set, the same value again as forceModel — llm.Options.ForceModel, so a
// per-beat override (e.g. keeping the hook or the reckoning on a stronger
// model in an otherwise cheaper hybrid setup) survives a provider's own
// per-role model mapping instead of being silently overwritten by it.
func chapterModel(spec config.ChapterSpec, defaultModel string) (model, forceModel string) {
	if spec.Model != "" {
		return spec.Model, spec.Model
	}
	return defaultModel, ""
}

type chapterTemplateData struct {
	Seed          story.Seed
	Bible         story.Bible // always script.Bible.ForChapters() — never the raw Bible, see its doc comment
	Spec          config.ChapterSpec
	MinWords      int             // TargetWords minus the length validator's tolerance
	MaxWords      int             // TargetWords plus the length validator's tolerance
	PriorChapters []story.Chapter // chapters.yaml order, Index < Spec.Index only
	FullContext   bool            // true: template should use .Text; false: use .Summary
	BannedPhrases []string
	Violations    []string // fed back on retry; empty on the first attempt

	// AlreadyDramatized is every prior chapter's summary, presented
	// explicitly as "these scenes are already shown" — distinct from the
	// PriorChapters context block, which is about staying factually
	// consistent. This is specifically about not re-dramatizing the same
	// event as a full scene in two different chapters.
	AlreadyDramatized []string

	// FixInstruction is set when this call is a targeted regeneration
	// (a continuity fix or a review-flagged weak chapter) instead of the
	// first pass — "" on the initial sequential generation.
	FixInstruction string

	// PartNumber/PartTotal are 0/0 for an ordinary, unsplit chapter.
	// When Spec.Split is true, generateChapter issues two calls instead
	// of one; PartNumber is 1 or 2 for those calls, and PriorPartText
	// carries part 1's finished text into part 2's prompt so it can
	// continue directly from it instead of starting the beat over.
	PartNumber    int
	PartTotal     int
	PriorPartText string

	// AvoidParagraphStarts, AvoidPhrases, and MoneyMentionCounts are
	// computed from PriorChapters and fed back into the prompt so the
	// model avoids the whole-script repetition guard's violations before
	// they happen, instead of only finding out after a validation failure
	// (or now, a point-fix). ~150 extra input tokens per chapter buys most
	// script-validation rounds never happening at all.
	AvoidParagraphStarts []string // every paragraph opening (first 5 words) already used in chapters 1..N-1
	AvoidPhrases         []string // every 6-word phrase already used twice — a 3rd use would violate
	MoneyMentionCounts   []string // "$1,200 (mentioned 3 time(s) already)" for every amount seen so far

	// AvoidSentenceOpenings is every two-word sentence opening ("she said",
	// "i knew", ...) already used story.ValidateSentenceOpeningRepetition's
	// cap (8) times across chapters 1..N-1 — one more use anywhere would
	// violate. Unlike the three fields above, this rule previously had NO
	// up-front warning at all (only a whole-script check after every chapter
	// was already written), making it the single biggest un-pre-empted
	// repair driver: ~12 point-fixes/run, purely reactive. Added 2026-08-18.
	AvoidSentenceOpenings []string

	// CrossScriptAvoidPhrases is the top crossScriptAvoidPhraseLimit six-word
	// phrases most repeated across the last crossScriptLookbackScripts
	// *accepted* scripts (see
	// store.TopUsedPhrases) — unlike AvoidPhrases, this is about a
	// DIFFERENT script's text, not this one's, catching the model's
	// tendency to reach for the same signature phrasing script after
	// script (a real comparison of two back-to-back scripts found 17
	// shared six-word phrases). runCrossScriptDedup is the blocking
	// backstop after the fact; this is the cheap up-front nudge that
	// should make it rarely trigger.
	CrossScriptAvoidPhrases []string

	// CloseImageDirective is set only for the close beat: the rotated
	// closing-image archetype (config.Chapters.CloseImages) that dictates
	// this script's final image, so consecutive scripts don't end on the
	// same shape or the same accounting-metaphor register. Empty for every
	// other beat, and empty for close too when no close_images are
	// configured (the beat then falls back to its description alone).
	CloseImageDirective string
}

func priorChapters(script *story.Script, beforeIndex int) []story.Chapter {
	var out []story.Chapter
	for _, c := range script.Chapters {
		if c.Index < beforeIndex {
			out = append(out, c)
		}
	}
	return out
}

func alreadyDramatized(prior []story.Chapter) []string {
	out := make([]string, 0, len(prior))
	for _, c := range prior {
		if s := strings.TrimSpace(c.Summary); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// paragraphStartsUsed collects every paragraph's first 5 words (lowercased,
// matching story.ValidateAntiRepetitionParagraphStarts' own window) already
// used anywhere in prior — every one, not just ones near that validator's
// 3-use limit, since the model should never reach for one again at all.
func paragraphStartsUsed(prior []story.Chapter) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range prior {
		for _, para := range strings.Split(c.Text, "\n\n") {
			words := strings.Fields(para)
			if len(words) < 5 {
				continue
			}
			start := strings.ToLower(strings.Join(words[:5], " "))
			if !seen[start] {
				seen[start] = true
				out = append(out, start)
			}
		}
	}
	return out
}

// phrasesAtRepetitionLimit collects every 6-word phrase already used twice
// in prior (matching story.ValidateAntiRepetitionNGrams' own window and
// limit) — one more use anywhere would trip the whole-script guard.
func phrasesAtRepetitionLimit(prior []story.Chapter) []string {
	const n = 6
	const maxOccurrences = 2

	counts := map[string]int{}
	var order []string
	for _, c := range prior {
		words := strings.Fields(strings.ToLower(c.Text))
		for i := 0; i+n <= len(words); i++ {
			gram := strings.Join(words[i:i+n], " ")
			if counts[gram] == 0 {
				order = append(order, gram)
			}
			counts[gram]++
		}
	}

	var out []string
	for _, gram := range order {
		if counts[gram] >= maxOccurrences {
			out = append(out, gram)
		}
	}
	return out
}

// sentenceOpeningsAtLimit collects every two-word sentence opening already
// used story.ValidateSentenceOpeningRepetition's own limit (8) times in
// prior — one more use anywhere would trip that whole-script guard. Uses
// story.SplitSentences so "opening" is derived exactly the same way the
// validator itself derives it (same sentence boundaries, same first-two-
// words rule), which matters here: a mismatched splitter would silently
// under- or over-count and either warn about openings that aren't actually
// close to the limit, or miss ones that are.
func sentenceOpeningsAtLimit(prior []story.Chapter) []string {
	const maxOccurrences = 8

	counts := map[string]int{}
	var order []string
	for _, c := range prior {
		for _, sent := range story.SplitSentences(c.Text) {
			words := strings.Fields(sent)
			if len(words) < 2 {
				continue
			}
			opening := strings.ToLower(strings.Join(words[:2], " "))
			if counts[opening] == 0 {
				order = append(order, opening)
			}
			counts[opening]++
		}
	}

	var out []string
	for _, opening := range order {
		if counts[opening] >= maxOccurrences {
			out = append(out, opening)
		}
	}
	return out
}

// moneyMentionCountsUsed mirrors story.ValidateMoneyAmountRepetition's own
// pattern and field (DisplayText, where exact amounts actually appear —
// Text has every number spelled out as words), formatted as a per-amount
// running count so the model can see it's approaching (or already at) the
// 5-mention limit before it writes a 6th.
func moneyMentionCountsUsed(prior []story.Chapter) []string {
	counts := map[string]int{}
	var order []string
	for _, c := range prior {
		for _, amount := range moneyAmountPattern.FindAllString(c.DisplayText, -1) {
			if counts[amount] == 0 {
				order = append(order, amount)
			}
			counts[amount]++
		}
	}

	out := make([]string, 0, len(order))
	for _, amount := range order {
		out = append(out, fmt.Sprintf("%s (mentioned %d time(s) already)", amount, counts[amount]))
	}
	return out
}

// moneyAmountPattern mirrors story.ValidateMoneyAmountRepetition's own
// pattern exactly — duplicated here rather than exported, since it's a
// one-line regex and the two packages' reasons for using it are different
// (validating vs proactively avoiding).
var moneyAmountPattern = regexp.MustCompile(`\$[0-9][0-9,]*(?:\.[0-9]+)?`)

// generateChapter renders chapter.tmpl for spec, calls the model, validates
// the result mechanically (length, sentence length, banned phrases, TTS
// digits, beat-specific checks), retries up to Settings.MaxChapterRetries
// feeding violations back, and — on success — makes the separate, cheap
// summary call before returning. fixInstruction carries a continuity or
// review note when this is a targeted regeneration; pass "" otherwise.
//
// spec.Split beats (the largest ones, most exposed to a provider's 503
// "high demand" rejection) are delegated to generateSplitChapter instead,
// which does the same thing across two smaller sequential calls.
func (o *Orchestrator) generateChapter(ctx context.Context, script *story.Script, spec config.ChapterSpec, fixInstruction string) (story.Chapter, error) {
	// Reset the peak-violations scratch counter (see PeakViolations doc)
	// so runChapters' early-abort check reads only what THIS call saw.
	o.lastChapterPeakViolations = 0

	if spec.Split {
		return o.generateSplitChapter(ctx, script, spec, fixInstruction)
	}

	prior := priorChapters(script, spec.Index)
	part, err := o.generateChapterPart(ctx, script, spec, spec.TargetWords, prior, fixInstruction, 0, 0, "")
	if err != nil {
		return story.Chapter{}, err
	}

	ch := story.Chapter{Index: spec.Index, Beat: spec.Beat, TargetWords: spec.TargetWords, Text: part.Text, DisplayText: part.DisplayText}
	summary, err := o.generateSummary(ctx, script, ch, causeFor(fixInstruction))
	if err != nil {
		return story.Chapter{}, fmt.Errorf("summary: %w", err)
	}
	ch.Summary = summary
	return ch, nil
}

// causeFor derives story.CauseInitial/story.CauseRepair from
// fixInstruction the same way every chapter-generation call site already
// distinguishes a first pass from a targeted fix: "" means the initial
// sequential generation (runChapters), anything else means this call
// exists to repair a continuity, repetition-guard, or review violation.
func causeFor(fixInstruction string) string {
	if fixInstruction == "" {
		return story.CauseInitial
	}
	return story.CauseRepair
}

// generateSplitChapter generates a large beat as two sequential calls: part
// A (roughly the first half) and part B (the continuation, given part A's
// finished text as context so it picks up the scene rather than restarting
// it). Each part is validated and retried independently against its own
// half-sized target — so a validation failure or a provider rejection on
// part B never throws away part A's already-accepted text. Only once both
// parts pass on their own does the combined chapter get one final
// full-chapter validation pass (covering checks, like overall length, that
// only make sense once the beat is whole); a failure there is rare enough
// (each half already met its own bar) that it simply falls back to
// regenerating both parts.
func (o *Orchestrator) generateSplitChapter(ctx context.Context, script *story.Script, spec config.ChapterSpec, fixInstruction string) (story.Chapter, error) {
	maxAttempts := o.settings.MaxChapterRetries
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	prior := priorChapters(script, spec.Index)
	partAWords := (spec.TargetWords + 1) / 2
	partBWords := spec.TargetWords - partAWords

	chapterCfg := story.DefaultChapterValidatorConfig(o.banned)
	chapterCfg.Bible = script.Bible

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return story.Chapter{}, err
		}

		partA, err := o.generateChapterPart(ctx, script, spec, partAWords, prior, fixInstruction, 1, 2, "")
		if err != nil {
			return story.Chapter{}, fmt.Errorf("part A: %w", err)
		}

		partB, err := o.generateChapterPart(ctx, script, spec, partBWords, prior, fixInstruction, 2, 2, partA.Text)
		if err != nil {
			return story.Chapter{}, fmt.Errorf("part B: %w", err)
		}

		ch := story.Chapter{
			Index: spec.Index, Beat: spec.Beat, TargetWords: spec.TargetWords,
			Text:        strings.TrimSpace(partA.Text) + "\n\n" + strings.TrimSpace(partB.Text),
			DisplayText: strings.TrimSpace(partA.DisplayText) + "\n\n" + strings.TrimSpace(partB.DisplayText),
		}

		violationsAll := story.ValidateChapter(ch, chapterCfg)
		errs := errorViolationsOnly(violationsAll)
		if len(errs) == 0 {
			if warns := warningViolationsOnly(violationsAll); len(warns) > 0 {
				o.logger.Warn("split chapter passed with style warnings", "chapter", spec.Index, "beat", spec.Beat, "warnings", violationMessages(warns))
			}
			summary, err := o.generateSummary(ctx, script, ch, causeFor(fixInstruction))
			if err != nil {
				return story.Chapter{}, fmt.Errorf("summary: %w", err)
			}
			ch.Summary = summary
			return ch, nil
		}

		if len(errs) > o.lastChapterPeakViolations {
			o.lastChapterPeakViolations = len(errs)
		}
		msgs := violationMessages(errs)
		o.logger.Warn("split chapter combined validation failed, regenerating both parts", "chapter", spec.Index, "beat", spec.Beat, "attempt", attempt, "violations", msgs)
		lastErr = fmt.Errorf("chapter %d (split) failed validation: %s", spec.Index, strings.Join(msgs, "; "))
	}

	return story.Chapter{}, fmt.Errorf("exhausted %d attempts: %w", maxAttempts, lastErr)
}

// generateChapterPart renders and validates a single LLM call against
// partTargetWords — either a whole ordinary chapter (partNumber 0) or one
// half of a split chapter (partNumber 1 or 2, with priorPartText set for
// part 2). It tracks two independent retry budgets: MaxChapterRetries for
// content-quality failures (validation violations) and MaxTechnicalRetries
// for truncated/malformed completions (a token-budget problem, not a
// quality one) — a truncation bumps MaxTokens by 50% and retries without
// spending a quality attempt, so a chapter doesn't fail outright just
// because one response got cut off.
func (o *Orchestrator) generateChapterPart(ctx context.Context, script *story.Script, spec config.ChapterSpec, partTargetWords int, prior []story.Chapter, fixInstruction string, partNumber, partTotal int, priorPartText string) (chapterResponse, error) {
	maxQualityAttempts := o.settings.MaxChapterRetries
	if maxQualityAttempts <= 0 {
		maxQualityAttempts = 3
	}
	maxTechnicalAttempts := o.settings.MaxTechnicalRetries
	if maxTechnicalAttempts <= 0 {
		maxTechnicalAttempts = 3
	}

	chapterCfg := story.DefaultChapterValidatorConfig(o.banned)
	chapterCfg.Bible = script.Bible
	// One half of a split chapter shouldn't each need the full dialogue-
	// line minimum on their own — that's checked once, against the
	// combined text, in generateSplitChapter's own validation pass.
	chapterCfg.SkipDialogueRequirement = partNumber > 0
	tolerance := chapterCfg.LengthTolerance
	if tolerance <= 0 {
		tolerance = 0.20
	}
	minWords := int(float64(partTargetWords) * (1 - tolerance))
	maxWords := int(float64(partTargetWords) * (1 + tolerance))

	partSpec := spec
	partSpec.TargetWords = partTargetWords

	maxTokens := chapterMaxTokens(partTargetWords)

	avoidParagraphStarts := paragraphStartsUsed(prior)
	avoidPhrases := phrasesAtRepetitionLimit(prior)
	moneyMentionCounts := moneyMentionCountsUsed(prior)
	avoidSentenceOpenings := sentenceOpeningsAtLimit(prior)

	// Best-effort: a store error here shouldn't fail chapter generation
	// over what's purely a cost-saving nudge — runCrossScriptDedup is the
	// real (blocking) backstop for this after the fact.
	crossScriptAvoidPhrases, err := o.store.TopUsedPhrases(ctx, crossScriptLookbackScripts, crossScriptAvoidPhraseLimit)
	if err != nil {
		o.logger.Warn("could not load cross-script avoid-phrases, continuing without them", "error", err)
	}

	// Closing-image rotation, close beat only: pick an archetype by the
	// count of recorded scripts so consecutive scripts don't end on the
	// same final image or the same accounting-metaphor register. mod len
	// cycles through every archetype before repeating. Best-effort — a
	// store hiccup falls back to the first archetype (still better than the
	// old single fixed close).
	var closeImageDirective string
	if partSpec.Beat == "close" && len(o.chapters.CloseImages) > 0 {
		idx := 0
		if n, err := o.store.CountRecordedScripts(ctx); err != nil {
			o.logger.Warn("could not count recorded scripts for closing-image rotation, using first archetype", "error", err)
		} else {
			idx = n % len(o.chapters.CloseImages)
		}
		img := o.chapters.CloseImages[idx]
		closeImageDirective = img.Directive
		o.logger.Info("closing-image archetype selected", "id", img.ID, "index", idx)
	}

	var violations []string
	var lastErr error
	qualityAttempt, technicalAttempt := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return chapterResponse{}, err
		}
		if qualityAttempt >= maxQualityAttempts {
			return chapterResponse{}, fmt.Errorf("exhausted %d quality attempts: %w", maxQualityAttempts, lastErr)
		}
		if technicalAttempt >= maxTechnicalAttempts {
			return chapterResponse{}, fmt.Errorf("exhausted %d technical attempts: %w", maxTechnicalAttempts, lastErr)
		}
		if err := o.checkCostLimit(script); err != nil {
			return chapterResponse{}, err
		}

		var buf bytes.Buffer
		data := chapterTemplateData{
			Seed: script.Seed, Bible: script.Bible.ForChapters(), Spec: partSpec, MinWords: minWords, MaxWords: maxWords,
			PriorChapters: prior, FullContext: o.settings.FullContext, BannedPhrases: o.banned,
			AlreadyDramatized: alreadyDramatized(prior),
			Violations:        violations, FixInstruction: fixInstruction,
			PartNumber: partNumber, PartTotal: partTotal, PriorPartText: priorPartText,
			AvoidParagraphStarts: avoidParagraphStarts, AvoidPhrases: avoidPhrases, MoneyMentionCounts: moneyMentionCounts,
			AvoidSentenceOpenings:   avoidSentenceOpenings,
			CrossScriptAvoidPhrases: crossScriptAvoidPhrases,
			CloseImageDirective:     closeImageDirective,
		}
		if err := o.tmpls.Chapter.Execute(&buf, data); err != nil {
			return chapterResponse{}, fmt.Errorf("render chapter prompt: %w", err)
		}

		model, forceModel := chapterModel(partSpec, o.settings.GenerateModel)
		resp, err := o.client.Complete(ctx, buf.String(), llm.Options{
			Model: model, ForceModel: forceModel, Role: llm.RoleGenerate, MaxTokens: maxTokens,
		})
		if err != nil {
			return chapterResponse{}, fmt.Errorf("complete: %w", err)
		}
		o.accumulate(script, resp, llm.RoleGenerate, causeFor(fixInstruction), chapterLabel(spec.Index, partNumber))

		raw, jsonErr := llm.ExtractJSON(resp.Text)
		var cr chapterResponse
		if jsonErr == nil {
			jsonErr = json.Unmarshal([]byte(raw), &cr)
		}
		if jsonErr != nil {
			technicalAttempt++
			oldMaxTokens := maxTokens
			maxTokens = maxTokens*3/2 + 1
			logArgs := []any{"chapter", spec.Index, "beat", spec.Beat, "technical_attempt", technicalAttempt, "old_max_tokens", oldMaxTokens, "new_max_tokens", maxTokens, "error", jsonErr}
			if partNumber > 0 {
				logArgs = append(logArgs, "part", partNumber)
			}
			o.logger.Warn("chapter completion truncated or malformed, raising token budget and retrying (not a quality attempt)", logArgs...)
			lastErr = fmt.Errorf("extract/parse json: %w", jsonErr)
			continue
		}

		// Insert any dropped sentence-ending punctuation before validating —
		// a pure mechanical fix (see fixMissingPunctuation), so a model
		// output missing a period doesn't burn a whole quality-retry
		// attempt on something a regex already resolves for free.
		if fixed, n := fixMissingPunctuation(cr.Text); n > 0 {
			cr.Text = fixed
		}
		if fixed, n := fixMissingPunctuation(cr.DisplayText); n > 0 {
			cr.DisplayText = fixed
		}

		partCh := story.Chapter{Index: spec.Index, Beat: spec.Beat, TargetWords: partTargetWords, Text: cr.Text, DisplayText: cr.DisplayText}
		violationsAll := story.ValidateChapter(partCh, chapterCfg)
		errs := errorViolationsOnly(violationsAll)
		if len(errs) == 0 {
			if warns := warningViolationsOnly(violationsAll); len(warns) > 0 {
				logArgs := []any{"chapter", spec.Index, "beat", spec.Beat, "warnings", violationMessages(warns)}
				if partNumber > 0 {
					logArgs = append(logArgs, "part", partNumber)
				}
				o.logger.Warn("chapter passed with style warnings", logArgs...)
			}
			return cr, nil
		}

		qualityAttempt++
		if len(errs) > o.lastChapterPeakViolations {
			o.lastChapterPeakViolations = len(errs)
		}
		msgs := violationMessages(errs)
		logArgs := []any{"chapter", spec.Index, "beat", spec.Beat, "quality_attempt", qualityAttempt, "violations", msgs}
		if partNumber > 0 {
			logArgs = append(logArgs, "part", partNumber)
		}
		o.logger.Warn("chapter validation failed, retrying", logArgs...)
		violations = msgs
		lastErr = fmt.Errorf("chapter %d failed validation: %s", spec.Index, strings.Join(msgs, "; "))
	}
}

// chapterLabel is the short, human-readable location logCostLine prints
// for a chapter-generation or summary call — "ch-7", or "ch-7-part1"/
// "ch-7-part2" for a split chapter's two halves (partNumber 0 means an
// ordinary, unsplit chapter).
func chapterLabel(chapterIndex, partNumber int) string {
	if partNumber > 0 {
		return fmt.Sprintf("ch-%d-part%d", chapterIndex, partNumber)
	}
	return fmt.Sprintf("ch-%d", chapterIndex)
}

// generateSummary deliberately has NO pre-call cost check, unlike every
// other LLM call site in this file: it always runs right after its
// chapter's body already succeeded but before runChapters saves that
// chapter (see generateChapter) — refusing here would throw away an
// already-generated, not-yet-persisted chapter instead of just skipping
// one more attempt. The existing post-save checkCostLimit in runChapters
// still catches an overage right after this chapter (body + summary) is
// safely on disk.
func (o *Orchestrator) generateSummary(ctx context.Context, script *story.Script, ch story.Chapter, cause string) (string, error) {
	var buf bytes.Buffer
	if err := o.tmpls.Summary.Execute(&buf, ch); err != nil {
		return "", fmt.Errorf("render summary prompt: %w", err)
	}

	resp, err := o.client.Complete(ctx, buf.String(), llm.Options{Model: o.settings.SummaryModel, Role: llm.RoleSummary, MaxTokens: 1000})
	if err != nil {
		return "", fmt.Errorf("complete: %w", err)
	}
	o.accumulate(script, resp, llm.RoleSummary, cause, fmt.Sprintf("ch-%d-summary", ch.Index))

	return strings.TrimSpace(resp.Text), nil
}
