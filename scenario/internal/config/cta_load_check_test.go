package config

import (
	"path/filepath"
	"testing"
)

func TestCTALoadsFromChaptersYAML(t *testing.T) {
	ch, err := loadYAML[Chapters](filepath.Join("..", "..", "configs", "chapters.yaml"))
	if err != nil {
		t.Fatalf("load chapters: %v", err)
	}
	if len(ch.CTA.Open) != 6 || len(ch.CTA.Mid1) != 6 || len(ch.CTA.Mid2) != 6 {
		t.Fatalf("expected 6/6/6 cta variants, got open=%d mid1=%d mid2=%d", len(ch.CTA.Open), len(ch.CTA.Mid1), len(ch.CTA.Mid2))
	}
	if len(ch.CloseImages) != 6 {
		t.Fatalf("expected 6 close_images, got %d", len(ch.CloseImages))
	}
	t.Logf("cta open[0]: %.70s...", ch.CTA.Open[0])
}
