// Package review runs the LLM quality-scoring call: 7 axes 0-10
// (hook_strength, profession_causality, restraint, scene_not_summary,
// planting_payoff, refusal_present, ai_smell), each scored against the
// finished script. Deciding what happens on failure — regenerate weak
// chapters, then accept, then eventually mark rejected — is
// internal/generate's orchestration; this package only produces and
// validates the scored result.
package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// WeakChapter is a chapter the model flagged as dragging a specific axis
// down, so internal/generate knows what to regenerate instead of redoing
// the whole script.
type WeakChapter struct {
	Index  int    `json:"index"`
	Axis   string `json:"axis"`
	Reason string `json:"reason"`
}

// Result is the parsed shape of prompts/review.tmpl's response.
type Result struct {
	Scores       story.QualityScores `json:"scores"`
	WeakChapters []WeakChapter       `json:"weak_chapters"`

	// Usage is the underlying LLM call's cost — not part of the model's
	// JSON response, filled in by Review from the raw llm.Response — so
	// internal/generate can fold review's own token spend into the
	// script's per-role usage breakdown (`gen stats`).
	Usage llm.Response `json:"-"`
}

// Passes reports whether Scores clears threshold: mean >= MeanMin and no
// single axis below AxisMin.
func (r Result) Passes(threshold config.QualityThreshold) bool {
	return r.Scores.Mean() >= threshold.MeanMin && r.Scores.Min() >= threshold.AxisMin
}

// Validate checks every axis is within the [0,10] scale the prompt asks
// for — a model returning 15 or -1 is a malformed response, not a low score.
func (r Result) Validate() error {
	axes := []struct {
		name  string
		value float64
	}{
		{"hook_strength", r.Scores.HookStrength},
		{"profession_causality", r.Scores.ProfessionCausality},
		{"restraint", r.Scores.Restraint},
		{"scene_not_summary", r.Scores.SceneNotSummary},
		{"planting_payoff", r.Scores.PlantingPayoff},
		{"refusal_present", r.Scores.RefusalPresent},
		{"ai_smell", r.Scores.AISmell},
	}
	for _, a := range axes {
		if a.value < 0 || a.value > 10 {
			return fmt.Errorf("review: axis %q score %.1f is out of range [0,10]", a.name, a.value)
		}
	}
	return nil
}

type templateData struct {
	Seed     story.Seed
	Bible    story.Bible
	FullText string
}

// Reviewer runs the review call. tmpl is prompts/review.tmpl, already
// parsed — this package doesn't touch the filesystem.
type Reviewer struct {
	client llm.Client
	tmpl   *template.Template
	model  string
}

func NewReviewer(client llm.Client, tmpl *template.Template, model string) *Reviewer {
	return &Reviewer{client: client, tmpl: tmpl, model: model}
}

// Review renders the review prompt against script, calls the model once,
// and returns the parsed, range-validated result.
func (rv *Reviewer) Review(ctx context.Context, script *story.Script) (Result, error) {
	var buf bytes.Buffer
	data := templateData{Seed: script.Seed, Bible: script.Bible, FullText: script.FullText()}
	if err := rv.tmpl.Execute(&buf, data); err != nil {
		return Result{}, fmt.Errorf("review: render prompt: %w", err)
	}

	resp, err := rv.client.Complete(ctx, buf.String(), llm.Options{Model: rv.model, Role: llm.RoleReview, MaxTokens: 2000})
	if err != nil {
		return Result{}, fmt.Errorf("review: complete: %w", err)
	}

	raw, err := llm.ExtractJSON(resp.Text)
	if err != nil {
		return Result{}, fmt.Errorf("review: extract json: %w", err)
	}

	var result Result
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return Result{}, fmt.Errorf("review: parse json: %w", err)
	}
	result.Usage = resp

	if err := result.Validate(); err != nil {
		return Result{}, err
	}

	return result, nil
}
