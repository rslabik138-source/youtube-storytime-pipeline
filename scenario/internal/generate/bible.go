package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// bibleResponse is the parsed shape of prompts/bible.tmpl's response. Title
// rides alongside the Bible fields since it belongs to story.Script, not
// story.Bible.
type bibleResponse struct {
	Title         string            `json:"title"`
	Narrator      story.Person      `json:"narrator"`
	Cast          []story.Person    `json:"cast"`
	Timeline      []story.Event     `json:"timeline"`
	FamilyLaw     string            `json:"family_law"`
	RefrainPhrase string            `json:"refrain_phrase"`
	SeededLine    string            `json:"seeded_line"`
	Numbers       map[string]string `json:"numbers"`
}

type bibleTemplateData struct {
	Seed          story.Seed
	UsedNames     []string
	UsedPlaces    []string
	UsedAphorisms []string
	Violations    []string // fed back on retry; empty on the first attempt
}

// generateBible renders bible.tmpl, calls the model, and validates the
// result against every bible-level check (unique names, monotonic
// timeline, non-empty refrain, and — the content-dedup addendum — no name
// reused from a previous accepted script). Validation failures feed back
// into the prompt as Violations and retry, up to Settings.MaxBibleRetries.
func (o *Orchestrator) generateBible(ctx context.Context, script *story.Script) (string, story.Bible, error) {
	usedNames, err := o.store.RecentUsedNames(ctx, 30)
	if err != nil {
		return "", story.Bible{}, fmt.Errorf("recent used names: %w", err)
	}
	usedPlaces, err := o.store.RecentUsedPlaces(ctx, 30)
	if err != nil {
		return "", story.Bible{}, fmt.Errorf("recent used places: %w", err)
	}
	usedAphorisms, err := o.store.RecentUsedAphorisms(ctx, 30)
	if err != nil {
		return "", story.Bible{}, fmt.Errorf("recent used aphorisms: %w", err)
	}

	maxAttempts := o.settings.MaxBibleRetries
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	var violations []string
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", story.Bible{}, err
		}
		if err := o.checkCostLimit(script); err != nil {
			return "", story.Bible{}, err
		}

		var buf bytes.Buffer
		data := bibleTemplateData{
			Seed: script.Seed, UsedNames: usedNames, UsedPlaces: usedPlaces,
			UsedAphorisms: usedAphorisms, Violations: violations,
		}
		if err := o.tmpls.Bible.Execute(&buf, data); err != nil {
			return "", story.Bible{}, fmt.Errorf("render bible prompt: %w", err)
		}

		resp, err := o.client.Complete(ctx, buf.String(), llm.Options{Model: o.settings.GenerateModel, Role: llm.RoleGenerate, MaxTokens: 4000})
		if err != nil {
			return "", story.Bible{}, fmt.Errorf("complete: %w", err)
		}
		o.accumulate(script, resp, llm.RoleGenerate, story.CauseInitial, "bible")

		raw, err := llm.ExtractJSON(resp.Text)
		if err != nil {
			lastErr = fmt.Errorf("extract json: %w", err)
			violations = []string{lastErr.Error()}
			continue
		}
		var br bibleResponse
		if err := json.Unmarshal([]byte(raw), &br); err != nil {
			lastErr = fmt.Errorf("parse json: %w", err)
			violations = []string{lastErr.Error()}
			continue
		}

		bible := story.Bible{
			Narrator: br.Narrator, Cast: br.Cast, Timeline: br.Timeline,
			FamilyLaw: br.FamilyLaw, RefrainPhrase: br.RefrainPhrase,
			SeededLine: br.SeededLine, Numbers: br.Numbers,
		}
		// Sex is a hard seed constraint (Seed.ProtagonistSex), not something
		// to leave to the model's own judgment — always overwrite whatever
		// (if anything) it produced, rather than validating and retrying.
		bible.Narrator.Sex = script.Seed.ProtagonistSex

		errs := errorViolationsOnly(story.ValidateBible(bible, story.BibleValidatorConfig{UsedNames: usedNames}))
		if len(errs) == 0 {
			return br.Title, bible, nil
		}

		msgs := violationMessages(errs)
		o.logger.Warn("bible validation failed, retrying", "attempt", attempt, "violations", msgs)
		violations = msgs
		lastErr = fmt.Errorf("failed validation: %s", strings.Join(msgs, "; "))
	}

	return "", story.Bible{}, fmt.Errorf("exhausted %d attempts: %w", maxAttempts, lastErr)
}
