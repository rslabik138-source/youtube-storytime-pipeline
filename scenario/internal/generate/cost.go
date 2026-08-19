package generate

import (
	"errors"
	"fmt"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/story"
)

// ErrCostLimitExceeded is returned once a script's running cost (computed
// from its recorded usage against pricing.yaml's rates) crosses
// Settings.MaxCostUSD. checkCostLimit is called at two kinds of points:
// right before every single LLM call (a hard stop — refuses to spend even
// one more call once already over budget, see e.g. bible.go/chapter.go),
// and right after a SaveScript (a resumable checkpoint — the script's
// state up to that point is already durable, so stopping there loses
// nothing already generated). Both call the same function; the difference
// is only which side of the spend they guard.
var ErrCostLimitExceeded = errors.New("generate: script cost limit exceeded")

// callCostUSD computes one call's dollar cost from pricing.yaml's rates.
// ok is false when model has no pricing entry (cost unknown, not free).
func callCostUSD(model string, tokensIn, tokensOut int, pricing config.Pricing) (usd float64, ok bool) {
	mp, found := pricing.For(model)
	if !found {
		return 0, false
	}
	return float64(tokensIn)/1_000_000*mp.InputPerMillion + float64(tokensOut)/1_000_000*mp.OutputPerMillion, true
}

// scriptCostUSD sums usage against pricing, silently skipping any model
// with no pricing entry (cost unknown, not assumed zero) — matching
// config.Pricing.For's own "no entry means unknown" contract.
func scriptCostUSD(usage []story.UsageEntry, pricing config.Pricing) float64 {
	var total float64
	for _, u := range usage {
		cost, ok := callCostUSD(u.Model, u.TokensIn, u.TokensOut, pricing)
		if !ok {
			continue
		}
		total += cost
	}
	return total
}

// checkCostLimit returns ErrCostLimitExceeded if Settings.MaxCostUSD is set
// (>0) and script's already-recorded running cost has crossed it. Safe to
// call at any time, saved or not: it only ever reads script.Usage, which
// RecordUsage keeps current in memory regardless of persistence — calling
// this BEFORE a call (the common case now) means a script already at or
// over budget never spends on one more attempt.
func (o *Orchestrator) checkCostLimit(script *story.Script) error {
	if o.settings.MaxCostUSD <= 0 {
		return nil
	}
	cost := scriptCostUSD(script.Usage, o.pricing)
	if cost > o.settings.MaxCostUSD {
		o.logger.Warn("script cost limit exceeded, stopping",
			"cost_usd", cost, "limit_usd", o.settings.MaxCostUSD)
		return fmt.Errorf("%w: $%.4f > $%.4f", ErrCostLimitExceeded, cost, o.settings.MaxCostUSD)
	}
	return nil
}
