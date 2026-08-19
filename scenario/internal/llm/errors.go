package llm

import "errors"

// ErrRateLimited and ErrServer classify a failed call as retryable. The
// openai-compat client wraps them with fmt.Errorf("%w: ...") so WithRetry
// can match them with errors.Is regardless of which provider produced them.
//
// ErrModelNotFound is different: a 404 means the model name configured for
// this provider doesn't exist (or isn't accessible to this key) — a config
// mistake, not a transient condition. WithRetry already leaves it alone
// (it only retries ErrRateLimited/ErrServer); WithFailover additionally
// stops immediately on it instead of masking the config error by moving on
// to the next provider.
var (
	ErrRateLimited   = errors.New("llm: rate limited")
	ErrServer        = errors.New("llm: server error")
	ErrModelNotFound = errors.New("llm: model not found — check the model name in settings.yaml")
)
