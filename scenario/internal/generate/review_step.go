package generate

import (
	"context"
	"fmt"

	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// runReview scores the script, saves every attempt (accepted or not —
// rejected scripts are kept "for analysis" per the brief), and on a
// below-threshold result regenerates the chapters the model flagged as
// weak before trying again. After Settings.MaxReviewRetries regeneration
// rounds it gives up and leaves the script rejected rather than looping
// forever.
func (o *Orchestrator) runReview(ctx context.Context, script *story.Script) error {
	maxAttempts := o.settings.MaxReviewRetries
	if maxAttempts <= 0 {
		maxAttempts = 2
	}

	for attempt := 0; ; attempt++ {
		if err := o.checkCostLimit(script); err != nil {
			return err
		}
		result, err := o.review.Review(ctx, script)
		if result.Usage != (llm.Response{}) {
			cause := story.CauseInitial
			if attempt > 0 {
				cause = story.CauseRepair // re-scoring after a weak-chapter fix
			}
			o.accumulate(script, result.Usage, llm.RoleReview, cause, "review")
		}
		if err != nil {
			return fmt.Errorf("review: %w", err)
		}
		if err := o.store.SaveQualityScores(ctx, script.ID, result.Scores); err != nil {
			return fmt.Errorf("save quality scores: %w", err)
		}
		script.Quality = result.Scores

		if result.Passes(o.settings.QualityThreshold) {
			script.Status = story.StatusAccepted
			return nil
		}
		if attempt >= maxAttempts {
			script.Status = story.StatusRejected
			return nil
		}

		o.logger.Warn("review below threshold, regenerating weak chapters",
			"attempt", attempt+1, "mean", result.Scores.Mean(), "min", result.Scores.Min())
		for _, wc := range result.WeakChapters {
			spec, ok := o.chapterSpec(wc.Index)
			if !ok {
				o.logger.Warn("review flagged an unknown chapter index, skipping", "chapter", wc.Index)
				continue
			}
			ch, err := o.generateChapter(ctx, script, spec, fmt.Sprintf("%s: %s", wc.Axis, wc.Reason))
			if err != nil {
				return fmt.Errorf("regenerate weak chapter %d: %w", wc.Index, err)
			}
			replaceChapter(script, ch)
			script.RecomputeWordCount()

			// Save after each fix, not just once at the end of the attempt
			// — a later weak-chapter fix failing on a 429/503 must not
			// lose the fixes that already succeeded before it.
			if err := o.store.SaveScript(ctx, script); err != nil {
				return fmt.Errorf("save progress after weak-chapter fix to chapter %d: %w", wc.Index, err)
			}
			if err := o.checkCostLimit(script); err != nil {
				return err
			}
		}
	}
}
