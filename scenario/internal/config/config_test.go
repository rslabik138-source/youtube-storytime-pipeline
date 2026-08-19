package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// validConfigDir writes a minimal-but-valid set of the six config files to
// a fresh temp dir. Tests that want an invalid config start from this and
// overwrite exactly the one file under test.
func validConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "axes.yaml", `
antagonist:
  - value: mother_in_law
weak_ally:
  - value: father
humiliation_type:
  - value: dismissive_joke
betrayal:
  - value: savings_taken
duration:
  short: [3, 7]
  long: [9, 11]
written_overreach:
  - value: typed_demand_list
object_container:
  - value: tin_box
legacy_artifact:
  - value: childs_drawing
reckoning_place:
  private: [kitchen_table]
  public: [church_hall]
ending_type:
  - value: cold_silence
protagonist_sex:
  - value: female
`)
	writeFile(t, dir, "professions.yaml", `
professions:
  - name: nurse
    epistemology: "when it bleeds, document, don't panic"
    record_type: care log
    attire: faded scrubs, a laminated ID badge
    exposes: [savings_taken, care_fund_drained]
`)
	writeFile(t, dir, "chapters.yaml", `
chapters:
  - index: 1
    beat: hook
    target_words: 170
    description: "name, age, humiliation in direct speech"
  - index: 2
    beat: cta_open
    target_words: 85
    description: "subscribe tied to the unopened object"
`)
	writeFile(t, dir, "names.yaml", `
regions:
  - name: midwest
    towns: [Cedar Falls, Millbrook]
    generations:
      young:
        first_names: [Dana, Erin]
        last_names: [Whitfield, Voss]
      old:
        first_names: [Carol, Russell]
        last_names: [Whitfield, Voss]
`)
	writeFile(t, dir, "banned_phrases.yaml", `
phrases:
  - "little did i know"
`)
	writeFile(t, dir, "settings.yaml", `
language: en
providers:
  - name: google
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GOOGLE_API_KEY
  - name: backup
    base_url: https://api.backup.example/v1
    api_key_env: BACKUP_API_KEY
    roles: [summary, review]
    max_tokens_per_request: 12000
generate_model: gemini-test
summary_model: gemini-test
review_model: gemini-test
target_words: 8500
wpm: 150
quality_threshold:
  mean_min: 7.0
  axis_min: 5.0
full_context: true
max_bible_retries: 3
max_chapter_retries: 3
max_review_retries: 2
max_continuity_fixes: 5
profession_cooldown: 8
rate_limit_rps: 0
request_timeout: 10m
chapter_break_ms: 700
paragraph_break_ms: 350
db_path: data/scenario.db
output_dir: output
`)
	return dir
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(validConfigDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Professions.Professions) != 1 {
		t.Fatalf("expected 1 profession, got %d", len(cfg.Professions.Professions))
	}
	if cfg.Settings.RequestTimeout != 10*time.Minute {
		t.Fatalf("expected 10m request timeout, got %v", cfg.Settings.RequestTimeout)
	}
	if len(cfg.Chapters.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(cfg.Chapters.Chapters))
	}
	if len(cfg.Settings.Providers) != 2 || cfg.Settings.Providers[0].Name != "google" {
		t.Fatalf("expected 2 providers, first named google, got %+v", cfg.Settings.Providers)
	}
	backup := cfg.Settings.Providers[1]
	if backup.Name != "backup" || len(backup.Roles) != 2 || backup.Roles[0] != "summary" || backup.Roles[1] != "review" {
		t.Fatalf("expected backup provider to declare roles [summary review], got %+v", backup)
	}
	if backup.MaxTokensPerRequest != 12000 {
		t.Fatalf("expected backup provider's max_tokens_per_request to be 12000, got %d", backup.MaxTokensPerRequest)
	}
	if len(cfg.Pricing.Models) != 0 {
		t.Fatalf("expected no pricing when pricing.yaml is absent, got %+v", cfg.Pricing.Models)
	}
}

func TestLoadWithPricingFile(t *testing.T) {
	dir := validConfigDir(t)
	writeFile(t, dir, "pricing.yaml", `
models:
  gemini-3.6-flash:
    input_per_million: 1.50
    output_per_million: 7.50
  gemini-3.5-flash-lite:
    input_per_million: 0.10
    output_per_million: 0.40
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mp, ok := cfg.Pricing.For("gemini-3.6-flash")
	if !ok {
		t.Fatalf("expected pricing for gemini-3.6-flash to be found")
	}
	if mp.InputPerMillion != 1.50 || mp.OutputPerMillion != 7.50 {
		t.Fatalf("unexpected pricing: %+v", mp)
	}

	if _, ok := cfg.Pricing.For("unknown-model"); ok {
		t.Fatalf("expected an unknown model to report ok=false, not a zero-value rate")
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		wantErr string
	}{
		{
			name:    "empty professions",
			file:    "professions.yaml",
			content: `professions: []`,
			wantErr: "at least one profession",
		},
		{
			name: "profession without attire",
			file: "professions.yaml",
			content: `
professions:
  - name: nurse
    epistemology: "x"
    record_type: "y"
    exposes: [savings_taken]
`,
			wantErr: "has no attire",
		},
		{
			name: "profession without exposes",
			file: "professions.yaml",
			content: `
professions:
  - name: nurse
    epistemology: "x"
    record_type: "y"
    attire: "z"
    exposes: []
`,
			wantErr: "exposes no betrayal",
		},
		{
			name: "duplicate profession",
			file: "professions.yaml",
			content: `
professions:
  - name: nurse
    epistemology: a
    record_type: b
    attire: p
    exposes: [x]
  - name: nurse
    epistemology: c
    record_type: d
    attire: q
    exposes: [y]
`,
			wantErr: "duplicate profession",
		},
		{
			name: "empty axis",
			file: "axes.yaml",
			content: `
antagonist: []
weak_ally: [{value: father}]
humiliation_type: [{value: x}]
betrayal: [{value: x}]
duration: {short: [3], long: [9]}
written_overreach: [{value: x}]
object_container: [{value: x}]
legacy_artifact: [{value: x}]
reckoning_place: {private: [kitchen_table], public: [church_hall]}
ending_type: [{value: x}]
protagonist_sex: [{value: female}]
`,
			wantErr: "antagonist",
		},
		{
			name: "duration missing long bucket",
			file: "axes.yaml",
			content: `
antagonist: [{value: a}]
weak_ally: [{value: a}]
humiliation_type: [{value: a}]
betrayal: [{value: a}]
duration: {short: [3, 7], long: []}
written_overreach: [{value: a}]
object_container: [{value: a}]
legacy_artifact: [{value: a}]
reckoning_place: {private: [a], public: [b]}
ending_type: [{value: a}]
protagonist_sex: [{value: a}]
`,
			wantErr: "duration must have",
		},
		{
			name: "reckoning_place missing public bucket",
			file: "axes.yaml",
			content: `
antagonist: [{value: a}]
weak_ally: [{value: a}]
humiliation_type: [{value: a}]
betrayal: [{value: a}]
duration: {short: [3], long: [9]}
written_overreach: [{value: a}]
object_container: [{value: a}]
legacy_artifact: [{value: a}]
reckoning_place: {private: [a], public: []}
ending_type: [{value: a}]
protagonist_sex: [{value: a}]
`,
			wantErr: "reckoning_place must have",
		},
		{
			name: "chapters not sequential",
			file: "chapters.yaml",
			content: `
chapters:
  - index: 1
    beat: hook
    target_words: 170
    description: x
  - index: 3
    beat: pivot
    target_words: 170
    description: x
`,
			wantErr: "expected 2",
		},
		{
			name: "names missing a generation",
			file: "names.yaml",
			content: `
regions:
  - name: midwest
    towns: [Cedar Falls]
    generations:
      young:
        first_names: [Dana]
        last_names: [Voss]
`,
			wantErr: `missing the "old" generation`,
		},
		{
			name:    "empty banned phrases",
			file:    "banned_phrases.yaml",
			content: `phrases: []`,
			wantErr: "banned_phrases.yaml must not be empty",
		},
		{
			name: "no providers",
			file: "settings.yaml",
			content: `
language: en
providers: []
generate_model: x
summary_model: x
review_model: x
target_words: 8500
wpm: 150
`,
			wantErr: "at least one provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := validConfigDir(t)
			writeFile(t, dir, tt.file, tt.content)

			_, err := Load(dir)
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestExposesFor(t *testing.T) {
	cfg, err := Load(validConfigDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	exposes, ok := cfg.Professions.ExposesFor("nurse")
	if !ok || len(exposes) != 2 {
		t.Fatalf("expected 2 exposed betrayal types for nurse, got %v (ok=%v)", exposes, ok)
	}
	if _, ok := cfg.Professions.ExposesFor("unknown_profession"); ok {
		t.Fatalf("expected ok=false for an unknown profession")
	}
}

func TestNamesRegion(t *testing.T) {
	cfg, err := Load(validConfigDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, ok := cfg.Names.Region("midwest")
	if !ok || len(r.Towns) != 2 {
		t.Fatalf("expected the midwest region with 2 towns, got %+v (ok=%v)", r, ok)
	}
	if _, ok := cfg.Names.Region("nowhere"); ok {
		t.Fatalf("expected ok=false for an unknown region")
	}
}
