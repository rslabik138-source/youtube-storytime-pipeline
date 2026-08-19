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
  gemini-3.5-flash-lite:
    cost_per_call_usd: 0.001
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pricing.yaml: %v", err)
	}

	p, err := LoadPricing(path)
	if err != nil {
		t.Fatalf("LoadPricing: %v", err)
	}

	cost, ok := p.CostFor("gemini-3.5-flash-lite")
	if !ok || cost != 0.001 {
		t.Fatalf("expected cost 0.001, got %v ok=%v", cost, ok)
	}
}

func TestCostForUnknownModelReturnsNotOK(t *testing.T) {
	p := Pricing{Models: map[string]ModelPricing{"known-model": {CostPerCallUSD: 0.01}}}
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
