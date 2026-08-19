// Package config loads and validates the six YAML files that drive seed
// generation, prompt content, and pipeline behavior: axes.yaml,
// professions.yaml, chapters.yaml, names.yaml, banned_phrases.yaml,
// settings.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type WeightedValue struct {
	Value  string  `yaml:"value"`
	Weight float64 `yaml:"weight"` // 0 is treated as 1.0 by callers
}

// DurationCategory splits the duration axis into short/long buckets so
// internal/seed can force alternation between them — a plain weighted list
// can't express that split.
type DurationCategory struct {
	Short []int `yaml:"short"`
	Long  []int `yaml:"long"`
}

// ReckoningPlaceCategory splits reckoning_place into private/public buckets
// for the same forced-alternation reason.
type ReckoningPlaceCategory struct {
	Private []string `yaml:"private"`
	Public  []string `yaml:"public"`
}

type Axes struct {
	Antagonist       []WeightedValue        `yaml:"antagonist"`
	WeakAlly         []WeightedValue        `yaml:"weak_ally"`
	HumiliationType  []WeightedValue        `yaml:"humiliation_type"`
	Betrayal         []WeightedValue        `yaml:"betrayal"`
	Duration         DurationCategory       `yaml:"duration"`
	WrittenOverreach []WeightedValue        `yaml:"written_overreach"`
	ObjectContainer  []WeightedValue        `yaml:"object_container"`
	LegacyArtifact   []WeightedValue        `yaml:"legacy_artifact"`
	ReckoningPlace   ReckoningPlaceCategory `yaml:"reckoning_place"`
	EndingType       []WeightedValue        `yaml:"ending_type"`
	ProtagonistSex   []WeightedValue        `yaml:"protagonist_sex"`
}

type Profession struct {
	Name         string   `yaml:"name"`
	Epistemology string   `yaml:"epistemology"`
	RecordType   string   `yaml:"record_type"`
	Exposes      []string `yaml:"exposes"` // betrayal values this record type can expose
	// Attire is the narrator's work clothing for this profession — worn
	// and specific to the job, not a general appearance description. Feeds
	// the avatar module's portrait prompt directly (see
	// internal/story.Seed.Attire and internal/export.BundleNarrator).
	Attire string `yaml:"attire"`
}

type Professions struct {
	Professions []Profession `yaml:"professions"`
}

// Get looks up a profession by name.
func (p Professions) Get(name string) (Profession, bool) {
	for _, pr := range p.Professions {
		if pr.Name == name {
			return pr, true
		}
	}
	return Profession{}, false
}

// ExposesFor is the hard pairing constraint the whole genre rests on: a
// betrayal is only ever drawn from the list its profession's record type
// can actually expose.
func (p Professions) ExposesFor(name string) ([]string, bool) {
	pr, ok := p.Get(name)
	if !ok {
		return nil, false
	}
	return pr.Exposes, true
}

func (p Professions) Names() []string {
	out := make([]string, len(p.Professions))
	for i, pr := range p.Professions {
		out[i] = pr.Name
	}
	return out
}

type ChapterSpec struct {
	Index       int    `yaml:"index"`
	Beat        string `yaml:"beat"`
	TargetWords int    `yaml:"target_words"`
	Description string `yaml:"description"`

	// Split, when true, generates this chapter as two sequential LLM
	// calls (first half, then a continuation given the first half as
	// context) instead of one. Each call requests roughly half the
	// tokens, which lowers the odds of hitting a provider's 503
	// "high demand" rejection on the largest beats, and makes a retry
	// after such a failure cheaper: only the failed half regenerates.
	Split bool `yaml:"split,omitempty"`

	// Model, when set, forces this specific beat's generation (and any
	// point-fixes on it) onto this model instead of settings.yaml's
	// generate role default — for a hybrid setup where most beats use a
	// cheap model but a few narratively critical ones (the hook, the
	// refrain-bearing beats, the reckoning) stay on a stronger, pricier
	// one. Wins over the provider's per-role model mapping (llm.Options.
	// ForceModel), not just the logical generate_model fallback.
	Model string `yaml:"model,omitempty"`
}

// CloseImageSpec is one interchangeable "how the final beat lands"
// archetype. The close beat (index 16) rotates through these across
// scripts so consecutive videos don't end on the same shape and the same
// accounting-metaphor register — one fixed close description was producing
// verbatim-similar endings (antagonist sits quietly -> ally does a chore ->
// aphorism about accounts -> object rests in place). Directive is injected
// into the close prompt via chapter.tmpl's `{{if eq .Spec.Beat "close"}}`
// block; ID is only for logs/readability.
type CloseImageSpec struct {
	ID        string `yaml:"id"`
	Directive string `yaml:"directive"`
}

// CTASet holds the interchangeable call-to-action copy for the three cta
// beats. These are NOT model-generated: two back-to-back scripts produced
// verbatim-identical CTAs, so the cta beats are filled from this rotated
// pool instead of the LLM. Each list rotates by recorded-script count so
// consecutive videos never repeat a CTA. In Open variants the literal
// "{object}" is replaced with the seed's object_container.
type CTASet struct {
	Open []string `yaml:"open"`
	Mid1 []string `yaml:"mid_1"`
	Mid2 []string `yaml:"mid_2"`
}

// For returns the variant pool for a cta beat name, or nil when the beat
// isn't a cta beat or has no configured variants (caller then falls back to
// normal LLM generation).
func (c CTASet) For(beat string) []string {
	switch beat {
	case "cta_open":
		return c.Open
	case "cta_mid_1":
		return c.Mid1
	case "cta_mid_2":
		return c.Mid2
	default:
		return nil
	}
}

type Chapters struct {
	Chapters []ChapterSpec `yaml:"chapters"`
	// CloseImages are rotated across scripts to vary the final image; see
	// CloseImageSpec. Empty is valid (close falls back to its description
	// alone) so existing configs without this key keep working.
	CloseImages []CloseImageSpec `yaml:"close_images"`
	// CTA is the rotated, config-driven copy for the cta beats; see CTASet.
	// Empty is valid (cta beats then fall back to LLM generation).
	CTA CTASet `yaml:"cta"`
}

func (c Chapters) TotalWords() int {
	n := 0
	for _, ch := range c.Chapters {
		n += ch.TargetWords
	}
	return n
}

type NamePool struct {
	FirstNames []string `yaml:"first_names"`
	LastNames  []string `yaml:"last_names"`
}

// RegionNames.Generations is keyed "young" (age 25-35) and "old" (age
// 55-70) per the brief.
type RegionNames struct {
	Name        string              `yaml:"name"`
	Towns       []string            `yaml:"towns"`
	Generations map[string]NamePool `yaml:"generations"`
}

type Names struct {
	Regions []RegionNames `yaml:"regions"`
}

func (n Names) Region(name string) (RegionNames, bool) {
	for _, r := range n.Regions {
		if r.Name == name {
			return r, true
		}
	}
	return RegionNames{}, false
}

func (n Names) RegionNames() []string {
	out := make([]string, len(n.Regions))
	for i, r := range n.Regions {
		out[i] = r.Name
	}
	return out
}

type BannedPhrases struct {
	Phrases []string `yaml:"phrases"`
}

// Provider is one entry in Settings.Providers, tried in order by
// llm.WithFailover. APIKeyEnv names the environment variable holding the
// key (empty for a key-less endpoint like local Ollama) — the mapping from
// provider to credential lives entirely in config, never in Go code, so
// adding a provider is a config-only change.
type Provider struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`

	// Models maps a role (generate, summary, review, continuity) to the
	// model THIS provider should use for it — not a single model, because
	// pinning every role to one model on failover silently collapses the
	// generate/summary/review split (every call ends up on whichever one
	// model the provider has). Omit a role to fall through to whatever
	// model the caller already requested.
	Models map[string]string `yaml:"models,omitempty"`

	// SupportsThinkingConfig marks a provider as a Gemini-family endpoint
	// that understands the "google": {"thinking_config": ...} extra_body
	// extension — Settings.ThinkingBudget is only applied to providers
	// with this set to true. Groq, Cerebras, etc. don't support it.
	SupportsThinkingConfig bool `yaml:"supports_thinking_config,omitempty"`

	// NativeThinkingAPI routes this provider through Gemini's native
	// generateContent endpoint (llm.NewGeminiNativeClient) instead of the
	// OpenAI-compatible one. Use this when checkThinkingDisabled's
	// diagnostic (`gen stats` / `gen generate`) shows output tokens
	// meaningfully exceeding the visible text — confirmed against the
	// real API: Gemini's OpenAI-compat layer accepts reasoning_effort but
	// never reports back how many tokens it actually spent thinking, while
	// the native endpoint's usageMetadata.thoughtsTokenCount does.
	NativeThinkingAPI bool `yaml:"native_thinking_api,omitempty"`

	// Roles restricts failover to only route these roles to this provider
	// — empty means every role. Use this for a backup provider whose own
	// limits (see MaxTokensPerRequest) can't handle the biggest calls
	// (chapter generation, continuity's whole-script check) but is fine
	// for small ones (summary, review).
	Roles []string `yaml:"roles,omitempty"`

	// MaxTokensPerRequest is this provider's own hard per-request token
	// cap (0 = none enforced here). Failover skips this provider for a
	// request estimated to exceed it, instead of sending it and getting
	// back a 413.
	MaxTokensPerRequest int `yaml:"max_tokens_per_request,omitempty"`
}

type QualityThreshold struct {
	MeanMin float64 `yaml:"mean_min"`
	AxisMin float64 `yaml:"axis_min"`
}

type Settings struct {
	Language string `yaml:"language"`

	Providers     []Provider `yaml:"providers"`
	GenerateModel string     `yaml:"generate_model"`
	SummaryModel  string     `yaml:"summary_model"`
	ReviewModel   string     `yaml:"review_model"`

	TargetWords int `yaml:"target_words"`
	WPM         int `yaml:"wpm"`

	QualityThreshold QualityThreshold `yaml:"quality_threshold"`
	FullContext      bool             `yaml:"full_context"`

	// ThinkingBudget maps a role (generate, summary, review, continuity)
	// to the Gemini thinking-token budget for that role, applied only to
	// providers with SupportsThinkingConfig set. A role with no entry
	// here gets no thinking_config override at all (provider default).
	ThinkingBudget map[string]int `yaml:"thinking_budget,omitempty"`

	MaxBibleRetries     int `yaml:"max_bible_retries"`
	MaxChapterRetries   int `yaml:"max_chapter_retries"`
	MaxReviewRetries    int `yaml:"max_review_retries"`
	MaxContinuityFixes  int `yaml:"max_continuity_fixes"`
	MaxRepetitionRounds int `yaml:"max_repetition_rounds"` // rounds of whole-script repetition-guard regeneration before giving up

	// MaxTechnicalRetries budgets truncated/malformed JSON completions
	// separately from MaxChapterRetries: a cut-off response is a token-
	// budget problem, not a content-quality one, so it gets its own
	// counter (and an automatic +50% MaxTokens bump each time) instead of
	// spending one of the chapter's real content-quality attempts.
	MaxTechnicalRetries int `yaml:"max_technical_retries"`

	// MaxCostUSD stops one script's generation once its running cost
	// (computed from recorded usage against pricing.yaml's rates) crosses
	// this — 0 disables the check. The stop always happens right after a
	// save, so the script's state up to that point is never lost, only
	// generation beyond the limit.
	MaxCostUSD float64 `yaml:"max_cost_usd,omitempty"`

	ProfessionCooldown int `yaml:"profession_cooldown"`

	RateLimitRPS   float64       `yaml:"rate_limit_rps"` // 0 disables the global throttle
	RequestTimeout time.Duration `yaml:"request_timeout"`

	ChapterBreakMS   int `yaml:"chapter_break_ms"`
	ParagraphBreakMS int `yaml:"paragraph_break_ms"`

	DBPath    string `yaml:"db_path"`
	OutputDir string `yaml:"output_dir"`
}

// ModelPricing is one model's $/million-token rates, used to turn
// llm.Response token counts into a dollar figure for `gen stats`.
// Thinking tokens are billed at OutputPerMillion — they're already
// counted within TokensOut, this is purely about which rate applies.
type ModelPricing struct {
	InputPerMillion  float64 `yaml:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million"`
}

// Pricing maps a model name to its rates. A model with no entry means
// "cost unknown" to callers — never a silent zero passed off as real.
type Pricing struct {
	Models map[string]ModelPricing `yaml:"models"`
}

func (p Pricing) For(model string) (ModelPricing, bool) {
	mp, ok := p.Models[model]
	return mp, ok
}

type Config struct {
	Axes          Axes
	Professions   Professions
	Chapters      Chapters
	Names         Names
	BannedPhrases BannedPhrases
	Settings      Settings
	Pricing       Pricing
}

func Load(dir string) (*Config, error) {
	axes, err := loadYAML[Axes](filepath.Join(dir, "axes.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: axes: %w", err)
	}
	professions, err := loadYAML[Professions](filepath.Join(dir, "professions.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: professions: %w", err)
	}
	chapters, err := loadYAML[Chapters](filepath.Join(dir, "chapters.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: chapters: %w", err)
	}
	names, err := loadYAML[Names](filepath.Join(dir, "names.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: names: %w", err)
	}
	banned, err := loadYAML[BannedPhrases](filepath.Join(dir, "banned_phrases.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: banned_phrases: %w", err)
	}
	settings, err := loadYAML[Settings](filepath.Join(dir, "settings.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: settings: %w", err)
	}
	pricing, err := loadOptionalYAML[Pricing](filepath.Join(dir, "pricing.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: pricing: %w", err)
	}

	cfg := &Config{
		Axes: axes, Professions: professions, Chapters: chapters,
		Names: names, BannedPhrases: banned, Settings: settings, Pricing: pricing,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadOptionalYAML is loadYAML but a missing file returns the zero value
// instead of an error — pricing.yaml is informational (cost display in
// `gen stats`), not required for the pipeline to run.
func loadOptionalYAML[T any](path string) (T, error) {
	var out T
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return out, nil
	}
	return loadYAML[T](path)
}

func loadYAML[T any](path string) (T, error) {
	var out T
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

func (c *Config) Validate() error {
	if err := c.validateProfessions(); err != nil {
		return err
	}
	if err := c.validateAxes(); err != nil {
		return err
	}
	if err := c.validateChapters(); err != nil {
		return err
	}
	if err := c.validateNames(); err != nil {
		return err
	}
	if len(c.BannedPhrases.Phrases) == 0 {
		return fmt.Errorf("config: banned_phrases.yaml must not be empty")
	}
	if len(c.Settings.Providers) == 0 {
		return fmt.Errorf("config: settings.yaml must define at least one provider")
	}
	if c.Settings.TargetWords <= 0 {
		return fmt.Errorf("config: settings.yaml target_words must be positive")
	}
	if c.Settings.WPM <= 0 {
		return fmt.Errorf("config: settings.yaml wpm must be positive")
	}
	return nil
}

func (c *Config) validateProfessions() error {
	if len(c.Professions.Professions) == 0 {
		return fmt.Errorf("config: professions.yaml must define at least one profession")
	}
	seen := make(map[string]bool, len(c.Professions.Professions))
	for _, p := range c.Professions.Professions {
		if p.Name == "" {
			return fmt.Errorf("config: professions.yaml has a profession with an empty name")
		}
		if seen[p.Name] {
			return fmt.Errorf("config: professions.yaml duplicate profession %q", p.Name)
		}
		seen[p.Name] = true
		if p.Epistemology == "" {
			return fmt.Errorf("config: profession %q has no epistemology", p.Name)
		}
		if p.RecordType == "" {
			return fmt.Errorf("config: profession %q has no record_type", p.Name)
		}
		if p.Attire == "" {
			return fmt.Errorf("config: profession %q has no attire", p.Name)
		}
		if len(p.Exposes) == 0 {
			return fmt.Errorf("config: profession %q exposes no betrayal types", p.Name)
		}
	}
	return nil
}

func (c *Config) validateAxes() error {
	weighted := []struct {
		name   string
		values []WeightedValue
	}{
		{"antagonist", c.Axes.Antagonist},
		{"weak_ally", c.Axes.WeakAlly},
		{"humiliation_type", c.Axes.HumiliationType},
		{"betrayal", c.Axes.Betrayal},
		{"written_overreach", c.Axes.WrittenOverreach},
		{"object_container", c.Axes.ObjectContainer},
		{"legacy_artifact", c.Axes.LegacyArtifact},
		{"ending_type", c.Axes.EndingType},
		{"protagonist_sex", c.Axes.ProtagonistSex},
	}
	for _, a := range weighted {
		if len(a.values) == 0 {
			return fmt.Errorf("config: axes.yaml: %q must have at least one value", a.name)
		}
		for _, v := range a.values {
			if err := validateWeighted(v); err != nil {
				return fmt.Errorf("config: axes.yaml %q: %w", a.name, err)
			}
		}
	}

	if len(c.Axes.Duration.Short) == 0 || len(c.Axes.Duration.Long) == 0 {
		return fmt.Errorf("config: axes.yaml duration must have at least one \"short\" and one \"long\" value")
	}
	if len(c.Axes.ReckoningPlace.Private) == 0 || len(c.Axes.ReckoningPlace.Public) == 0 {
		return fmt.Errorf("config: axes.yaml reckoning_place must have at least one \"private\" and one \"public\" value")
	}
	return nil
}

func (c *Config) validateChapters() error {
	if len(c.Chapters.Chapters) == 0 {
		return fmt.Errorf("config: chapters.yaml must define at least one chapter")
	}
	for i, ch := range c.Chapters.Chapters {
		wantIndex := i + 1
		if ch.Index != wantIndex {
			return fmt.Errorf("config: chapters.yaml entry %d has index %d, expected %d (indices must be sequential from 1)",
				i, ch.Index, wantIndex)
		}
		if ch.Beat == "" {
			return fmt.Errorf("config: chapters.yaml entry %d has no beat", i)
		}
		if ch.TargetWords <= 0 {
			return fmt.Errorf("config: chapters.yaml beat %q has no positive target_words", ch.Beat)
		}
	}
	return nil
}

func (c *Config) validateNames() error {
	if len(c.Names.Regions) == 0 {
		return fmt.Errorf("config: names.yaml must define at least one region")
	}
	for _, r := range c.Names.Regions {
		if r.Name == "" {
			return fmt.Errorf("config: names.yaml has a region with an empty name")
		}
		if len(r.Towns) == 0 {
			return fmt.Errorf("config: names.yaml region %q has no towns", r.Name)
		}
		for _, gen := range []string{"young", "old"} {
			pool, ok := r.Generations[gen]
			if !ok {
				return fmt.Errorf("config: names.yaml region %q is missing the %q generation", r.Name, gen)
			}
			if len(pool.FirstNames) == 0 || len(pool.LastNames) == 0 {
				return fmt.Errorf("config: names.yaml region %q generation %q needs first_names and last_names", r.Name, gen)
			}
		}
	}
	return nil
}

func validateWeighted(v WeightedValue) error {
	if v.Value == "" {
		return fmt.Errorf("entry with empty value")
	}
	if v.Weight < 0 {
		return fmt.Errorf("%q has a negative weight", v.Value)
	}
	return nil
}
