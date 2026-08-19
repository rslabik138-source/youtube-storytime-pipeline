// Package thumb holds meta.json's shape and the cost-budget check that
// runs before any text-generation call.
package thumb

import (
	"encoding/json"
	"fmt"

	"github.com/placeholder/thumbnail/internal/textgen"
)

// VariantMeta is one generated variant's text and output file.
type VariantMeta struct {
	File      string         `json:"file"`
	OpenerID  string         `json:"opener_id"`
	Lines     []textgen.Line `json:"lines"`
	FinalLine string         `json:"final_line"`
}

// Meta is meta.json's shape.
type Meta struct {
	ID        string        `json:"id"`
	FaceID    string        `json:"face_id"`
	Variants  []VariantMeta `json:"variants"`
	TextModel string        `json:"text_model"`
	CostUSD   float64       `json:"cost_usd"`
}

// JSON marshals Meta the way `thumb generate` writes meta.json.
func (m Meta) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("thumb: marshal meta: %w", err)
	}
	return b, nil
}

// CheckBudget refuses up front if generating variants texts at
// costPerCall each would exceed maxCostUSD — called BEFORE any
// generation call. costPerCall <= 0 (unknown pricing) skips the check
// entirely, matching avatar's portrait.CheckBudget: an unknown cost can't
// be compared against a limit.
func CheckBudget(costPerCall float64, variants int, maxCostUSD float64) error {
	if costPerCall <= 0 {
		return nil
	}
	if variants <= 0 {
		variants = 1
	}
	estimated := costPerCall * float64(variants)
	if estimated > maxCostUSD {
		return fmt.Errorf("thumb: estimated cost $%.4f (%d variant(s) at $%.4f each) exceeds max_cost_usd $%.2f — lower --variants or raise max_cost_usd in settings.yaml",
			estimated, variants, costPerCall, maxCostUSD)
	}
	return nil
}
