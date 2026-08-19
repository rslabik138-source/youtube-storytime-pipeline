// Package llm is the only thing internal/generate, internal/review, and
// internal/continuity depend on for model access — every test in this
// codebase mocks this interface instead of reaching the network.
//
// Providers are OpenAI-compatible chat completion endpoints (Google AI
// Studio, Groq, Cerebras, OpenRouter, a local Ollama, ...), reached through
// github.com/sashabaranov/go-openai with a per-provider base URL. Adding a
// provider is a configs/settings.yaml edit, never a Go code change.
package llm

import "context"

// Role identifies which stage of the pipeline a call belongs to —
// "generate", "summary", "review", or "continuity". It drives two things a
// call site's Model field alone can't: WithRoleModelOverride picks a
// provider's model for THIS role specifically (so failing over to a
// backup provider doesn't collapse the generate/summary/review model
// split into one model), and WithThinkingBudget picks the Gemini thinking
// budget configured for this role.
type Role string

const (
	RoleGenerate   Role = "generate"
	RoleSummary    Role = "summary"
	RoleReview     Role = "review"
	RoleContinuity Role = "continuity"
)

type Options struct {
	Model       string
	MaxTokens   int
	Temperature float64
	System      string
	Role        Role

	// ReasoningEffort is the standard OpenAI-style thinking-effort control
	// ("none", "low", "medium", "high") — go-openai has a native field for
	// it (ChatCompletionRequest.ReasoningEffort), and Google's Gemini
	// OpenAI-compat endpoint accepts it directly over the wire. This is
	// the CONFIRMED-working way to control Gemini's thinking budget
	// through this endpoint — an earlier attempt at a vendor-specific
	// "google": {"thinking_config": ...} extra_body field was rejected
	// outright by the real API with a 400 "Unknown name \"google\"".
	ReasoningEffort string

	// ExtraBody carries vendor-specific fields merged into the wire
	// request's top-level JSON object (mirroring the official OpenAI
	// client SDKs' "extra_body" convention — note that convention is a
	// client-side merge done by those SDKs before their own HTTP POST, not
	// something the server itself recognizes as a literal "extra_body"
	// key; a raw POST rejects an unrecognized top-level field the same as
	// it would any other typo). go-openai has no first-class field for
	// arbitrary vendor extensions, so any non-empty ExtraBody routes
	// through a raw HTTP request instead of the SDK call. Prefer
	// ReasoningEffort when it covers what you need — it goes through the
	// normal, already-compatible SDK path.
	ExtraBody map[string]any

	// ForceModel, when non-empty, must survive WithRoleModelOverride
	// unchanged — set alongside Model (to the same value) whenever a
	// caller has picked a SPECIFIC model for a specific reason (e.g.
	// chapters.yaml's per-beat model override, keeping a narratively
	// critical beat on a stronger model while the rest of the script uses
	// a cheaper one), as opposed to Model alone, which is just "whatever
	// the caller's default guess was" and IS meant to be overridden by a
	// provider's own per-role model mapping.
	ForceModel string

	// ThinkingBudgetTokens/HasThinkingBudget carry the raw numeric thinking
	// budget WithThinkingBudget computed, for a GeminiNativeClient — the
	// native generateContent endpoint takes a real integer thinkingBudget
	// and (confirmed against the real API) also reports the real spend
	// back via usageMetadata.thoughtsTokenCount, unlike the OpenAI-compat
	// endpoint's reasoning_effort, which Gemini accepts but never reports
	// actual spend for (completion_tokens_details.reasoning_tokens is
	// simply absent from its responses — see checkThinkingDisabled in
	// cmd/gen). HasThinkingBudget distinguishes "role has no configured
	// budget" from "role's configured budget is exactly 0" — an int alone
	// can't.
	ThinkingBudgetTokens int
	HasThinkingBudget    bool
}

type Response struct {
	Text      string
	TokensIn  int
	TokensOut int
	// ThinkingTokens is the reasoning/thinking token count billed at the
	// output rate (OpenAI-compatible APIs report this as
	// usage.completion_tokens_details.reasoning_tokens) — already
	// included in TokensOut, broken out separately so cost reporting can
	// show how much of the bill is actually thinking.
	ThinkingTokens int
	Provider       string // which provider actually served this — stamped by WithFailover
	Model          string
}

type Client interface {
	Complete(ctx context.Context, prompt string, opts Options) (Response, error)
}
