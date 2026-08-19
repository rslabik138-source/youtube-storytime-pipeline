// Package generate is the orchestrator: seed -> bible -> chapters (each with
// a separate summary call) -> continuity check -> review -> save. Every
// dependency (llm.Client, the three templates this package renders
// directly, the continuity.Checker, the review.Reviewer, the store.Store)
// is injected, so the whole pipeline runs end-to-end against fakes in
// tests, no network and no real database required.
package generate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/continuity"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/review"
	"github.com/placeholder/scenario/internal/seed"
	"github.com/placeholder/scenario/internal/store"
	"github.com/placeholder/scenario/internal/story"
)

// Templates bundles the three prompts this package renders directly.
// internal/continuity and internal/review own continuity.tmpl and
// review.tmpl themselves, since each runs as one self-contained call.
type Templates struct {
	Bible    *template.Template
	Chapter  *template.Template
	Summary  *template.Template
	PointFix *template.Template
}

// Options controls one Generate call.
type Options struct {
	Profession string // "" lets seed.Generator choose freely
	DryRun     bool   // seed + bible only: no chapters, no continuity, no review, nothing saved
}

// Orchestrator runs the full pipeline. Every field is a dependency injected
// by cmd/gen (or a test) — this package never constructs its own client,
// store, or templates.
type Orchestrator struct {
	seedGen    *seed.Generator
	client     llm.Client
	tmpls      Templates
	continuity *continuity.Checker
	review     *review.Reviewer
	store      store.Store
	chapters   config.Chapters
	banned     []string
	settings   config.Settings
	pricing    config.Pricing
	logger     *slog.Logger

	// lastChapterPeakViolations is scratch state, not general shared
	// state: generateChapter resets it to 0 at the start of every call,
	// and its own retry loops (in generateChapterPart/generateSplitChapter)
	// raise it to the largest single-attempt blocking-violation count they
	// saw. runChapters reads it right after each call returns, to power
	// the "first 3 chapters are all clearly struggling — the prompt is
	// broken, not the seed" early-abort check, without threading an extra
	// return value through every generateChapter call site (continuity
	// fixes, review fixes, script-validation fixes, RegenerateChapter —
	// none of which care about it).
	lastChapterPeakViolations int
}

func NewOrchestrator(
	seedGen *seed.Generator,
	client llm.Client,
	tmpls Templates,
	continuityChecker *continuity.Checker,
	reviewer *review.Reviewer,
	st store.Store,
	chapters config.Chapters,
	banned []string,
	settings config.Settings,
	pricing config.Pricing,
	logger *slog.Logger,
) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Orchestrator{
		seedGen: seedGen, client: client, tmpls: tmpls, continuity: continuityChecker,
		review: reviewer, store: st, chapters: chapters, banned: banned, settings: settings, pricing: pricing, logger: logger,
	}
}

// Generate runs the whole pipeline once and returns the resulting script.
// A rejected script is a valid terminal state, not a Go error — it's saved
// (per the brief: "saved for analysis") and returned with Status ==
// story.StatusRejected. An error return means the pipeline broke partway
// through (an LLM call failed every retry, a template wouldn't render, a
// save failed) — every chapter completed before the failure is already
// persisted, so the caller (cmd/gen's `--resume`) can continue from there
// instead of starting over.
func (o *Orchestrator) Generate(ctx context.Context, rng *rand.Rand, opts Options) (*story.Script, error) {
	sd, err := o.drawSeed(ctx, rng, opts.Profession)
	if err != nil {
		return nil, fmt.Errorf("generate: draw seed: %w", err)
	}

	script := &story.Script{
		ID:        uuid.NewString(),
		CreatedAt: time.Now().UTC(),
		Seed:      sd,
		Status:    story.StatusPending,
		Model:     o.settings.GenerateModel,
	}

	title, bible, err := o.generateBible(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("generate: bible: %w", err)
	}
	script.Title = title
	script.Bible = bible

	if opts.DryRun {
		return script, nil
	}

	// Persist right after the bible — before a single chapter is written —
	// so even a failure on chapter 1 leaves a resumable, pending script
	// instead of losing the seed/bible draw entirely.
	if err := o.store.SaveScript(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: save script: %w", err)
	}

	if err := o.runChapters(ctx, script); err != nil {
		return nil, err
	}
	return o.finishPipeline(ctx, script)
}

// Resume continues an existing pending script from wherever chapter
// generation left off — `gen generate --resume`'s entry point. It never
// regenerates the seed, the bible, or a chapter that already exists.
func (o *Orchestrator) Resume(ctx context.Context, scriptID string) (*story.Script, error) {
	script, err := o.store.GetScript(ctx, scriptID)
	if err != nil {
		return nil, fmt.Errorf("generate: resume: load script: %w", err)
	}
	if script.Status != story.StatusPending {
		return nil, fmt.Errorf("generate: resume: script %s is not pending (status=%s) — nothing to resume", scriptID, script.Status)
	}

	if err := o.runChapters(ctx, script); err != nil {
		return nil, err
	}
	return o.finishPipeline(ctx, script)
}

// earlyAbortSampleSize/earlyAbortViolationThreshold power runChapters'
// early-abort check: if each of the first earlyAbortSampleSize freshly
// generated chapters needed more than earlyAbortViolationThreshold
// blocking violations fixed in a single attempt, that's a prompt problem,
// not a seed problem — the remaining chapters would show the same trouble,
// so it's cheaper to stop now than to pay for all 16.
const (
	earlyAbortSampleSize         = 3
	earlyAbortViolationThreshold = 5
)

// ErrPromptLikelyBroken is runChapters' early-abort error — see
// earlyAbortSampleSize's doc comment.
var ErrPromptLikelyBroken = errors.New("generate: the first chapters show heavy, consistent validation trouble — likely a prompt problem, not this seed")

// runChapters generates every chapter in o.chapters.Chapters that isn't
// already present in script.Chapters, saving progress to the store after
// each one succeeds — so a failure partway through (a rate limit, a
// provider outage, an unrecoverable validation failure) leaves every prior
// chapter safely persisted instead of losing the tokens already spent.
func (o *Orchestrator) runChapters(ctx context.Context, script *story.Script) error {
	done := make(map[int]bool, len(script.Chapters))
	for _, ch := range script.Chapters {
		done[ch.Index] = true
	}

	generated, troubled := 0, 0
	for _, spec := range o.chapters.Chapters {
		if done[spec.Index] {
			continue
		}

		// cta beats are filled from the rotated config pool, never the LLM —
		// this is what stops verbatim-identical CTAs across scripts. They
		// cost nothing and skip the LLM early-abort sampling below.
		if variants := o.chapters.CTA.For(spec.Beat); len(variants) > 0 {
			ch := o.buildCTAChapter(ctx, script, spec, variants)
			replaceChapter(script, ch)
			sortChapters(script)
			script.RecomputeWordCount()
			if err := o.store.SaveScript(ctx, script); err != nil {
				return fmt.Errorf("generate: save progress after chapter %d: %w", spec.Index, err)
			}
			continue
		}

		ch, err := o.generateChapter(ctx, script, spec, "")
		if err != nil {
			return fmt.Errorf("generate: chapter %d (%s): %w", spec.Index, spec.Beat, err)
		}
		replaceChapter(script, ch)
		sortChapters(script)
		script.RecomputeWordCount()

		if err := o.store.SaveScript(ctx, script); err != nil {
			return fmt.Errorf("generate: save progress after chapter %d: %w", spec.Index, err)
		}
		if err := o.checkCostLimit(script); err != nil {
			return err
		}

		generated++
		if generated <= earlyAbortSampleSize {
			if o.lastChapterPeakViolations > earlyAbortViolationThreshold {
				troubled++
			}
			if generated == earlyAbortSampleSize && troubled == earlyAbortSampleSize {
				o.logger.Warn("the first chapters all needed heavy validation fixes, stopping early — check the prompt, not this seed",
					"sample_size", earlyAbortSampleSize, "violation_threshold", earlyAbortViolationThreshold)
				return fmt.Errorf("%w (each of the first %d had over %d violations in a single attempt)",
					ErrPromptLikelyBroken, earlyAbortSampleSize, earlyAbortViolationThreshold)
			}
		}
	}
	return nil
}

// buildCTAChapter fills a cta beat from the rotated config pool instead of
// the LLM (config.CTASet) — the fix for verbatim-identical CTAs across
// scripts. The variant is chosen by recorded-script count so consecutive
// scripts pick different copy (mod len cycles through all of them before
// repeating); "{object}" is replaced with the seed's object_container.
// Best-effort on the count: a store hiccup just falls back to the first
// variant, still better than an LLM-generated CTA that may echo another
// script.
func (o *Orchestrator) buildCTAChapter(ctx context.Context, script *story.Script, spec config.ChapterSpec, variants []string) story.Chapter {
	idx := 0
	if n, err := o.store.CountRecordedScripts(ctx); err != nil {
		o.logger.Warn("could not count recorded scripts for cta rotation, using first variant", "beat", spec.Beat, "error", err)
	} else {
		idx = n % len(variants)
	}
	text := strings.ReplaceAll(variants[idx], "{object}", script.Seed.ObjectContainer)
	o.logger.Info("cta filled from config (not LLM)", "beat", spec.Beat, "variant", idx)
	return story.Chapter{
		Index:       spec.Index,
		Beat:        spec.Beat,
		TargetWords: spec.TargetWords,
		Text:        text,
		DisplayText: text,
		Summary:     "Direct address to the listener — a call to action (subscribe, comment). No plot events.",
	}
}

// finishPipeline runs continuity, review, and the final save/acceptance —
// the shared tail for both a fresh Generate and a Resume.
func (o *Orchestrator) finishPipeline(ctx context.Context, script *story.Script) (*story.Script, error) {
	if err := o.runContinuity(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: continuity: %w", err)
	}
	script.RecomputeWordCount()

	if err := o.runScriptValidation(ctx, script); err != nil {
		if !errors.Is(err, ErrValidationRoundsExhausted) {
			return nil, fmt.Errorf("generate: script validation: %w", err)
		}
		// Stop here on purpose — don't run review on a script whose
		// repetition guard never converged. Real money was already spent
		// generating and repairing this script; keep it (status
		// accepted_with_warnings) instead of throwing that away over
		// mechanical violations a human can glance at, rather than paying
		// for review on top.
		o.logger.Warn("script validation rounds exhausted, accepting with warnings instead of discarding",
			"cost_usd", scriptCostUSD(script.Usage, o.pricing))
		// Still run cross-script dedup so a warnings-script doesn't reuse
		// another produced script's signature phrasings. Soft-fail: this
		// script already failed to converge on within-script validation,
		// so keep it rather than discarding it over unresolved
		// cross-script overlap (mirrors the accept-with-warnings choice).
		if e := o.runCrossScriptDedup(ctx, script); e != nil {
			o.logger.Warn("cross-script dedup did not fully resolve on accepted-with-warnings script, keeping as-is", "error", e)
		}
		return o.acceptWithWarnings(ctx, script)
	}
	script.RecomputeWordCount()

	if err := o.runCrossScriptDedup(ctx, script); err != nil {
		if !errors.Is(err, ErrCrossScriptDedupExhausted) {
			return nil, fmt.Errorf("generate: cross-script dedup: %w", err)
		}
		o.logger.Warn("cross-script dedup rounds exhausted, accepting with warnings instead of discarding",
			"cost_usd", scriptCostUSD(script.Usage, o.pricing))
		// fixFlaggedChapters regenerates whole chapters to resolve
		// cross-script overlap without re-checking runScriptValidation's
		// rules afterward — a regeneration here can reintroduce exactly the
		// within-script violations that stage already cleared (a refrain
		// echo, a repeated ngram). One more bounded, soft-fail pass catches
		// those instead of shipping them silently; if it still doesn't
		// converge, accept anyway — the script already survived two repair
		// passes and discarding it now would waste the money already spent.
		if e := o.runScriptValidation(ctx, script); e != nil && !errors.Is(e, ErrValidationRoundsExhausted) {
			return nil, fmt.Errorf("generate: post-dedup validation: %w", e)
		}
		return o.acceptWithWarnings(ctx, script)
	}
	script.RecomputeWordCount()

	if err := o.runReview(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: review: %w", err)
	}
	script.RecomputeWordCount()

	// Final save: SaveScript is a full upsert, so this one call persists
	// whatever continuity/review changed (chapter text, word count) plus
	// the final accepted/rejected status, all in one transaction.
	if err := o.store.SaveScript(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: final save: %w", err)
	}
	if script.Status == story.StatusAccepted {
		if err := o.store.RecordAcceptance(ctx, script); err != nil {
			return nil, fmt.Errorf("generate: record acceptance: %w", err)
		}
	}

	return script, nil
}

// acceptWithWarnings marks script accepted_with_warnings, saves it, and
// records it for future cross-script dedup lookups — the shared tail for
// finishPipeline's two repetition-guard-exhaustion paths (within-script
// validation and cross-script dedup), skipping review either way since
// paying for a quality check on a script already known to have unresolved
// mechanical violations isn't worth it.
func (o *Orchestrator) acceptWithWarnings(ctx context.Context, script *story.Script) (*story.Script, error) {
	script.Status = story.StatusAcceptedWithWarnings
	script.RecomputeWordCount()
	if err := o.store.SaveScript(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: final save (accepted with warnings): %w", err)
	}
	if err := o.store.RecordAcceptance(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: record acceptance: %w", err)
	}
	return script, nil
}

func sortChapters(script *story.Script) {
	sort.Slice(script.Chapters, func(i, j int) bool { return script.Chapters[i].Index < script.Chapters[j].Index })
}

func (o *Orchestrator) drawSeed(ctx context.Context, rng *rand.Rand, profession string) (story.Seed, error) {
	if profession != "" {
		return o.seedGen.NextWithProfession(ctx, rng, profession)
	}
	return o.seedGen.Next(ctx, rng)
}

// RegenerateChapter reloads scriptID from the store and regenerates exactly
// one chapter — validated and retried through the same generateChapter path
// as the main pipeline — then persists the result. This is `gen regenerate
// --chapter N`'s entry point: a single-chapter fix that doesn't require
// rerunning bible generation, continuity, or review.
func (o *Orchestrator) RegenerateChapter(ctx context.Context, scriptID string, chapterIndex int) (*story.Script, error) {
	script, err := o.store.GetScript(ctx, scriptID)
	if err != nil {
		return nil, fmt.Errorf("generate: regenerate chapter: load script: %w", err)
	}
	spec, ok := o.chapterSpec(chapterIndex)
	if !ok {
		return nil, fmt.Errorf("generate: regenerate chapter: no chapter %d in the chapters config", chapterIndex)
	}

	ch, err := o.generateChapter(ctx, script, spec, "manual regeneration requested via `gen regenerate`")
	if err != nil {
		return nil, fmt.Errorf("generate: regenerate chapter %d: %w", chapterIndex, err)
	}
	replaceChapter(script, ch)
	script.RecomputeWordCount()

	if err := o.store.SaveScript(ctx, script); err != nil {
		return nil, fmt.Errorf("generate: regenerate chapter: save: %w", err)
	}
	return script, nil
}

// accumulate folds one LLM response's cost and provider into script — the
// only per-call bookkeeping generate.go does directly. continuity and
// review's own token usage isn't folded in here since those packages
// return parsed results, not raw llm.Response. cause is story.CauseInitial
// or story.CauseRepair (see that type's doc comment); label is a short,
// human-readable location for the console cost line (logCostLine) — "ch-7",
// "ch-7-summary", "ch-7-pointfix", "bible", "continuity", "review".
func (o *Orchestrator) accumulate(script *story.Script, resp llm.Response, role llm.Role, cause, label string) {
	script.RecordUsage(string(role), resp.Provider, resp.Model, resp.TokensIn, resp.TokensOut, resp.ThinkingTokens, cause)
	o.logger.Info("llm call usage", "role", role, "provider", resp.Provider, "model", resp.Model,
		"tokens_in", resp.TokensIn, "tokens_out", resp.TokensOut, "thinking_tokens", resp.ThinkingTokens, "cause", cause)
	o.logCostLine(script, resp, role, label)
}

// logCostLine prints one compact, human-scannable line per LLM call to
// stderr — real numbers straight from THIS call's own usage metadata, not
// a controlled test run after the fact. Every Gemini response already
// reports its thinking-token count; if think=N is nonzero here on a role
// whose thinking_budget is configured at its documented minimum, that's
// the provider not honoring the requested budget, visible immediately on
// the call that did it instead of only discoverable later in `gen stats`'s
// aggregate. Deliberately separate from the structured "llm call usage"
// slog line above — that one is for grep/log-aggregation, this one is for
// a human watching a run in a terminal.
func (o *Orchestrator) logCostLine(script *story.Script, resp llm.Response, role llm.Role, label string) {
	callCost, _ := callCostUSD(resp.Model, resp.TokensIn, resp.TokensOut, o.pricing)
	runCost := scriptCostUSD(script.Usage, o.pricing)
	fmt.Fprintf(os.Stderr, "[cost] %s/%s %s  in=%d out=%d think=%d  $%.4f  run=$%.4f\n",
		role, label, resp.Model, resp.TokensIn, resp.TokensOut, resp.ThinkingTokens, callCost, runCost)
}

func errorViolationsOnly(vs []story.Violation) []story.Violation {
	var out []story.Violation
	for _, v := range vs {
		if v.Severity == story.SeverityError {
			out = append(out, v)
		}
	}
	return out
}

func warningViolationsOnly(vs []story.Violation) []story.Violation {
	var out []story.Violation
	for _, v := range vs {
		if v.Severity == story.SeverityWarning {
			out = append(out, v)
		}
	}
	return out
}

func violationMessages(vs []story.Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Message
	}
	return out
}

func replaceChapter(script *story.Script, ch story.Chapter) {
	for i, c := range script.Chapters {
		if c.Index == ch.Index {
			script.Chapters[i] = ch
			return
		}
	}
	script.Chapters = append(script.Chapters, ch)
}

func (o *Orchestrator) chapterSpec(index int) (config.ChapterSpec, bool) {
	for _, c := range o.chapters.Chapters {
		if c.Index == index {
			return c, true
		}
	}
	return config.ChapterSpec{}, false
}
