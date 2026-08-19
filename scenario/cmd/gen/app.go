package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/template"

	"golang.org/x/time/rate"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/continuity"
	"github.com/placeholder/scenario/internal/generate"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/review"
	"github.com/placeholder/scenario/internal/seed"
	"github.com/placeholder/scenario/internal/store"
)

// toRoleMap converts a settings.yaml role-keyed map (generate, summary,
// review, continuity) to the llm.Role-keyed map llm.WithRoleModelOverride
// and llm.WithThinkingBudget expect. An empty/nil input returns nil, which
// both decorators treat as "no override."
func toRoleMap[V any](m map[string]V) map[llm.Role]V {
	if len(m) == 0 {
		return nil
	}
	out := make(map[llm.Role]V, len(m))
	for k, v := range m {
		out[llm.Role(k)] = v
	}
	return out
}

// toRoles converts settings.yaml's roles list ([]string) to []llm.Role.
func toRoles(roles []string) []llm.Role {
	if len(roles) == 0 {
		return nil
	}
	out := make([]llm.Role, len(roles))
	for i, r := range roles {
		out[i] = llm.Role(r)
	}
	return out
}

// buildClient wires every configured provider through WithRetry, then all
// of them through WithFailover in settings.yaml order, then an optional
// global WithRateLimit. onlyProvider restricts to a single named provider
// (the CLI's --provider flag) instead of the full failover chain.
func buildClient(cfg *config.Config, logger *slog.Logger, onlyProvider string) (llm.Client, error) {
	var named []llm.Named
	for _, p := range cfg.Settings.Providers {
		if onlyProvider != "" && p.Name != onlyProvider {
			continue
		}

		apiKey := ""
		if p.APIKeyEnv != "" {
			apiKey = os.Getenv(p.APIKeyEnv)
			if apiKey == "" {
				if onlyProvider != "" {
					// Explicitly requested — a missing key here is an error,
					// not something to silently skip.
					return nil, fmt.Errorf("provider %q needs environment variable %s to be set", p.Name, p.APIKeyEnv)
				}
				logger.Warn("skipping provider, its API key env var isn't set", "provider", p.Name, "env", p.APIKeyEnv)
				continue
			}
		}

		var c llm.Client
		if p.NativeThinkingAPI {
			c = llm.NewGeminiNativeClient(p.Name, p.BaseURL, apiKey)
		} else {
			c = llm.NewOpenAICompatClient(p.Name, p.BaseURL, apiKey)
		}
		c = llm.WithRoleModelOverride(c, toRoleMap(p.Models)) // this provider's own model per role wins over generate/summary/review_model
		if p.SupportsThinkingConfig {
			c = llm.WithThinkingBudget(c, toRoleMap(cfg.Settings.ThinkingBudget))
		}
		// RetryConfig{} defaults to llm.DefaultRetryDelays (2s,5s,15s,45s,120s
		// with jitter) — enough patience for a real 429/503 to clear before
		// WithFailover gives up on this provider.
		named = append(named, llm.Named{
			Name: p.Name, Client: llm.WithRetry(c, llm.RetryConfig{}),
			Roles: toRoles(p.Roles), MaxTokensPerRequest: p.MaxTokensPerRequest,
		})
	}
	if len(named) == 0 {
		if onlyProvider != "" {
			return nil, fmt.Errorf("no provider named %q in settings.yaml", onlyProvider)
		}
		return nil, fmt.Errorf("no usable provider: set at least one provider's API key env var from settings.yaml (e.g. GOOGLE_AI_STUDIO_API_KEY or GROQ_API_KEY)")
	}

	var client llm.Client = llm.WithFailover(logger, named...)
	if cfg.Settings.RateLimitRPS > 0 {
		client = llm.WithRateLimit(client, rate.NewLimiter(rate.Limit(cfg.Settings.RateLimitRPS), 1))
	}
	return client, nil
}

type templates struct {
	bible      *template.Template
	chapter    *template.Template
	summary    *template.Template
	continuity *template.Template
	review     *template.Template
	pointFix   *template.Template
}

func loadTemplates(dir string) (templates, error) {
	parse := func(name string) (*template.Template, error) {
		tmpl, err := template.ParseFiles(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		return tmpl, nil
	}

	var t templates
	var err error
	if t.bible, err = parse("bible.tmpl"); err != nil {
		return templates{}, err
	}
	if t.chapter, err = parse("chapter.tmpl"); err != nil {
		return templates{}, err
	}
	if t.summary, err = parse("summary.tmpl"); err != nil {
		return templates{}, err
	}
	if t.continuity, err = parse("continuity.tmpl"); err != nil {
		return templates{}, err
	}
	if t.review, err = parse("review.tmpl"); err != nil {
		return templates{}, err
	}
	if t.pointFix, err = parse("pointfix.tmpl"); err != nil {
		return templates{}, err
	}
	return t, nil
}

// buildOrchestrator wires the full pipeline for `gen generate` and `gen
// regenerate`. The caller owns the returned Store and must Close it.
func buildOrchestrator(cfg *config.Config, dbPath, promptsDir, providerOverride string, logger *slog.Logger) (*generate.Orchestrator, store.Store, error) {
	st, err := openStoreFromConfig(cfg, dbPath)
	if err != nil {
		return nil, nil, err
	}

	client, err := buildClient(cfg, logger, providerOverride)
	if err != nil {
		st.Close()
		return nil, nil, err
	}

	tmpls, err := loadTemplates(promptsDir)
	if err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("load templates: %w", err)
	}

	seedGen := seed.NewGenerator(cfg.Axes, cfg.Professions, cfg.Names, st,
		seed.Constraints{ProfessionCooldown: cfg.Settings.ProfessionCooldown})
	checker := continuity.NewChecker(client, tmpls.continuity, cfg.Settings.GenerateModel, cfg.Settings.MaxContinuityFixes, cfg.Settings.FullContext)
	reviewer := review.NewReviewer(client, tmpls.review, cfg.Settings.ReviewModel)

	orch := generate.NewOrchestrator(
		seedGen, client,
		generate.Templates{Bible: tmpls.bible, Chapter: tmpls.chapter, Summary: tmpls.summary, PointFix: tmpls.pointFix},
		checker, reviewer, st, cfg.Chapters, cfg.BannedPhrases.Phrases, cfg.Settings, cfg.Pricing, logger,
	)
	return orch, st, nil
}
