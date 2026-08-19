package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPricingParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.yaml")
	content := `
models:
  gemini-2.5-flash-image:
    cost_per_image_usd: 0.039
  imagen-4.0-fast-generate-001:
    cost_per_image_usd: 0.02
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pricing.yaml: %v", err)
	}

	p, err := LoadPricing(path)
	if err != nil {
		t.Fatalf("LoadPricing: %v", err)
	}

	cost, ok := p.CostFor("gemini-2.5-flash-image")
	if !ok || cost != 0.039 {
		t.Fatalf("expected cost 0.039 for gemini model, got %v ok=%v", cost, ok)
	}
	cost, ok = p.CostFor("imagen-4.0-fast-generate-001")
	if !ok || cost != 0.02 {
		t.Fatalf("expected cost 0.02 for imagen model, got %v ok=%v", cost, ok)
	}
}

func TestCostForUnknownModelReturnsNotOK(t *testing.T) {
	p := Pricing{Models: map[string]ModelPricing{"known-model": {CostPerImageUSD: 0.01}}}
	cost, ok := p.CostFor("unknown-model")
	if ok {
		t.Fatalf("expected ok=false for an unknown model, got cost=%v", cost)
	}
}

func TestLoadPricingMissingFileReturnsError(t *testing.T) {
	if _, err := LoadPricing(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
