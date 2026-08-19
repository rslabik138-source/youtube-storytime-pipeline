package generate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/placeholder/scenario/internal/story"
)

// ErrValidationRoundsExhausted means runScriptValidation never fully
// converged within Settings.MaxRepetitionRounds — some mechanical
// violations (a repeated phrase, a placement rule) are still unresolved.
// finishPipeline treats this specially: instead of discarding the script
// (real money was already spent generating and repairing it), it accepts
// the script with story.StatusAcceptedWithWarnings and stops, skipping
// cross-script dedup and review rather than spending further on a script
// that's already shown it doesn't converge easily.
var ErrValidationRoundsExhausted = errors.New("generate: script validation: rounds exhausted with unresolved violations")

// runScriptValidation is the whole-script mechanical validation gate that
// runs after continuity: 6-word phrase repetition, sentence-opening
// repetition, refrain-phrase placement (exactly chapters 6 and 13), and
// exact money-amount repetition, plus a defense-in-depth re-check of every
// per-chapter rule. This is what makes the anti-repetition guard actually
// BLOCKING — previously these checks existed in internal/story but were
// never invoked by the pipeline.
//
// Before spending any model calls, a deterministic pass removes exact
// duplicate sentences for free (dedupeExactRepeatedSentences). For
// whatever violations remain, a chapter with MaxViolationsForPointFix (8)
// or fewer gets point-fixed — one small rewrite call per violation instead
// of a full chapter regeneration (~12,000 tokens) — and
// only falls back to a full regeneration if a point-fix can't be applied
// (see pointFixChapter). A chapter with more violations than that goes
// straight to full regeneration: patching that many sentences risks
// leaving it incoherent, and a fresh pass is more reliable. Either way the
// whole script is re-validated — up to Settings.MaxRepetitionRounds
// rounds, since fixing one chapter can shift which occurrence of a
// repeated element is now the "extra" one.
//
// MaxRepetitionRounds is deliberately small (2, not the 6 an earlier
// version used): each round can trigger a full regeneration per flagged
// chapter, and 6 rounds x 16 chapters is up to 96 extra generation calls —
// real spend on a script that, per real runs, still didn't converge and
// got discarded anyway. See ErrValidationRoundsExhausted: exhausting the
// budget now keeps the script (accepted with warnings) instead of that.
func (o *Orchestrator) runScriptValidation(ctx context.Context, script *story.Script) error {
	maxRounds := o.settings.MaxRepetitionRounds
	if maxRounds <= 0 {
		// Raised 2->4 on 2026-08-19: a validate pass across the 4 most
		// recently accepted scripts found every single one had exhausted
		// the old 2-round budget and shipped with 25-51 unresolved blocking
		// violations apiece — including missing sentence-ending punctuation
		// that reaches the TTS as a run-on (audible to a viewer, not just
		// mechanical) and the seeded_line/refrain echoed verbatim 3-5 times.
		// 4 rounds is still far short of the old 6-round version this file's
		// history says didn't converge either, but combined with re-running
		// the free fixes below every round (previously only once, before
		// round 1), most of what round 1-2 used to fail on now clears
		// before round 3.
		maxRounds = 4
	}
	chapterCfg := story.DefaultChapterValidatorConfig(o.banned)
	chapterCfg.Bible = script.Bible

	if err := o.runFreeMechanicalFixes(ctx, script); err != nil {
		return err
	}

	var lastViolationCount int
	for round := 1; round <= maxRounds; round++ {
		violations := errorViolationsOnly(story.ValidateScript(script, chapterCfg))
		if len(violations) == 0 {
			return nil
		}
		lastViolationCount = len(violations)

		o.logger.Warn("script validation found violations, regenerating flagged chapters", "round", round, "chapters", distinctChapters(violations))
		if err := o.fixFlaggedChapters(ctx, script, violations); err != nil {
			return fmt.Errorf("script validation: %w", err)
		}

		// A full chapter regeneration in fixFlaggedChapters can reintroduce
		// exactly the mechanical issues these two fixes exist to catch
		// (a duplicate sentence, a dropped sentence-ending period) — rerun
		// them after every round, not just once before round 1, so a fresh
		// instance gets cleared for free instead of burning round budget or
		// shipping unresolved.
		if err := o.runFreeMechanicalFixes(ctx, script); err != nil {
			return err
		}
	}

	return fmt.Errorf("%w: %d rounds, %d violations still unresolved", ErrValidationRoundsExhausted, maxRounds, lastViolationCount)
}

// runFreeMechanicalFixes applies the two zero-cost, deterministic repairs —
// dedupeExactRepeatedSentences and fixMissingPunctuationInScript — and
// persists the result if either changed anything. No model call either way;
// called once before round 1 and again after every repair round (see
// runScriptValidation).
func (o *Orchestrator) runFreeMechanicalFixes(ctx context.Context, script *story.Script) error {
	changed := false

	if removed := dedupeExactRepeatedSentences(script); removed > 0 {
		o.logger.Info("removed exact duplicate sentences (no model call needed)", "count", removed)
		changed = true
	}
	if inserted := fixMissingPunctuationInScript(script); inserted > 0 {
		o.logger.Info("inserted missing sentence-ending punctuation (no model call needed)", "count", inserted)
		changed = true
	}
	if !changed {
		return nil
	}

	script.RecomputeWordCount()
	if err := o.store.SaveScript(ctx, script); err != nil {
		return fmt.Errorf("save progress after free mechanical fixes: %w", err)
	}
	return o.checkCostLimit(script)
}

// distinctChapters counts how many distinct, chapter-tied violations are in
// vs — just for logging context, since fixFlaggedChapters silently ignores
// whole-script violations with no chapter of their own (Chapter == 0).
func distinctChapters(vs []story.Violation) int {
	seen := map[int]bool{}
	for _, v := range vs {
		if v.Chapter != 0 {
			seen[v.Chapter] = true
		}
	}
	return len(seen)
}

// fixFlaggedChapters groups violations by chapter and, for each, tries a
// cheap point-fix first (see pointFixChapter) before falling back to a full
// chapter regeneration — shared by runScriptValidation and
// runCrossScriptDedup, the two callers that discover violations tied to
// specific chapters and need them resolved the same way. Violations with no
// chapter of their own (Chapter == 0) are silently skipped — there's
// nothing to regenerate for those.
func (o *Orchestrator) fixFlaggedChapters(ctx context.Context, script *story.Script, violations []story.Violation) error {
	byChapter := map[int][]story.Violation{}
	var order []int
	for _, v := range violations {
		if v.Chapter == 0 {
			continue
		}
		if _, ok := byChapter[v.Chapter]; !ok {
			order = append(order, v.Chapter)
		}
		byChapter[v.Chapter] = append(byChapter[v.Chapter], v)
	}

	for _, idx := range order {
		vs := byChapter[idx]

		fixed := false
		if len(vs) <= maxViolationsForPointFix {
			var err error
			fixed, err = o.pointFixChapter(ctx, script, idx, vs)
			if err != nil {
				return fmt.Errorf("point-fix chapter %d: %w", idx, err)
			}
		}

		if !fixed {
			spec, ok := o.chapterSpec(idx)
			if !ok {
				o.logger.Warn("validation flagged an unknown chapter index, skipping", "chapter", idx)
				continue
			}
			instruction := "Fix these problems without reintroducing them: " + strings.Join(violationMessages(vs), "; ")
			ch, err := o.generateChapter(ctx, script, spec, instruction)
			if err != nil {
				return fmt.Errorf("regenerate chapter %d: %w", idx, err)
			}
			replaceChapter(script, ch)
			sortChapters(script)
		}
		script.RecomputeWordCount()

		// Save after each fix, not just once at the end — a later fix
		// failing on a 429/503 must not lose the fixes that already
		// succeeded before it.
		if err := o.store.SaveScript(ctx, script); err != nil {
			return fmt.Errorf("save progress after fix to chapter %d: %w", idx, err)
		}
		if err := o.checkCostLimit(script); err != nil {
			return err
		}
	}
	return nil
}
