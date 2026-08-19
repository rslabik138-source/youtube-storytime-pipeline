package generate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/placeholder/scenario/internal/continuity"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// runContinuity runs the one continuity check call and, for every chapter
// it flags (capped at the Checker's own maxFixes — continuity.ErrTooManyFixes
// surfaces as a hard error here rather than being silently absorbed),
// regenerates just that chapter with the fix instruction folded in. It also
// applies the corrected title continuity.tmpl produces — the title is
// verified (and, if necessary, rewritten) only now, after the full text
// exists, specifically so it can't drift from what the finished chapters
// actually say (a duration or number in the title contradicting the text).
func (o *Orchestrator) runContinuity(ctx context.Context, script *story.Script) error {
	if err := o.checkCostLimit(script); err != nil {
		return err
	}
	report, err := o.continuity.Check(ctx, script.Bible, script)
	if report.Usage != (llm.Response{}) {
		// The one continuity check per script — always the initial,
		// whole-script pass, never itself a repair (the chapter
		// regenerations it triggers below are repairs; the check that
		// found them isn't).
		o.accumulate(script, report.Usage, llm.RoleContinuity, story.CauseInitial, "continuity")
	}
	if err != nil {
		if errors.Is(err, continuity.ErrTooManyFixes) {
			return fmt.Errorf("too many continuity problems (%d) to patch chapter-by-chapter: %w", len(report.Fixes), err)
		}
		return fmt.Errorf("check: %w", err)
	}

	if report.Title != "" && report.Title != script.Title {
		o.logger.Warn("continuity corrected the title", "old", script.Title, "new", report.Title)
		script.Title = report.Title
	}

	for _, idx := range report.AffectedChapters() {
		spec, ok := o.chapterSpec(idx)
		if !ok {
			o.logger.Warn("continuity fix referenced an unknown chapter index, skipping", "chapter", idx)
			continue
		}

		ch, err := o.generateChapter(ctx, script, spec, continuityInstruction(report, idx))
		if err != nil {
			return fmt.Errorf("regenerate chapter %d for continuity: %w", idx, err)
		}
		replaceChapter(script, ch)
		script.RecomputeWordCount()

		// Save after each fix, not just once at the end of the batch — a
		// later fix in the same batch failing on a 429/503 (after
		// WithRetry/WithFailover both give up) must not lose the fixes
		// that already succeeded before it.
		if err := o.store.SaveScript(ctx, script); err != nil {
			return fmt.Errorf("save progress after continuity fix to chapter %d: %w", idx, err)
		}
		if err := o.checkCostLimit(script); err != nil {
			return err
		}
	}
	return nil
}

func continuityInstruction(report continuity.Report, chapterIndex int) string {
	var parts []string
	for _, f := range report.Fixes {
		if f.ChapterIndex == chapterIndex {
			parts = append(parts, fmt.Sprintf("%s: %s", f.Issue, f.Instruction))
		}
	}
	return strings.Join(parts, " ")
}
