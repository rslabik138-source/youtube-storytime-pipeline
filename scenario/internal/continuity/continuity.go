// Package continuity runs the final, separate LLM call that compares a
// finished script's full text against its Bible looking for changed names,
// broken timelines, forgotten details, numeric contradictions, and unclosed
// promises. It returns a validated fix list; deciding what to do with that
// list (regenerate a handful of chapters vs. give up on the whole script) is
// internal/generate's job, not this package's.
package continuity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"text/template"

	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

// Fix is one continuity problem the model found, tied to the chapter that
// needs to change to resolve it.
type Fix struct {
	ChapterIndex int    `json:"chapter_index"`
	Issue        string `json:"issue"`
	Instruction  string `json:"instruction"`
}

// Report is the parsed shape of prompts/continuity.tmpl's response.
type Report struct {
	Fixes []Fix `json:"fixes"`

	// Title is the corrected/verified title, generated AFTER the full text
	// exists and cross-checked against it (a title fixed in the bible,
	// before any chapter was written, can drift from what the finished
	// text actually says — e.g. a duration in the title that no longer
	// matches the story). Empty means the current title is already
	// accurate and needs no change.
	Title string `json:"title"`

	// Usage is the underlying LLM call's cost — not part of the model's
	// JSON response, filled in by Check from the raw llm.Response — so
	// internal/generate can fold continuity's own token spend into the
	// script's per-role usage breakdown (`gen stats`).
	Usage llm.Response `json:"-"`
}

// AffectedChapters returns the distinct chapter indexes named in Fixes,
// sorted ascending, for internal/generate to regenerate.
func (r Report) AffectedChapters() []int {
	seen := map[int]bool{}
	var out []int
	for _, f := range r.Fixes {
		if !seen[f.ChapterIndex] {
			seen[f.ChapterIndex] = true
			out = append(out, f.ChapterIndex)
		}
	}
	sort.Ints(out)
	return out
}

// ErrTooManyFixes means the model reported more problems than the caller is
// willing to patch chapter-by-chapter — internal/generate treats this as a
// signal to fall back to a full-script regeneration instead of chasing fixes
// one at a time. The parsed Report is still returned alongside this error so
// the caller can inspect what was found.
var ErrTooManyFixes = errors.New("continuity: too many fixes to patch individually")

type templateData struct {
	Bible        story.Bible
	FullText     string
	CurrentTitle string
}

// Checker runs the continuity check. tmpl is prompts/continuity.tmpl,
// already parsed — this package doesn't touch the filesystem.
type Checker struct {
	client      llm.Client
	tmpl        *template.Template
	model       string
	maxFixes    int
	fullContext bool
}

// NewChecker builds a Checker. maxFixes <= 0 defaults to 5, per the brief's
// "≤5 fixes → targeted regeneration" rule. fullContext mirrors Settings.
// FullContext: false (the default) sends chapter summaries instead of the
// full script text — continuity's job is factual (names, numbers,
// timeline), and summaries already carry those facts, at a fraction of
// the tokens (and, in practice, a fraction of the thinking-token spend a
// real run showed the full text invites — see Script.SummaryText). true
// sends the real full text, for when precision on subtle prose-level
// wording matters more than cost.
func NewChecker(client llm.Client, tmpl *template.Template, model string, maxFixes int, fullContext bool) *Checker {
	if maxFixes <= 0 {
		maxFixes = 5
	}
	return &Checker{client: client, tmpl: tmpl, model: model, maxFixes: maxFixes, fullContext: fullContext}
}

// maxTechnicalAttempts/initialMaxTokens bound Check's own truncation
// retry — a long fix list (the whole script's worth of continuity
// problems, one JSON object per fix) can legitimately exceed a
// conservative token budget; see the retry loop's comment below.
const (
	maxTechnicalAttempts = 3
	initialMaxTokens     = 3000
)

// Check renders the continuity prompt against bible and script's full text
// and calls the model, retrying with a larger MaxTokens budget (not the
// caller's problem to notice or pay for twice) if the response comes back
// truncated — a real run showed this happen on a script with an unusually
// long fix list, cutting the JSON off mid-object. Every attempt's tokens
// are added into the returned Report.Usage, truncated ones included, so a
// caller accumulating cost from Report.Usage never silently loses spend
// that happened before a successful (or ultimately failed) attempt.
func (c *Checker) Check(ctx context.Context, bible story.Bible, script *story.Script) (Report, error) {
	text := script.SummaryText()
	if c.fullContext {
		text = script.FullText()
	}

	var buf bytes.Buffer
	data := templateData{Bible: bible, FullText: text, CurrentTitle: script.Title}
	if err := c.tmpl.Execute(&buf, data); err != nil {
		return Report{}, fmt.Errorf("continuity: render prompt: %w", err)
	}
	prompt := buf.String()

	maxTokens := initialMaxTokens
	var combined llm.Response
	var raw string
	var lastErr error
	for attempt := 1; attempt <= maxTechnicalAttempts; attempt++ {
		resp, err := c.client.Complete(ctx, prompt, llm.Options{Model: c.model, Role: llm.RoleContinuity, MaxTokens: maxTokens})
		if err != nil {
			return Report{Usage: combined}, fmt.Errorf("continuity: complete: %w", err)
		}
		combined.TokensIn += resp.TokensIn
		combined.TokensOut += resp.TokensOut
		combined.ThinkingTokens += resp.ThinkingTokens
		combined.Provider = resp.Provider
		combined.Model = resp.Model

		raw, lastErr = llm.ExtractJSON(resp.Text)
		if lastErr == nil {
			break
		}
		maxTokens = maxTokens*3/2 + 1
	}
	if lastErr != nil {
		return Report{Usage: combined}, fmt.Errorf("continuity: extract json: %w", lastErr)
	}

	var report Report
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return Report{Usage: combined}, fmt.Errorf("continuity: parse json: %w", err)
	}
	report.Usage = combined

	if len(report.Fixes) > c.maxFixes {
		return report, fmt.Errorf("%w: got %d, max %d", ErrTooManyFixes, len(report.Fixes), c.maxFixes)
	}

	return report, nil
}
