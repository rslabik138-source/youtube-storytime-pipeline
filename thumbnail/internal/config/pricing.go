package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModelPricing is one text model's flat per-call cost.
type ModelPricing struct {
	CostPerCallUSD float64 `yaml:"cost_per_call_usd"`
}

// Pricing is pricing.yaml's shape — $/call rates used only to compute
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

// CostFor returns model's per-call cost, or ok=false if the model has no
// pricing entry.
func (p Pricing) CostFor(model string) (usd float64, ok bool) {
	m, found := p.Models[model]
	if !found {
		return 0, false
	}
	return m.CostPerCallUSD, true
}
