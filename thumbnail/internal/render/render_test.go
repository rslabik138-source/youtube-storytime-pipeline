package render

import (
	"html/template"
	"strings"
	"testing"
)

func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("thumbnail").Parse(
		`<html><img src="{{.PortraitDataURI}}">{{range .Lines}}<span class="color-{{.Color}}">{{.Text}}</span>{{end}}<b>{{.FinalLine}}</b>{{if .BadgeEnabled}}<div>{{.BadgeText}}</div>{{end}}</html>`)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	return tmpl
}

func TestBuildHTMLIncludesEveryField(t *testing.T) {
	data := ViewData{
		Lines: []LineView{
			{Text: "SHE TOOK EVERYTHING", Color: "yellow"},
			{Text: "AND NEVER LOOKED BACK", Color: "red"},
		},
		FinalLine:       "THEN THE LETTER ARRIVED",
		PortraitDataURI: "data:image/png;base64,AAAA",
		BadgeEnabled:    true,
		BadgeText:       "UNDERESTIMATED",
	}
	got, err := BuildHTML(testTemplate(t), data)
	if err != nil {
		t.Fatalf("BuildHTML: %v", err)
	}
	for _, want := range []string{
		"data:image/png;base64,AAAA", "SHE TOOK EVERYTHING", "color-yellow",
		"AND NEVER LOOKED BACK", "color-red", "THEN THE LETTER ARRIVED", "UNDERESTIMATED",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered HTML to contain %q, got: %s", want, got)
		}
	}
}

func TestBuildHTMLOmitsBadgeWhenDisabled(t *testing.T) {
	data := ViewData{FinalLine: "x", BadgeEnabled: false, BadgeText: "SHOULD NOT APPEAR"}
	got, err := BuildHTML(testTemplate(t), data)
	if err != nil {
		t.Fatalf("BuildHTML: %v", err)
	}
	if strings.Contains(got, "SHOULD NOT APPEAR") {
		t.Fatalf("expected badge text omitted when disabled, got: %s", got)
	}
}

// TestBuildHTMLEscapesLineText guards against thumbnail text (ultimately
// LLM-generated) being interpreted as markup — html/template's contextual
// escaping should turn a stray "<" into "&lt;" rather than opening a tag.
func TestBuildHTMLEscapesLineText(t *testing.T) {
	data := ViewData{
		Lines:     []LineView{{Text: `<script>alert(1)</script>`, Color: "white"}},
		FinalLine: "x",
	}
	got, err := BuildHTML(testTemplate(t), data)
	if err != nil {
		t.Fatalf("BuildHTML: %v", err)
	}
	if strings.Contains(got, "<script>alert(1)</script>") {
		t.Fatalf("expected line text to be escaped, not injected as markup, got: %s", got)
	}
}

func TestEncodePortraitProducesDataURI(t *testing.T) {
	got := EncodePortrait([]byte{0x89, 0x50, 0x4E, 0x47})
	if !strings.HasPrefix(string(got), "data:image/png;base64,") {
		t.Fatalf("expected a data:image/png;base64, prefix, got: %s", got)
	}
}
