package textgen

import (
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/placeholder/thumbnail/internal/manifest"
)

// Result is one successful text-generation call: the parsed, validated
// text plus what it cost.
type Result struct {
	Text      ThumbnailText
	TokensIn  int
	TokensOut int
	Model     string
}

// maxAttempts bounds retries on a validation failure (bad color, wrong
// line count, over the word limit) — this is a cheap, short JSON call, so
// a generous retry budget costs little.
const maxAttempts = 5

// Generate renders prompts/thumbnail.tmpl against m, calls client, parses
// the JSON response, and validates it against Validate's rules — retrying
// with the violations fed back into the prompt up to maxAttempts times.
func Generate(ctx context.Context, client Client, tmpl *template.Template, m manifest.Manifest, model string, opener Opener) (Result, error) {
	var lastErr error
	var violations []string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}

		prompt, err := Render(tmpl, m, opener, violations)
		if err != nil {
			return Result{}, err
		}

		resp, err := client.Complete(ctx, prompt, model)
		if err != nil {
			return Result{}, fmt.Errorf("textgen: complete: %w", err)
		}

		obj, err := extractJSON(resp.Text)
		if err != nil {
			lastErr = err
			violations = []string{"your previous response wasn't valid JSON: " + err.Error()}
			continue
		}

		var text ThumbnailText
		if err := json.Unmarshal([]byte(obj), &text); err != nil {
			lastErr = fmt.Errorf("textgen: unmarshal response: %w", err)
			violations = []string{"your previous response's JSON didn't match the expected shape: " + err.Error()}
			continue
		}

		if vs := Validate(text); len(vs) > 0 {
			lastErr = fmt.Errorf("textgen: validation failed: %v", vs)
			violations = vs
			continue
		}

		return Result{Text: text, TokensIn: resp.TokensIn, TokensOut: resp.TokensOut, Model: resp.Model}, nil
	}

	return Result{}, fmt.Errorf("textgen: exhausted %d attempts: %w", maxAttempts, lastErr)
}
