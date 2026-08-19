package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModelPricing is one model's flat per-image cost.
type ModelPricing struct {
	CostPerImageUSD float64 `yaml:"cost_per_image_usd"`
}

// Pricing is pricing.yaml's shape — $/image rates used only to compute
// meta.json's cost_usd and to enforce max_cost_usd, never sent to any API.
// A model with no entry here means unknown cost, not free — callers should
// show "?" rather than a silently misleading $0.
type Pricing struct {
	Models map[string]ModelPricing `yaml:"models"`
}

// LoadPricing reads and parses pricing.yaml at path.
func LoadPricing(path string) (Pricing, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Pricing{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var p Pricing
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Pricing{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return p, nil
}

// CostFor returns model's per-image cost, or ok=false if the model has no
// pricing entry.
func (p Pricing) CostFor(model string) (usd float64, ok bool) {
	m, found := p.Models[model]
	if !found {
		return 0, false
	}
	return m.CostPerImageUSD, true
}
