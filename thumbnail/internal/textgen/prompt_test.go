package textgen

import (
	"strings"
	"testing"
	"text/template"

	"github.com/placeholder/thumbnail/internal/manifest"
)

func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("thumbnail").Parse(
		`Narrator: {{.Narrator.Name}}, {{.Profession}}. Family law: {{.Story.FamilyLaw}}. Refrain: {{.Story.RefrainPhrase}}. Opener: {{.Opener.ChosenPattern}}.{{if .Opener.Recent}} Avoid: {{range .Opener.Recent}}{{.}}; {{end}}{{end}}{{if .Violations}} Fix: {{range .Violations}}{{.}}; {{end}}{{end}}`)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	return tmpl
}

func testManifest() manifest.Manifest {
	return manifest.Manifest{
		ID: "s1", Profession: "accountant",
		Narrator: manifest.Narrator{Name: "Clara Vance", Age: 43, Sex: "female"},
		Story: manifest.Story{
			FamilyLaw: "the numbers always add up eventually", RefrainPhrase: "I kept the ledger",
		},
	}
}

func testOpener() Opener {
	return Opener{ChosenPattern: "FOR {years} YEARS I {action}."}
}

func TestRenderIncludesStoryFacts(t *testing.T) {
	got, err := Render(testTemplate(t), testManifest(), testOpener(), nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Clara Vance", "accountant", "the numbers always add up eventually", "I kept the ledger"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered prompt to contain %q, got: %s", want, got)
		}
	}
}

func TestRenderIncludesChosenOpener(t *testing.T) {
	got, err := Render(testTemplate(t), testManifest(), testOpener(), nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "FOR {years} YEARS I {action}.") {
		t.Fatalf("expected the chosen opener pattern in the prompt, got: %s", got)
	}
}

func TestRenderIncludesRecentOpenersAvoidList(t *testing.T) {
	op := Opener{ChosenPattern: "HE CALLED ME {insult}. I SAID NOTHING.", Recent: []string{"FOR {years} YEARS I {action}."}}
	got, err := Render(testTemplate(t), testManifest(), op, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "Avoid:") || !strings.Contains(got, "FOR {years} YEARS I {action}.") {
		t.Fatalf("expected recent openers as an avoid-list, got: %s", got)
	}
}

func TestRenderIncludesViolationsOnRetry(t *testing.T) {
	got, err := Render(testTemplate(t), testManifest(), testOpener(), []string{"too many lines"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "too many lines") {
		t.Fatalf("expected violations fed back into the prompt, got: %s", got)
	}
}

func TestRenderOmitsViolationsBlockOnFirstAttempt(t *testing.T) {
	got, err := Render(testTemplate(t), testManifest(), testOpener(), nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "Fix:") {
		t.Fatalf("expected no violations block on the first attempt, got: %s", got)
	}
}
