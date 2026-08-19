package llm

import "context"

type roleModelOverrideClient struct {
	inner  Client
	models map[Role]string
}

// WithRoleModelOverride picks the model for THIS provider based on
// opts.Role, overriding whatever Options.Model the caller asked for. This
// is how a per-provider, per-role model (Provider.Models in settings.yaml)
// wins over the generic generate_model/summary_model/review_model name the
// orchestrator passes in — necessary because those logical names mean
// nothing on a provider whose own model catalog doesn't include them (e.g.
// asking Groq for a Gemini model name 404s), AND because collapsing every
// role to one fixed model (the previous, single-Model version of this
// decorator) silently defeated the generate/summary/review model split —
// every call ended up on whichever one model the provider was pinned to.
// A role with no entry in models — or an empty models map entirely — falls
// through to whatever Options.Model already was. opts.ForceModel, when
// set, wins over this entirely — see its doc comment on Options.
func WithRoleModelOverride(c Client, models map[Role]string) Client {
	if len(models) == 0 {
		return c
	}
	return &roleModelOverrideClient{inner: c, models: models}
}

func (r *roleModelOverrideClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	if opts.ForceModel == "" {
		if model, ok := r.models[opts.Role]; ok && model != "" {
			opts.Model = model
		}
	}
	return r.inner.Complete(ctx, prompt, opts)
}
