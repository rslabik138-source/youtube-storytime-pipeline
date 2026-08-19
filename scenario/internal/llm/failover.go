package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Named pairs a Client with the friendly name shown in failover logs, plus
// two eligibility constraints checked before a request is ever sent to it:
//
//   - Roles: the roles this provider is allowed to serve. Empty means every
//     role — the common case for a full-capability provider. A provider
//     used only as a backup for cheap calls (e.g. a low-token-quota free
//     tier) should list just those roles, e.g. []Role{RoleSummary,
//     RoleReview}, so failover never routes an oversized generate/continuity
//     request to it in the first place.
//   - MaxTokensPerRequest: the provider's own hard per-request token cap
//     (0 = no cap enforced here). A request whose estimated size exceeds
//     this is skipped rather than sent and left to fail with a 413.
type Named struct {
	Name                string
	Client              Client
	Roles               []Role
	MaxTokensPerRequest int
}

func (n Named) supportsRole(r Role) bool {
	if len(n.Roles) == 0 {
		return true
	}
	for _, role := range n.Roles {
		if role == r {
			return true
		}
	}
	return false
}

// EstimateTokens is a rough, provider-agnostic estimate (~4 characters per
// token, a common rule of thumb for English prose) used only to decide
// whether a request is likely to clear a provider's per-request token cap
// before sending it. It is not exact and must never be used for cost
// accounting — Response.TokensIn/TokensOut from the provider is
// authoritative for that.
func EstimateTokens(s string) int {
	return len(s)/4 + 1
}

type failoverClient struct {
	providers []Named
	logger    *slog.Logger
}

// WithFailover tries providers in order, skipping any that don't declare
// support for opts.Role or whose MaxTokensPerRequest is smaller than this
// request's estimated size. Each remaining entry is expected to already be
// wrapped in its own WithRetry, so by the time Complete here sees an error,
// that provider's own retries are exhausted — failover always moves on
// immediately rather than retrying a second time on top.
func (f *failoverClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	if len(f.providers) == 0 {
		return Response{}, errors.New("llm: no providers configured")
	}

	estimated := EstimateTokens(prompt) + opts.MaxTokens

	var lastErr error
	var tried bool
	for _, p := range f.providers {
		if !p.supportsRole(opts.Role) {
			continue
		}
		if p.MaxTokensPerRequest > 0 && estimated > p.MaxTokensPerRequest {
			if f.logger != nil {
				f.logger.WarnContext(ctx, "llm: skipping provider, request too large for its per-request cap",
					"provider", p.Name, "role", opts.Role, "estimated_tokens", estimated, "max_tokens_per_request", p.MaxTokensPerRequest)
			}
			continue
		}
		tried = true

		resp, err := p.Client.Complete(ctx, prompt, opts)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// A 404 means this provider's configured model name is wrong — a
		// config mistake, not something falling over to the next provider
		// would meaningfully fix. Fail loudly and immediately instead of
		// masking it behind a working backup provider.
		if errors.Is(err, ErrModelNotFound) {
			return Response{}, fmt.Errorf("llm: %s: %w", p.Name, err)
		}

		if f.logger != nil {
			f.logger.WarnContext(ctx, "llm: provider failed, trying next", "provider", p.Name, "error", err)
		}
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
	}
	if !tried {
		return Response{}, fmt.Errorf("llm: no configured provider supports role %q for a request of this size (~%d tokens)", opts.Role, estimated)
	}
	return Response{}, fmt.Errorf("llm: all providers failed, last error: %w", lastErr)
}

// WithFailover tries providers in order. See failoverClient.Complete for
// the eligibility rules (role support, per-request token cap) applied
// before each one is tried.
func WithFailover(logger *slog.Logger, providers ...Named) Client {
	return &failoverClient{providers: providers, logger: logger}
}
