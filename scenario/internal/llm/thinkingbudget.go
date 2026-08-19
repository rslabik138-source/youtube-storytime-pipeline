package llm

import "context"

type thinkingBudgetClient struct {
	inner  Client
	budget map[Role]int
}

// WithThinkingBudget sets Options.ReasoningEffort for whatever role
// opts.Role names, mapping a numeric budget to the standard "none" /
// "low" / "medium" / "high" levels. This is the CONFIRMED-working
// mechanism for Gemini's OpenAI-compatible endpoint — go-openai forwards
// ReasoningEffort natively as the wire-standard "reasoning_effort" field.
// (An earlier version of this decorator sent Gemini's native
// "google": {"thinking_config": {"thinking_budget": N}} via extra_body;
// the real API rejected it outright with 400 "Unknown name \"google\"" —
// that shape is a client-side convenience some official SDKs merge in
// before their own HTTP call, not something the server itself parses from
// a raw POST body.)
//
// A role with no entry in budget, or an empty budget map entirely, is a
// no-op passthrough — this decorator should only wrap providers that
// actually understand the field (Provider.SupportsThinkingConfig in
// settings.yaml).
func WithThinkingBudget(c Client, budget map[Role]int) Client {
	if len(budget) == 0 {
		return c
	}
	return &thinkingBudgetClient{inner: c, budget: budget}
}

// reasoningEffortFor buckets a numeric thinking-token budget into the
// nearest standard level. The buckets mirror OpenAI's own documented
// mapping (low/medium/high ~ 1K/8K/24K token budgets).
//
// budget<=0 maps to "low", not "none": a real run against
// gemini-3.6-flash rejected "none" outright with a 400 ("Request contains
// an invalid argument") — this model family apparently doesn't accept
// fully-disabled reasoning as a valid value, only a minimum level.
func reasoningEffortFor(budget int) string {
	switch {
	case budget <= 2000:
		return "low"
	case budget <= 12000:
		return "medium"
	default:
		return "high"
	}
}

func (t *thinkingBudgetClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	if budget, ok := t.budget[opts.Role]; ok {
		opts.ReasoningEffort = reasoningEffortFor(budget)
		opts.ThinkingBudgetTokens = budget
		opts.HasThinkingBudget = true
	}
	return t.inner.Complete(ctx, prompt, opts)
}
