package textgen

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/placeholder/thumbnail/internal/manifest"
)

// Opener carries the opening-line construction thumb picked for this
// thumbnail (see internal/openerpicker) into the prompt: ChosenPattern is
// the SHAPE line 1 must follow, and Recent lists the constructions used on
// the last few thumbnails so the model avoids repeating them.
type Opener struct {
	ChosenPattern string
	Recent        []string
}

// Data is prompts/thumbnail.tmpl's template data.
type Data struct {
	Narrator   manifest.Narrator
	Profession string
	Story      manifest.Story
	Opener     Opener
	// Violations is fed back on a retry after Validate found problems with
	// the previous attempt — empty on the first try.
	Violations []string
}

// humanize turns a snake_case enum token into plain words: "older_sister"
// -> "older sister". A value with no underscores passes through unchanged.
func humanize(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}

// Render executes tmpl (prompts/thumbnail.tmpl, already parsed) against m,
// with the picked opener and any violations (from a prior failed attempt)
// so the model can see exactly what shape to open with and what to fix.
func Render(tmpl *template.Template, m manifest.Manifest, opener Opener, violations []string) (string, error) {
	// The seed/bible enums arrive as snake_case tokens (medication_diverted,
	// older_sister, quiet_acknowledgment). Humanize them so the model can't
	// echo a raw token like "MEDICATION_DIVERTED" straight into the thumbnail.
	story := m.Story
	story.Antagonist = humanize(story.Antagonist)
	story.Betrayal = humanize(story.Betrayal)
	story.EndingType = humanize(story.EndingType)

	data := Data{
		Narrator: m.Narrator, Profession: m.Profession, Story: story,
		Opener:     opener,
		Violations: violations,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("textgen: render: %w", err)
	}
	return buf.String(), nil
}
