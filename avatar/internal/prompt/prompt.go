// Package prompt builds the portrait image-generation prompt from a
// scenario bundle's manifest, via prompts/avatar.tmpl.
package prompt

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/placeholder/avatar/internal/manifest"
)

// Data is prompts/avatar.tmpl's template data.
type Data struct {
	Sex        string
	Age        int
	Build      string
	Hair       string
	FaceNote   string
	Attire     string
	Profession string
}

// FromManifest builds Data from m, filling in neutral fallback text for
// any appearance field an older bundle (exported before build/hair/
// face_note/attire existed) left blank — so the rendered prompt still
// reads as a complete sentence instead of an empty gap between commas.
func FromManifest(m manifest.Manifest) Data {
	d := Data{
		Sex: m.Narrator.Sex, Age: m.Narrator.Age, Profession: m.Profession,
		Build: m.Narrator.Build, Hair: m.Narrator.Hair,
		FaceNote: m.Narrator.FaceNote, Attire: m.Narrator.Attire,
	}
	if d.Build == "" {
		// Not "average build" — the template's fixed "a {{.Build}}" would
		// read as "a average build" (wrong article before a vowel sound).
		d.Build = "medium build"
	}
	if d.Hair == "" {
		d.Hair = "a neat, flattering hairstyle"
	}
	if d.FaceNote == "" {
		// Not an expression — the template already fixes a warm smile and
		// eye contact; a "plain, ordinary expression" fallback would fight it.
		d.FaceNote = "clear, friendly, approachable features"
	}
	if d.Attire == "" {
		d.Attire = "neat, tidy professional attire"
	}
	return d
}

// Render executes tmpl (prompts/avatar.tmpl, already parsed) against m.
func Render(tmpl *template.Template, m manifest.Manifest) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, FromManifest(m)); err != nil {
		return "", fmt.Errorf("prompt: render: %w", err)
	}
	return buf.String(), nil
}
