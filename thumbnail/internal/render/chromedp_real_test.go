package render

import (
	"context"
	"html/template"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestChromedpRendererProducesRealScreenshot is the only test in this
// package that touches an actual browser — everything else in the render
// package is verified against FakeRenderer, which can't tell us whether
// the real CSS (the 62/38 split, the font auto-fit script, the red final-
// line plate) actually lays out the way the fake assumes. Skipped unless
// THUMB_REAL_CHROME_TEST=1, since it needs a real local Chrome/Chromium
// install and takes real wall-clock time to launch one.
func TestChromedpRendererProducesRealScreenshot(t *testing.T) {
	if os.Getenv("THUMB_REAL_CHROME_TEST") != "1" {
		t.Skip("set THUMB_REAL_CHROME_TEST=1 to run against a real local Chrome install")
	}

	tmplPath := filepath.Join("..", "..", "templates", "thumbnail.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parse %s: %v", tmplPath, err)
	}

	html, err := BuildHTML(tmpl, ViewData{
		Lines: []LineView{
			{Text: "MY BUSINESS PARTNER TOOK $40,000 FROM THE CARE FUND", Color: "yellow"},
			{Text: "YOU ALWAYS WERE TOO CAREFUL WITH THE NUMBERS, CLARA", Color: "red"},
		},
		FinalLine:       "THEN THE AUDITOR CALLED",
		PortraitDataURI: EncodePortrait(stubPNG()),
		BadgeEnabled:    true,
		BadgeText:       "underestimated",
	})
	if err != nil {
		t.Fatalf("BuildHTML: %v", err)
	}

	execPath := os.Getenv("THUMB_CHROME_PATH")
	r := ChromedpRenderer{ExecPath: execPath, Timeout: 20 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	png, err := r.Render(ctx, html)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(png) < 1000 {
		t.Fatalf("expected a real screenshot of non-trivial size, got %d bytes", len(png))
	}

	if out := os.Getenv("THUMB_REAL_CHROME_TEST_OUT"); out != "" {
		if err := os.WriteFile(out, png, 0o644); err != nil {
			t.Fatalf("write %s: %v", out, err)
		}
	}
}
