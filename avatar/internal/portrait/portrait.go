// Package portrait holds meta.json's shape and the cost-budget check that
// runs before any image generation call.
package portrait

import (
	"encoding/json"
	"fmt"
)

// Meta is meta.json's shape. File is set for the default single-portrait
// case (portrait.png); Files is set instead when --variants > 1 produced
// several candidate images to pick from by hand.
type Meta struct {
	ID       string   `json:"id"`
	File     string   `json:"file,omitempty"`
	Files    []string `json:"files,omitempty"`
	Prompt   string   `json:"prompt"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	CostUSD  float64  `json:"cost_usd"`
}

// JSON marshals Meta the way `avatar generate` writes meta.json.
func (m Meta) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("portrait: marshal meta: %w", err)
	}
	return b, nil
}

// CheckBudget refuses up front if generating variants images at
// costPerImage each would exceed maxCostUSD — called BEFORE any
// generation call, so an over-budget request is rejected before spending
// anything, not discovered after the fact. costPerImage <= 0 (unknown
// pricing — see config.Pricing.CostFor's ok return) skips the check
// entirely: an unknown cost can't be compared against a limit, and
// refusing to run over a missing price entry would be more surprising
// than useful.
func CheckBudget(costPerImage float64, variants int, maxCostUSD float64) error {
	if costPerImage <= 0 {
		return nil
	}
	if variants <= 0 {
		variants = 1
	}
	estimated := costPerImage * float64(variants)
	if estimated > maxCostUSD {
		return fmt.Errorf("portrait: estimated cost $%.4f (%d image(s) at $%.4f each) exceeds max_cost_usd $%.2f — lower --variants or raise max_cost_usd in settings.yaml",
			estimated, variants, costPerImage, maxCostUSD)
	}
	return nil
}
