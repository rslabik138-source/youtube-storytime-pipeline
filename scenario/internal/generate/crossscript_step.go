package generate

import (
	"context"
	"errors"
	"fmt"

	"github.com/placeholder/scenario/internal/story"
)

// ErrCrossScriptDedupExhausted means runCrossScriptDedup never fully
// converged within Settings.MaxRepetitionRounds — some six-word phrases
// still overlap a prior accepted script. finishPipeline treats this the
// same way it treats ErrValidationRoundsExhausted: keep the script
// (accepted_with_warnings) rather than discard money already spent, instead
// of failing the whole run.
var ErrCrossScriptDedupExhausted = errors.New("generate: cross-script dedup: rounds exhausted with unresolved overlap")

// crossScriptPhraseOverlapThreshold is how many six-word phrases shared
// with any single prior accepted script counts as a genuine cross-script
// repetition problem rather than coincidental overlap — a real comparison
// of two back-to-back generated scripts found 17 shared phrases, including
// a verbatim repeated sentence ("this is a quiet reckoning, not a shouting
// match across a crowded room"), so overlap at that scale is a real
// pattern the model falls into, not noise.
const crossScriptPhraseOverlapThreshold = 5

// crossScriptLookbackScripts bounds how many of the most recent accepted
// scripts count toward both the "avoid these phrasings" prompt guidance
// and this validator — an old script eventually cycles out, the same way
// profession_cooldown and the other content-dedup checks already scope
// themselves to a recent window rather than the store's entire history.
const crossScriptLookbackScripts = 10

// crossScriptAvoidPhraseLimit caps how many of the top-repeated phrases from
// TopUsedPhrases go into the up-front prompt warning (CrossScriptAvoidPhrases
// below). Raised 40->200 on 2026-08-19: at ~8 tokens/phrase * ~30 generate
// calls/script, even 200 phrases only costs ~$0.03/script — negligible next
// to a single repair call — while the old 40-phrase cap meant most of the
// store's ~134k accumulated phrases (19 scripts and growing) had no chance
// of being pre-warned at all, so cross_script_phrase_reuse stayed the
// single biggest repair driver even after the sentence-opening fix. Dumping
// the ENTIRE phrase store instead was priced out: ~$20/script just for that
// one prompt block, 20x the whole run's current cost — see the session note
// this constant's history came out of before changing it again.
const crossScriptAvoidPhraseLimit = 200

// runCrossScriptDedup checks the finished script's six-word phrases
// against the last crossScriptLookbackScripts accepted scripts —
// story.ValidateScript only ever sees one script at a time, so this
// cross-script comparison has to live here, backed by the store, instead.
// More than crossScriptPhraseOverlapThreshold phrases shared with any
// single prior script forces regeneration of the chapters carrying those
// phrases, via the same point-fix-or-regenerate path runScriptValidation
// itself uses (fixFlaggedChapters). Runs after runScriptValidation (so
// within-script repetition is already cleaned up) and before review/
// acceptance, since RecordAcceptance is what books this script's own
// phrases for the next one to be checked against.
func (o *Orchestrator) runCrossScriptDedup(ctx context.Context, script *story.Script) error {
	maxRounds := o.settings.MaxRepetitionRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}

	for round := 1; round <= maxRounds; round++ {
		occurrences := story.SixGramOccurrences(script)
		if len(occurrences) == 0 {
			return nil
		}
		phrases := make([]string, 0, len(occurrences))
		for gram := range occurrences {
			phrases = append(phrases, gram)
		}

		overlaps, err := o.store.PhraseOverlaps(ctx, phrases, crossScriptLookbackScripts)
		if err != nil {
			return fmt.Errorf("cross-script phrase overlap: %w", err)
		}
		if len(overlaps) == 0 {
			return nil
		}

		countByScript := map[string]int{}
		for _, scriptIDs := range overlaps {
			for _, sid := range scriptIDs {
				countByScript[sid]++
			}
		}
		offendingScripts := map[string]bool{}
		for sid, n := range countByScript {
			if n > crossScriptPhraseOverlapThreshold {
				offendingScripts[sid] = true
			}
		}
		if len(offendingScripts) == 0 {
			return nil
		}

		var violations []story.Violation
		for phrase, scriptIDs := range overlaps {
			shared := false
			for _, sid := range scriptIDs {
				if offendingScripts[sid] {
					shared = true
					break
				}
			}
			if !shared {
				continue
			}
			chapterIdx := occurrences[phrase][0]
			violations = append(violations, story.Violation{
				Rule: "cross_script_phrase_reuse", Severity: story.SeverityError, Chapter: chapterIdx, Phrase: phrase,
				Message: fmt.Sprintf(
					"six-word phrase %q in chapter %d was already used in a previous accepted script — rewrite it",
					phrase, chapterIdx),
			})
		}
		if len(violations) == 0 {
			return nil
		}

		o.logger.Warn("cross-script phrase overlap found, regenerating flagged chapters",
			"round", round, "prior_scripts", len(offendingScripts), "phrases", len(violations))
		if err := o.fixFlaggedChapters(ctx, script, violations); err != nil {
			return fmt.Errorf("cross-script phrase overlap: %w", err)
		}
	}

	return fmt.Errorf("%w: %d rounds", ErrCrossScriptDedupExhausted, maxRounds)
}
