package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"testing"
	"text/template"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/continuity"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/review"
	"github.com/placeholder/scenario/internal/seed"
	"github.com/placeholder/scenario/internal/store"
	"github.com/placeholder/scenario/internal/story"
)

// ==================== fixtures ====================

func testAxes() config.Axes {
	return config.Axes{
		Antagonist:       []config.WeightedValue{{Value: "mother_in_law"}},
		WeakAlly:         []config.WeightedValue{{Value: "father"}},
		HumiliationType:  []config.WeightedValue{{Value: "dismissive_joke"}},
		Duration:         config.DurationCategory{Short: []int{3}, Long: []int{9}},
		WrittenOverreach: []config.WeightedValue{{Value: "email_chain"}},
		ObjectContainer:  []config.WeightedValue{{Value: "tin_box"}},
		LegacyArtifact:   []config.WeightedValue{{Value: "childs_drawing"}},
		ReckoningPlace:   config.ReckoningPlaceCategory{Private: []string{"kitchen_table"}, Public: []string{"church_hall"}},
		EndingType:       []config.WeightedValue{{Value: "cold_silence"}},
		ProtagonistSex:   []config.WeightedValue{{Value: "female"}},
	}
}

func testProfessions() config.Professions {
	return config.Professions{Professions: []config.Profession{
		{Name: "nurse", Epistemology: "documents everything", RecordType: "care log", Exposes: []string{"savings_taken"}},
	}}
}

func testNames() config.Names {
	return config.Names{Regions: []config.RegionNames{
		{Name: "midwest", Towns: []string{"Cedar Falls"}, Generations: map[string]config.NamePool{
			"young": {FirstNames: []string{"Dana"}, LastNames: []string{"Whitfield"}},
			"old":   {FirstNames: []string{"Russell"}, LastNames: []string{"Voss"}},
		}},
	}}
}

func testChapters() config.Chapters {
	return config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "pivot", TargetWords: 40, Description: "turn"},
		{Index: 3, Beat: "close", TargetWords: 40, Description: "ending"},
	}}
}

func testSettings() config.Settings {
	return config.Settings{
		GenerateModel:      "gen-model",
		SummaryModel:       "sum-model",
		ReviewModel:        "rev-model",
		TargetWords:        120,
		WPM:                150,
		QualityThreshold:   config.QualityThreshold{MeanMin: 7.0, AxisMin: 5.0},
		FullContext:        false,
		MaxBibleRetries:    2,
		MaxChapterRetries:  2,
		MaxReviewRetries:   1,
		MaxContinuityFixes: 5,
	}
}

func testBibleTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("bible").Parse(
		"SEED profession={{.Seed.Profession}} used={{.UsedNames}} violations={{.Violations}}\n")
	if err != nil {
		t.Fatalf("parse bible template: %v", err)
	}
	return tmpl
}

func testChapterTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("chapter").Parse(
		"CHAPTER idx={{.Spec.Index}} beat={{.Spec.Beat}} target={{.Spec.TargetWords}} fix={{.FixInstruction}} violations={{.Violations}} part={{.PartNumber}}/{{.PartTotal}} priorpart=[{{.PriorPartText}}] avoidstarts={{.AvoidParagraphStarts}} avoidphrases={{.AvoidPhrases}} money={{.MoneyMentionCounts}} crossavoid={{.CrossScriptAvoidPhrases}}\n")
	if err != nil {
		t.Fatalf("parse chapter template: %v", err)
	}
	return tmpl
}

func testPointFixTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("pointfix").Parse(
		"POINTFIX sentence=[{{.Sentence}}] avoid={{.Avoid}}\n")
	if err != nil {
		t.Fatalf("parse pointfix template: %v", err)
	}
	return tmpl
}

func testSummaryTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("summary").Parse("SUMMARIZE: {{.Text}}\n")
	if err != nil {
		t.Fatalf("parse summary template: %v", err)
	}
	return tmpl
}

func testContinuityTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("continuity").Parse("CONTINUITY {{.FullText}}\n")
	if err != nil {
		t.Fatalf("parse continuity template: %v", err)
	}
	return tmpl
}

func testReviewTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("review").Parse("REVIEW {{.FullText}}\n")
	if err != nil {
		t.Fatalf("parse review template: %v", err)
	}
	return tmpl
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestOrchestrator(t *testing.T, mainClient, continuityClient, reviewClient llm.Client, st store.Store, settings config.Settings, chapters config.Chapters) *Orchestrator {
	t.Helper()
	return newTestOrchestratorWithPricing(t, mainClient, continuityClient, reviewClient, st, settings, chapters, config.Pricing{})
}

func newTestOrchestratorWithPricing(t *testing.T, mainClient, continuityClient, reviewClient llm.Client, st store.Store, settings config.Settings, chapters config.Chapters, pricing config.Pricing) *Orchestrator {
	t.Helper()
	seedGen := seed.NewGenerator(testAxes(), testProfessions(), testNames(), st, seed.Constraints{MaxAttempts: 50})
	checker := continuity.NewChecker(continuityClient, testContinuityTemplate(t), "continuity-model", settings.MaxContinuityFixes, settings.FullContext)
	reviewer := review.NewReviewer(reviewClient, testReviewTemplate(t), "review-model")
	tmpls := Templates{Bible: testBibleTemplate(t), Chapter: testChapterTemplate(t), Summary: testSummaryTemplate(t), PointFix: testPointFixTemplate(t)}
	return NewOrchestrator(seedGen, mainClient, tmpls, checker, reviewer, st, chapters, []string{"zzz_banned_phrase"}, settings, pricing, testLogger())
}

// fillerPool needs to be large enough that 6-word sliding windows sampled
// across several test chapters don't coincidentally collide and trip the
// whole-script repetition guard by test-fixture accident.
var fillerPool = []string{
	"ledger", "receipt", "invoice", "kitchen", "morning", "silence", "porch", "engine", "thread", "needle",
	"garden", "window", "letter", "folder", "drawer", "circuit", "wiring", "record", "shelf", "hallway",
	"quiet", "table", "light", "across", "worn", "floor", "while", "everyone", "else", "kept",
	"talking", "about", "nothing", "much", "that", "particular", "evening", "until", "someone", "finally",
	"noticed", "long", "standing", "near", "door", "watching", "clock", "above", "sink", "outside",
	"wind", "moved", "through", "bare", "branches", "slow", "steady", "way", "nobody", "seemed",
	"hear", "smelled", "like", "coffee", "and", "old", "paper", "distant", "radio", "hummed",
	"low", "voice", "drifting", "past", "screen", "gravel", "driveway", "settled", "under", "tires",
	"late", "arriving", "car", "shadow", "fell", "yard", "longer", "each", "passing", "year",
}

// fillerText generates deterministic-but-varied text with short sentences
// (well under the 22-word max) and no digits, for a word count validators
// will accept exactly. It mixes short 5-9 word sentences with an
// occasional 16-18 word one every 3rd sentence — sentence-length variance
// is only a warning now (story.ValidateChapterSentenceLengthVariance), so
// this no longer needs to clear any blocking threshold; it just keeps the
// fixture text looking like real prose.
func fillerText(words int, seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	var sb strings.Builder
	count := 0
	sentenceIndex := 0
	for count < words {
		sentenceLen := 4 + rng.Intn(3) // 4-6
		if sentenceIndex%3 == 2 {
			sentenceLen = 16 + rng.Intn(3) // 16-18: reliably clears the variance threshold even in a short chapter
		}
		if sentenceLen > words-count {
			sentenceLen = words - count
		}
		for i := 0; i < sentenceLen; i++ {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(fillerPool[rng.Intn(len(fillerPool))])
			count++
		}
		sb.WriteString(". ")
		sentenceIndex++
	}
	return strings.TrimSpace(sb.String())
}

// manyLongSentences builds n sentences of exactly wordsPerSentence words
// each — used to trip several sentence_length ERROR violations at once
// (wordsPerSentence > 28, story.DefaultChapterValidatorConfig's
// MaxSentenceWords) in a single generation attempt, deterministically and
// without relying on randomness.
func manyLongSentences(n, wordsPerSentence int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		for j := 0; j < wordsPerSentence; j++ {
			sb.WriteString(fillerPool[(i*wordsPerSentence+j)%len(fillerPool)])
			sb.WriteByte(' ')
		}
		sb.WriteString(". ")
	}
	return strings.TrimSpace(sb.String())
}

func chapterJSON(words int, seed int64) string {
	return chapterJSONFromText(fillerText(words, seed))
}

// hookChapterJSON is chapterJSON's counterpart for chapter 1 (hook) tests
// that don't care about the hook-identity validator specifically — it
// satisfies ValidateChapterHookIdentity (name, age, one line of direct
// speech) so tests about retries/cost/model-routing/etc. aren't tripped up
// by a check they're not testing. Matches goodBible()'s narrator: Dana
// Whitfield, age 42.
func hookChapterJSON(words int, seed int64) string {
	text := "My name is Dana Whitfield and I am forty-two years old. \"You will never run this place,\" my partner said without flinching, and I stayed quiet. " + fillerText(words, seed)
	return chapterJSONFromText(text)
}

func chapterJSONFromText(text string) string {
	b, err := json.Marshal(chapterResponse{Text: text, DisplayText: text})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func goodBible() string {
	b, err := json.Marshal(bibleResponse{
		Title:         "The Ledger Never Lies",
		Narrator:      story.Person{Name: "Dana Whitfield", Age: 42, Role: "narrator", City: "Cedar Falls"},
		Cast:          []story.Person{{Name: "Russell Voss", Age: 61, Role: "antagonist"}},
		Timeline:      []story.Event{{Year: 1, What: "the humiliation"}, {Year: 8, What: "the reckoning"}},
		FamilyLaw:     "the numbers always add up eventually",
		RefrainPhrase: "I kept the ledger the way I kept everything",
		SeededLine:    "a receipt never lies",
		Numbers:       map[string]string{"amount": "twelve hundred dollars"},
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func bibleWithDuplicateNames() string {
	b, err := json.Marshal(bibleResponse{
		Title:     "Bad Bible",
		Narrator:  story.Person{Name: "Dana Whitfield", Role: "narrator", City: "Cedar Falls"},
		Cast:      []story.Person{{Name: "Dana Whitfield", Role: "antagonist"}}, // duplicate on purpose
		Timeline:  []story.Event{{Year: 1, What: "start"}},
		FamilyLaw: "law", RefrainPhrase: "refrain", SeededLine: "seed",
		Numbers: map[string]string{},
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func passingReviewJSON() string {
	return `{"scores": {"hook_strength": 8, "profession_causality": 8, "restraint": 8, "scene_not_summary": 8, "planting_payoff": 8, "refusal_present": 8, "ai_smell": 8, "comment": "good"}, "weak_chapters": []}`
}

func lowReviewJSON(weakIndex int) string {
	return fmt.Sprintf(`{"scores": {"hook_strength": 4, "profession_causality": 4, "restraint": 4, "scene_not_summary": 4, "planting_payoff": 4, "refusal_present": 4, "ai_smell": 4, "comment": "weak"}, "weak_chapters": [{"index": %d, "axis": "hook_strength", "reason": "needs work"}]}`, weakIndex)
}

func noFixesJSON() string { return `{"fixes": []}` }

func oneFixJSON(chapterIndex int) string {
	return fmt.Sprintf(`{"fixes": [{"chapter_index": %d, "issue": "name drifted", "instruction": "keep the name consistent"}]}`, chapterIndex)
}

func manyFixesJSON(n int) string {
	var fixes []string
	for i := 1; i <= n; i++ {
		fixes = append(fixes, fmt.Sprintf(`{"chapter_index": %d, "issue": "x", "instruction": "y"}`, i))
	}
	return `{"fixes": [` + strings.Join(fixes, ",") + `]}`
}

func resp(text string) llm.Response { return llm.Response{Text: text, Provider: "test-provider"} }

// ==================== tests ====================

func TestGenerateStopsWhenCostLimitExceededButSavesProgress(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
	}}

	// OutputPerMillion of 1,000,000 makes cost-in-dollars equal to
	// TokensOut exactly, so the arithmetic here is easy to verify: bible
	// ($1) + chapter 1 ($5) + its summary ($1) = $7, crossing a $5 limit
	// right after chapter 1's own save.
	genResp := func(text string, tokensOut int) llm.Response {
		return llm.Response{Text: text, Provider: "test-provider", Model: "gen-model", TokensOut: tokensOut}
	}
	sumResp := func(text string, tokensOut int) llm.Response {
		return llm.Response{Text: text, Provider: "test-provider", Model: "sum-model", TokensOut: tokensOut}
	}

	mainClient := llm.NewFakeClient(
		genResp(goodBible(), 1),
		genResp(hookChapterJSON(40, 1), 5),
		sumResp("summary one", 1),
	)
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxCostUSD = 5.0
	pricing := config.Pricing{Models: map[string]config.ModelPricing{
		"gen-model": {InputPerMillion: 0, OutputPerMillion: 1_000_000},
		"sum-model": {InputPerMillion: 0, OutputPerMillion: 1_000_000},
	}}
	o := newTestOrchestratorWithPricing(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter, pricing)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if !errors.Is(err, ErrCostLimitExceeded) {
		t.Fatalf("expected ErrCostLimitExceeded, got %v", err)
	}
	if script != nil {
		t.Fatalf("expected Generate to return a nil script alongside the cost-limit error")
	}

	pending, lerr := st.ListScripts(context.Background(), store.ListFilter{Status: story.StatusPending, Limit: 1})
	if lerr != nil || len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending (saved) script, got %v (err=%v)", pending, lerr)
	}
	saved, gerr := st.GetScript(context.Background(), pending[0].ID)
	if gerr != nil {
		t.Fatalf("GetScript: %v", gerr)
	}
	if len(saved.Chapters) != 1 {
		t.Fatalf("expected chapter 1 to already be saved before the cost limit stopped generation, got %d chapters", len(saved.Chapters))
	}
}

func TestGenerateCostLimitDisabledByDefault(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
		resp(chapterJSON(40, 2)), resp("summary two"),
		resp(chapterJSON(40, 3)), resp("summary three"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxCostUSD = 0 // disabled
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("expected no cost-limit error when MaxCostUSD is 0 (disabled), got %v", err)
	}
	if script.Status != story.StatusAccepted {
		t.Fatalf("expected acceptance, got %q", script.Status)
	}
}

func TestGenerateRecordsUsageBreakdownAcrossAllRoles(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
	}}

	genResp := func(text string, in, out, thinking int) llm.Response {
		return llm.Response{Text: text, Provider: "google-ai-studio", Model: "gemini-3.6-flash", TokensIn: in, TokensOut: out, ThinkingTokens: thinking}
	}
	sumResp := func(text string, in, out int) llm.Response {
		return llm.Response{Text: text, Provider: "google-ai-studio", Model: "gemini-3.5-flash-lite", TokensIn: in, TokensOut: out}
	}

	mainClient := llm.NewFakeClient(
		genResp(goodBible(), 1000, 200, 150),
		genResp(hookChapterJSON(40, 1), 1500, 400, 300),
		sumResp("summary one", 100, 20),
	)
	continuityClient := llm.NewFakeClient(genResp(noFixesJSON(), 2000, 50, 0))
	reviewClient := llm.NewFakeClient(sumResp(passingReviewJSON(), 900, 30))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	byRole := map[string]story.UsageEntry{}
	for _, u := range script.Usage {
		byRole[u.Role] = u
	}

	gen, ok := byRole["generate"]
	if !ok {
		t.Fatalf("expected a generate usage entry, got %+v", script.Usage)
	}
	// bible (1000/200/150) + chapter 1 (1500/400/300) = 2500/600/450
	if gen.Calls != 2 || gen.TokensIn != 2500 || gen.TokensOut != 600 || gen.ThinkingTokens != 450 {
		t.Fatalf("unexpected generate usage: %+v", gen)
	}
	if gen.Model != "gemini-3.6-flash" {
		t.Fatalf("expected generate model gemini-3.6-flash, got %q", gen.Model)
	}

	sum, ok := byRole["summary"]
	if !ok || sum.Calls != 1 || sum.TokensIn != 100 || sum.TokensOut != 20 {
		t.Fatalf("unexpected summary usage: %+v (ok=%v)", sum, ok)
	}

	cont, ok := byRole["continuity"]
	if !ok || cont.Calls != 1 || cont.TokensIn != 2000 || cont.TokensOut != 50 {
		t.Fatalf("unexpected continuity usage: %+v (ok=%v)", cont, ok)
	}

	rev, ok := byRole["review"]
	if !ok || rev.Calls != 1 || rev.TokensIn != 900 || rev.TokensOut != 30 {
		t.Fatalf("unexpected review usage: %+v (ok=%v)", rev, ok)
	}

	wantTotalIn := 2500 + 100 + 2000 + 900
	wantTotalOut := 600 + 20 + 50 + 30
	if script.TokensIn != wantTotalIn || script.TokensOut != wantTotalOut {
		t.Fatalf("expected aggregate tokens in=%d out=%d, got in=%d out=%d", wantTotalIn, wantTotalOut, script.TokensIn, script.TokensOut)
	}

	persisted, err := st.GetScript(context.Background(), script.ID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if len(persisted.Usage) != 4 {
		t.Fatalf("expected the usage breakdown to be persisted (4 entries), got %d: %+v", len(persisted.Usage), persisted.Usage)
	}
}

func TestGenerateHappyPathAcceptsAndSaves(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
		resp(chapterJSON(40, 2)), resp("summary two"),
		resp(chapterJSON(40, 3)), resp("summary three"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if script.Status != story.StatusAccepted {
		t.Fatalf("expected status accepted, got %q", script.Status)
	}
	if script.Title != "The Ledger Never Lies" {
		t.Fatalf("unexpected title: %q", script.Title)
	}
	if len(script.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(script.Chapters))
	}
	for _, ch := range script.Chapters {
		if ch.Summary == "" {
			t.Fatalf("chapter %d has no summary", ch.Index)
		}
	}
	if script.WordCount == 0 {
		t.Fatalf("expected a non-zero word count")
	}

	got, err := st.GetScript(context.Background(), script.ID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if got.Status != story.StatusAccepted {
		t.Fatalf("expected persisted status accepted, got %q", got.Status)
	}

	names, err := st.RecentUsedNames(context.Background(), 30)
	if err != nil {
		t.Fatalf("RecentUsedNames: %v", err)
	}
	if !containsStr(names, "Dana Whitfield") {
		t.Fatalf("expected RecordAcceptance to have recorded the narrator's name, got %v", names)
	}
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestGenerateDryRunStopsAfterBibleAndSavesNothing(t *testing.T) {
	mainClient := llm.NewFakeClient(resp(goodBible()))
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(script.Chapters) != 0 {
		t.Fatalf("expected no chapters on a dry run, got %d", len(script.Chapters))
	}
	if script.Bible.RefrainPhrase == "" {
		t.Fatalf("expected the bible to still be populated on a dry run")
	}

	if _, err := st.GetScript(context.Background(), script.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected a dry run to save nothing, got err=%v", err)
	}
}

func TestGenerateBibleNarratorSexAlwaysMatchesSeedRegardlessOfLLMOutput(t *testing.T) {
	// goodBible() never sets Person.Sex at all — bible.go must still set
	// script.Bible.Narrator.Sex from Seed.ProtagonistSex afterward,
	// unconditionally. That's a hard seed constraint, not something to
	// trust the model to echo back correctly.
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Seed.ProtagonistSex == "" {
		t.Fatalf("test fixture bug: expected a non-empty ProtagonistSex from the seed draw")
	}
	if script.Bible.Narrator.Sex != script.Seed.ProtagonistSex {
		t.Fatalf("expected narrator sex %q to match seed's ProtagonistSex %q", script.Bible.Narrator.Sex, script.Seed.ProtagonistSex)
	}
}

func TestGenerateBibleValidationRetriesThenSucceeds(t *testing.T) {
	mainClient := llm.NewFakeClient(resp(bibleWithDuplicateNames()), resp(goodBible()))
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxBibleRetries = 2
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Title != "The Ledger Never Lies" {
		t.Fatalf("expected the second (valid) bible to win, got title %q", script.Title)
	}
	if len(mainClient.Calls) != 2 {
		t.Fatalf("expected exactly 2 bible calls, got %d", len(mainClient.Calls))
	}
	if !strings.Contains(mainClient.Calls[1].Prompt, "appears more than once") {
		t.Fatalf("expected the retry prompt to carry the violation message, got: %s", mainClient.Calls[1].Prompt)
	}
}

func TestGenerateBibleExhaustsRetriesReturnsError(t *testing.T) {
	mainClient := llm.NewFakeClient(resp(bibleWithDuplicateNames()), resp(bibleWithDuplicateNames()))
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxBibleRetries = 2
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, testChapters())

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("expected an 'exhausted' error, got %v", err)
	}
}

func TestChapterMaxTokensScalesWithTargetAndLeavesHeadroom(t *testing.T) {
	// display_text duplicates roughly the same content as text, plus JSON
	// structure plus thinking tokens — the completion needs real headroom
	// beyond a naive "just enough for the visible words" estimate, or a
	// long chapter's JSON truncates mid-string.
	got := chapterMaxTokens(1190)
	if got < 1190*2 {
		t.Fatalf("expected headroom for both text and display_text (>%d), got %d", 1190*2, got)
	}

	small := chapterMaxTokens(85)
	large := chapterMaxTokens(1190)
	if large <= small {
		t.Fatalf("expected a larger target to get a larger token budget, got small=%d large=%d", small, large)
	}
}

func TestGenerateAutoFixesMissingPunctuationWithoutConsumingQualityBudget(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	brokenText := "My name is Dana Whitfield and I am forty-two years old. \"You will never run this place,\" my partner said without flinching. " +
		"She set the jar down on a Tuesday morning She paused to wave at the mailman. " + fillerText(30, 1)

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(brokenText)),
		resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 1 // if the missing period spent a quality attempt, this would exhaust immediately
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v (a missing period should be fixed mechanically, not treated as a quality failure)", err)
	}
	if !strings.Contains(script.Chapters[0].Text, "morning. She paused") {
		t.Fatalf("expected the missing period to be inserted before validation, got: %s", script.Chapters[0].Text)
	}
	// bible(1) + chapter(1) + summary(1) = 3 — no retry.
	if len(mainClient.Calls) != 3 {
		t.Fatalf("expected 3 calls (no retry), got %d: %+v", len(mainClient.Calls), mainClient.Calls)
	}
}

func TestGenerateChapterValidationRetriesThenSucceeds(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(fillerText(40, 1)+" zzz_banned_phrase.")), // contains a banned phrase: still blocking
		resp(hookChapterJSON(40, 2)), resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 2
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(script.Chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(script.Chapters))
	}
	// bible(1) + chapter attempt 1 (bad, no summary call) + chapter attempt 2 (good) + summary(1) = 4
	if len(mainClient.Calls) != 4 {
		t.Fatalf("expected 4 calls to the main client, got %d", len(mainClient.Calls))
	}
	if !strings.Contains(mainClient.Calls[2].Prompt, "violations=[chapter 1") {
		t.Fatalf("expected the chapter retry prompt to carry the banned-phrase violation, got: %s", mainClient.Calls[2].Prompt)
	}
}

func TestGenerateChapterExhaustsRetriesReturnsError(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	mainClient := llm.NewFakeClient(resp(goodBible()), resp(chapterJSONFromText(fillerText(40, 1)+" zzz_banned_phrase.")))
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 1
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err == nil || !strings.Contains(err.Error(), "chapter 1") {
		t.Fatalf("expected a chapter-1 exhaustion error, got %v", err)
	}
}

func TestGenerateAbortsEarlyWhenFirstThreeChaptersAllHeavilyViolate(t *testing.T) {
	fourChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 200, Description: "opening"},
		{Index: 2, Beat: "pivot", TargetWords: 200, Description: "middle"},
		{Index: 3, Beat: "cast", TargetWords: 200, Description: "cast"},
		{Index: 4, Beat: "close", TargetWords: 200, Description: "ending"},
	}}

	// 6 sentences of 30 words each = 6 sentence_length errors in one
	// attempt (over DefaultChapterValidatorConfig's 28-word cap) — well
	// past the 5-violation early-abort threshold.
	badText := manyLongSentences(6, 30)
	goodText := fillerText(200, 99)
	goodHookText := "My name is Dana Whitfield and I am forty-two years old. You will never run this place, she said without flinching. " + goodText

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(badText)), resp(chapterJSONFromText(goodHookText)), resp("summary one"),
		resp(chapterJSONFromText(badText)), resp(chapterJSONFromText(goodText)), resp("summary two"),
		resp(chapterJSONFromText(badText)), resp(chapterJSONFromText(goodText)), resp("summary three"),
		// nothing queued for chapter 4 — if the early abort didn't fire,
		// the fake client errors on an empty queue instead of silently
		// generating chapter 4.
	)
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 3
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, fourChapters)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if !errors.Is(err, ErrPromptLikelyBroken) {
		t.Fatalf("expected ErrPromptLikelyBroken, got %v", err)
	}

	pending, lerr := st.ListScripts(context.Background(), store.ListFilter{Status: story.StatusPending, Limit: 1})
	if lerr != nil || len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending (saved) script, got %v (err=%v)", pending, lerr)
	}
	saved, gerr := st.GetScript(context.Background(), pending[0].ID)
	if gerr != nil {
		t.Fatalf("GetScript: %v", gerr)
	}
	if len(saved.Chapters) != 3 {
		t.Fatalf("expected the first 3 (troubled but eventually successful) chapters to be saved before the abort, got %d", len(saved.Chapters))
	}
}

func TestGenerateDoesNotAbortWhenOnlyTwoOfFirstThreeChaptersHeavilyViolate(t *testing.T) {
	threeChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 200, Description: "opening"},
		{Index: 2, Beat: "pivot", TargetWords: 200, Description: "middle"},
		{Index: 3, Beat: "cast", TargetWords: 200, Description: "cast"},
	}}

	badText := manyLongSentences(6, 30)
	goodText := fillerText(200, 99)
	goodHookText := "My name is Dana Whitfield and I am forty-two years old. You will never run this place, she said without flinching. " + goodText

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(badText)), resp(chapterJSONFromText(goodHookText)), resp("summary one"),
		resp(chapterJSONFromText(badText)), resp(chapterJSONFromText(goodText)), resp("summary two"),
		resp(chapterJSONFromText(goodText)), resp("summary three"), // chapter 3 is clean on the first try
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 3
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, threeChapters)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("expected no early abort when only 2 of the first 3 chapters were troubled, got %v", err)
	}
	if len(script.Chapters) != 3 {
		t.Fatalf("expected all 3 chapters generated, got %d", len(script.Chapters))
	}
}

func TestGenerateChapterTruncatedJSONDoesNotConsumeQualityBudget(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(`{"text": "this response got cut off mid-strin`), // truncated JSON: a technical failure, not a quality one
		resp(hookChapterJSON(40, 2)), resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 1 // if truncation spent a quality attempt, this would exhaust immediately
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v (a technical truncation should retry on its own budget, not the 1-attempt quality budget)", err)
	}
	if len(script.Chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(script.Chapters))
	}
}

func TestGenerateChapterExhaustsTechnicalRetriesReturnsError(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(`{"text": "cut off one`),
		resp(`{"text": "cut off two`),
	)
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxTechnicalRetries = 2
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err == nil || !strings.Contains(err.Error(), "technical") {
		t.Fatalf("expected a technical-exhaustion error, got %v", err)
	}
}

func TestGenerateSplitChapterMakesTwoCallsWithPartBGivenPartAAsContext(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "the_years", TargetWords: 80, Split: true, Description: "time passes"},
	}}

	partAText := fillerText(40, 1)
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(partAText)), // part A
		resp(chapterJSON(40, 2)),             // part B
		resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(script.Chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(script.Chapters))
	}
	// bible(1) + part A(1) + part B(1) + summary(1) = 4
	if len(mainClient.Calls) != 4 {
		t.Fatalf("expected 4 calls to the main client, got %d", len(mainClient.Calls))
	}
	if !strings.Contains(mainClient.Calls[1].Prompt, "part=1/2") {
		t.Fatalf("expected part A's prompt to be marked part=1/2, got: %s", mainClient.Calls[1].Prompt)
	}
	if !strings.Contains(mainClient.Calls[1].Prompt, "target=40") {
		t.Fatalf("expected part A to target half the chapter's words (40), got: %s", mainClient.Calls[1].Prompt)
	}
	if !strings.Contains(mainClient.Calls[2].Prompt, "part=2/2") {
		t.Fatalf("expected part B's prompt to be marked part=2/2, got: %s", mainClient.Calls[2].Prompt)
	}
	if !strings.Contains(mainClient.Calls[2].Prompt, partAText) {
		t.Fatalf("expected part B's prompt to carry part A's finished text as context, got: %s", mainClient.Calls[2].Prompt)
	}

	if !strings.Contains(script.Chapters[0].Text, partAText) {
		t.Fatalf("expected the combined chapter to include part A's text, got: %s", script.Chapters[0].Text)
	}
	if script.Chapters[0].TargetWords != 80 {
		t.Fatalf("expected the combined chapter to keep the full 80-word target, got %d", script.Chapters[0].TargetWords)
	}
}

func TestGenerateSplitChapterDialogueRequirementCheckedOnCombinedTextOnly(t *testing.T) {
	// the_reckoning needs >= 3 detected lines of direct speech across the
	// WHOLE chapter (story.directSpeechRequiredBeats), but each half of a
	// split chapter is generated as its own call. Part A has only 1 line
	// and part B has only 2 — neither half clears 3 on its own — so this
	// only succeeds if generateChapterPart's SkipDialogueRequirement
	// (partNumber > 0) actually exempts each half from the per-part check,
	// leaving the >= 3 requirement to be enforced once against the
	// combined text in generateSplitChapter's own validation pass.
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "the_reckoning", TargetWords: 80, Split: true, Description: "confrontation"},
	}}

	partAText := fillerText(35, 1) + " Enough, she said."
	partBText := fillerText(30, 2) + " You knew, he muttered. I never forgot, she replied."

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(partAText)), // part A: 1 line of direct speech
		resp(chapterJSONFromText(partBText)), // part B: 2 lines — combined total is 3
		resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v (each half has fewer than 3 lines, so a per-part dialogue check would wrongly reject this)", err)
	}
	// bible(1) + part A(1) + part B(1) + summary(1) = 4 — no regeneration
	// of either part, confirming neither half was individually held to
	// the 3-line minimum.
	if len(mainClient.Calls) != 4 {
		t.Fatalf("expected 4 calls to the main client (no regeneration), got %d: %+v", len(mainClient.Calls), mainClient.Calls)
	}
	if !strings.Contains(script.Chapters[0].Text, partAText) || !strings.Contains(script.Chapters[0].Text, partBText) {
		t.Fatalf("expected the combined chapter to contain both parts' text, got: %s", script.Chapters[0].Text)
	}
}

func TestGenerateSplitChapterPartBFailureOnlyRetriesPartB(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "the_years", TargetWords: 80, Split: true, Description: "time passes"},
	}}

	partAText := fillerText(40, 1)
	partBGoodText := fillerText(40, 3)
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(partAText)), // part A succeeds first try
		resp(chapterJSONFromText(fillerText(40, 2)+" zzz_banned_phrase.")), // part B attempt 1: banned phrase, still blocking
		resp(chapterJSONFromText(partBGoodText)),                           // part B attempt 2: succeeds
		resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 2
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	partACalls, partBCalls := 0, 0
	for _, call := range mainClient.Calls {
		if strings.Contains(call.Prompt, "part=1/2") {
			partACalls++
		}
		if strings.Contains(call.Prompt, "part=2/2") {
			partBCalls++
		}
	}
	if partACalls != 1 {
		t.Fatalf("expected part A to be generated exactly once (a part B failure shouldn't redo it), got %d calls", partACalls)
	}
	if partBCalls != 2 {
		t.Fatalf("expected part B to be retried once after its validation failure, got %d calls", partBCalls)
	}

	if !strings.Contains(script.Chapters[0].Text, partAText) {
		t.Fatalf("expected the final chapter to still contain part A's original (never regenerated) text, got: %s", script.Chapters[0].Text)
	}
	if !strings.Contains(script.Chapters[0].Text, partBGoodText) {
		t.Fatalf("expected the final chapter to contain part B's successful retry text, got: %s", script.Chapters[0].Text)
	}
}

func TestGenerateChapterPromptCarriesAvoidContextFromPriorChapters(t *testing.T) {
	twoChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "pivot", TargetWords: 40, Description: "middle"},
	}}

	// Chapter 1 uses "the ledger never once balanced" twice (at the
	// repetition limit) and mentions $1,200 once — chapter 2's prompt
	// should be warned about both before it's even generated.
	// The hook-identity sentence lives in its own paragraph after the
	// ledger text, not prepended to it — prepending would shift the
	// paragraph-start dedup (paragraphStartsUsed) onto the identity
	// sentence instead of "the ledger never once balanced", which is
	// what this test's avoidstart assertion below depends on.
	hookIdentity := "\n\nMy name is Dana Whitfield and I am forty-two years old. You will never run this place, she said without flinching."
	ch1Text := "The ledger never once balanced that spring. She kept one thousand two hundred dollars anyway. " +
		"Still the ledger never once balanced no matter how she tried." + hookIdentity
	ch1Display := "The ledger never once balanced that spring. She kept $1,200 anyway. " +
		"Still the ledger never once balanced no matter how she tried." + hookIdentity

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(mustChapterJSON(ch1Text, ch1Display)), resp("summary one"),
		resp(chapterJSON(40, 2)), resp("summary two"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), twoChapters)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Calls: bible(0), ch1(1), sum1(2), ch2(3), sum2(4) — chapter 2's
	// generation call is index 3.
	ch2Prompt := mainClient.Calls[3].Prompt
	if !strings.Contains(ch2Prompt, "the ledger never once balanced") {
		t.Fatalf("expected chapter 2's prompt to carry the at-limit phrase from chapter 1, got: %s", ch2Prompt)
	}
	if !strings.Contains(ch2Prompt, "$1,200") {
		t.Fatalf("expected chapter 2's prompt to carry the money-mention count from chapter 1, got: %s", ch2Prompt)
	}
}

// bibleWithAppearance mirrors goodBible() but sets the narrator appearance
// fields (Build/Hair/FaceNote) real bundle exports need — distinctive
// strings so a leak into a chapter-generation prompt is unambiguous.
func bibleWithAppearance() string {
	b, err := json.Marshal(bibleResponse{
		Title: "The Ledger Never Lies",
		Narrator: story.Person{
			Name: "Dana Whitfield", Age: 42, Role: "narrator", City: "Cedar Falls",
			Build: "sturdy", Hair: "silver hair cropped close", FaceNote: "a thin scar above the left eyebrow",
		},
		Cast:          []story.Person{{Name: "Russell Voss", Age: 61, Role: "antagonist"}},
		Timeline:      []story.Event{{Year: 1, What: "the humiliation"}, {Year: 8, What: "the reckoning"}},
		FamilyLaw:     "the numbers always add up eventually",
		RefrainPhrase: "I kept the ledger the way I kept everything",
		SeededLine:    "a receipt never lies",
		Numbers:       map[string]string{"amount": "twelve hundred dollars"},
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestChapterGenerationPromptNeverContainsNarratorAppearanceFields guards
// story.Bible.ForChapters(): Build/Hair/FaceNote exist only for the avatar
// module's portrait prompt (see manifest.json's narrator block) and must
// never reach a chapter-generation LLM call. prompts/chapter.tmpl doesn't
// name these fields today, so this can't catch a leak through the current
// template — it's a regression guard against a future template edit (or a
// future full-struct dump) reintroducing one; TestBibleForChaptersClears-
// OnlyAppearanceFields in internal/story is what actually exercises the
// clearing behavior.
func TestChapterGenerationPromptNeverContainsNarratorAppearanceFields(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
	}}

	mainClient := llm.NewFakeClient(resp(bibleWithAppearance()), resp(hookChapterJSON(40, 1)), resp("summary one"))
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Calls: bible(0), chapter 1 generation(1), summary(2).
	ch1Prompt := mainClient.Calls[1].Prompt
	for _, leaked := range []string{"sturdy", "silver hair cropped close", "a thin scar above the left eyebrow"} {
		if strings.Contains(ch1Prompt, leaked) {
			t.Fatalf("expected chapter prompt to never contain narrator appearance field %q, got: %s", leaked, ch1Prompt)
		}
	}
	// Sanity check the fixture actually round-tripped the appearance
	// fields into the generated script (so this test would fail loudly if
	// bibleWithAppearance() stopped doing what its name says).
	if script.Bible.Narrator.Build != "sturdy" {
		t.Fatalf("expected the generated script's Bible to still carry the full appearance fields, got Build=%q", script.Bible.Narrator.Build)
	}
}

func mustChapterJSON(text, display string) string {
	b, err := json.Marshal(chapterResponse{Text: text, DisplayText: display})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// recordAcceptedPriorScript saves and accepts a minimal script directly
// against st, bypassing the generation pipeline — the cheapest way to seed
// store-level cross-script history (used_phrases, used_names, ...) that a
// real prior Generate() run would have produced.
func recordAcceptedPriorScript(t *testing.T, st store.Store, id string, chapters []story.Chapter) {
	t.Helper()
	prior := &story.Script{ID: id, Status: story.StatusAccepted, Chapters: chapters}
	if err := st.SaveScript(context.Background(), prior); err != nil {
		t.Fatalf("SaveScript(%s): %v", id, err)
	}
	if err := st.RecordAcceptance(context.Background(), prior); err != nil {
		t.Fatalf("RecordAcceptance(%s): %v", id, err)
	}
}

func TestGenerateChapterPromptCarriesCrossScriptAvoidPhrases(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	st := store.NewMemoryStore()
	recordAcceptedPriorScript(t, st, "prior-script", []story.Chapter{
		{Index: 1, Beat: "hook", Text: "this is a signature phrase repeated across every single script we write"},
	})

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ch1Prompt := mainClient.Calls[1].Prompt
	if !strings.Contains(ch1Prompt, "this is a signature phrase repeated") {
		t.Fatalf("expected chapter 1's prompt to carry the prior script's phrase as a cross-script avoid-phrase, got: %s", ch1Prompt)
	}
}

// crossScriptSharedSentences are six distinct, exactly-six-word sentences
// — each contributes exactly one distinct six-word phrase. Used by both
// cross-script dedup tests below, embedded in the CURRENT script separated
// by distinct filler sentences (crossScriptFillers) so that only the
// within-sentence six-gram matches the prior script — story.
// SixGramOccurrences slides across a whole chapter's text regardless of
// sentence boundaries, so joining these sentences directly back-to-back
// (with no filler) would make every boundary-crossing window match the
// prior script too, since the prior script joins them the exact same way;
// that inflates "shared phrases" far past what the test intends.
var crossScriptSharedSentences = []string{
	"the ledger never once balanced correctly",
	"her hand shook slightly against wood",
	"the afternoon light cut across floor",
	"nobody said a single kind word",
	"the silence lasted longer than expected",
	"his voice dropped lower than usual",
}

// crossScriptFillerWords are five nonsense tokens per index, all globally
// unique (never repeated across indices, never appearing anywhere in
// crossScriptSharedSentences or the prior script's text). story.
// SixGramOccurrences slides a 6-word window across a chapter's whole text
// with no regard for sentence boundaries, so a plain prose filler risks a
// boundary-crossing window (the filler's last word + the shared
// sentence's first few, or vice versa) coincidentally matching a
// boundary-crossing window in the prior script's own text — five unique,
// never-elsewhere tokens on each side guarantee every window touching the
// filler contains at least one token found nowhere else, so only the exact
// aligned 6-word window (the shared sentence itself, with none of the
// filler in it) can ever match.
var crossScriptFillerWords = [][]string{
	{"zzalpha", "zzbravo", "zzcharlie", "zzdelta", "zzecho"},
	{"zzfoxtrot", "zzgolf", "zzhotel", "zzindia", "zzjuliet"},
	{"zzkilo", "zzlima", "zzmike", "zznovember", "zzoscar"},
	{"zzpapa", "zzquebec", "zzromeo", "zzsierra", "zztango"},
	{"zzuniform", "zzvictor", "zzwhiskey", "zzxray", "zzyankee"},
	{"zzzulu", "zzalphatwo", "zzbravotwo", "zzcharlietwo", "zzdeltatwo"},
}

// buildCrossScriptHookText embeds n of crossScriptSharedSentences into a
// valid hook chapter, each preceded by its own uniquely-worded filler so
// only the shared sentence itself (not its surrounding context) overlaps
// the prior script recorded via recordAcceptedPriorScript.
func buildCrossScriptHookText(n int) string {
	var sb strings.Builder
	sb.WriteString("My name is Dana Whitfield and I am forty-two years old. You will never run this place, she said without flinching.")
	for i := 0; i < n; i++ {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(crossScriptFillerWords[i], " "))
		sb.WriteString(". ")
		sb.WriteString(crossScriptSharedSentences[i])
		sb.WriteString(".")
	}
	return sb.String()
}

func TestGenerateCrossScriptDedupPointFixesChapterSharingTooManyPhrases(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	st := store.NewMemoryStore()
	recordAcceptedPriorScript(t, st, "prior-script", []story.Chapter{
		{Index: 1, Beat: "hook", Text: strings.Join(crossScriptSharedSentences, ". ") + "."},
	})

	hookText := buildCrossScriptHookText(6) // one over crossScriptPhraseOverlapThreshold (5)

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(hookText)), resp("summary one"),
		// Six point-fix rewrite calls, one per shared phrase (order follows
		// map iteration inside runCrossScriptDedup, but every canned
		// response here is phrase-free, so any order resolves cleanly).
		resp(chapterJSONFromText("The numbers were always somehow wrong.")),
		resp(chapterJSONFromText("Her fingers trembled against the table.")),
		resp(chapterJSONFromText("Late sun spilled over the boards.")),
		resp(chapterJSONFromText("Everyone stayed quiet that whole time.")),
		resp(chapterJSONFromText("Minutes stretched on far too long.")),
		resp(chapterJSONFromText("He spoke softer than she remembered.")),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	lower := strings.ToLower(script.Chapters[0].Text)
	for _, s := range crossScriptSharedSentences {
		if strings.Contains(lower, s) {
			t.Fatalf("expected shared phrase %q to be point-fixed away, got: %s", s, script.Chapters[0].Text)
		}
	}
	if script.Status != story.StatusAccepted {
		t.Fatalf("expected the script to end up accepted, got %q", script.Status)
	}
}

func TestGenerateCrossScriptDedupIgnoresOverlapAtOrBelowThreshold(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	st := store.NewMemoryStore()
	recordAcceptedPriorScript(t, st, "prior-script", []story.Chapter{
		{Index: 1, Beat: "hook", Text: strings.Join(crossScriptSharedSentences, ". ") + "."},
	})

	hookText := buildCrossScriptHookText(5) // exactly crossScriptPhraseOverlapThreshold — at the limit, not over it

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(hookText)), resp("summary one"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), oneChapter)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// bible + chapter + summary = 3 — no cross-script regeneration calls.
	if len(mainClient.Calls) != 3 {
		t.Fatalf("expected 3 calls (no cross-script fix at exactly the threshold), got %d: %+v", len(mainClient.Calls), mainClient.Calls)
	}
	for _, s := range crossScriptSharedSentences[:5] {
		if !strings.Contains(script.Chapters[0].Text, s) {
			t.Fatalf("expected shared phrase %q to survive untouched (at, not over, the threshold), got: %s", s, script.Chapters[0].Text)
		}
	}
}

func TestGenerateUsesPerBeatModelOverrideForPinnedChapters(t *testing.T) {
	twoChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening", Model: "gemini-3.6-flash"},
		{Index: 2, Beat: "pivot", TargetWords: 40, Description: "middle"}, // no override — uses the role default
	}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
		resp(chapterJSON(40, 2)), resp("summary two"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.GenerateModel = "gemini-3.5-flash-lite" // the role default, for the un-pinned chapter
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, twoChapters)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Calls: bible(0), ch1(1), sum1(2), ch2(3), sum2(4).
	ch1Opts := mainClient.Calls[1].Opts
	if ch1Opts.Model != "gemini-3.6-flash" || ch1Opts.ForceModel != "gemini-3.6-flash" {
		t.Fatalf("expected the pinned hook chapter to request gemini-3.6-flash with ForceModel set, got %+v", ch1Opts)
	}

	ch2Opts := mainClient.Calls[3].Opts
	if ch2Opts.Model != "gemini-3.5-flash-lite" || ch2Opts.ForceModel != "" {
		t.Fatalf("expected the un-pinned chapter to use the role default with no ForceModel, got %+v", ch2Opts)
	}
}

func TestGenerateContinuityCorrectsTitleAfterFullText(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("s1"),
		resp(chapterJSON(40, 2)), resp("s2"),
		resp(chapterJSON(40, 3)), resp("s3"),
	)
	continuityClient := llm.NewFakeClient(resp(`{"fixes": [], "title": "The Ledger Never Lies (Four Years Later)"}`))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Title != "The Ledger Never Lies (Four Years Later)" {
		t.Fatalf("expected continuity's corrected title to win, got %q", script.Title)
	}

	persisted, err := st.GetScript(context.Background(), script.ID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if persisted.Title != "The Ledger Never Lies (Four Years Later)" {
		t.Fatalf("expected the corrected title to be persisted, got %q", persisted.Title)
	}
}

func TestGenerateContinuityFixTriggersChapterRegeneration(t *testing.T) {
	twoChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "close", TargetWords: 40, Description: "ending"},
	}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
		resp(chapterJSON(40, 2)), resp("summary two"),
		resp(hookChapterJSON(40, 3)), resp("summary one fixed"), // regenerated chapter 1 + its summary
	)
	continuityClient := llm.NewFakeClient(resp(oneFixJSON(1)))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), twoChapters)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(script.Chapters) != 2 {
		t.Fatalf("expected 2 chapters (fix replaces, doesn't add), got %d", len(script.Chapters))
	}
	// bible(1) + ch1(1) + sum1(1) + ch2(1) + sum2(1) + ch1-refix(1) + sum1-refix(1) = 7
	if len(mainClient.Calls) != 7 {
		t.Fatalf("expected 7 calls to the main client, got %d", len(mainClient.Calls))
	}
	if !strings.Contains(mainClient.Calls[5].Prompt, "keep the name consistent") {
		t.Fatalf("expected the continuity-fix regeneration prompt to carry the fix instruction, got: %s", mainClient.Calls[5].Prompt)
	}

	// Cause tagging: the initial sequential pass (bible + both chapters +
	// both summaries) is CauseInitial; the continuity-triggered
	// regeneration of chapter 1 (+ its re-summary) is CauseRepair — same
	// role/provider/model, so this only shows up as two separate Usage
	// entries if RecordUsage's aggregation key actually includes cause.
	var genInitial, genRepair, sumInitial, sumRepair *story.UsageEntry
	for i := range script.Usage {
		u := &script.Usage[i]
		switch {
		case u.Role == "generate" && u.Cause == story.CauseInitial:
			genInitial = u
		case u.Role == "generate" && u.Cause == story.CauseRepair:
			genRepair = u
		case u.Role == "summary" && u.Cause == story.CauseInitial:
			sumInitial = u
		case u.Role == "summary" && u.Cause == story.CauseRepair:
			sumRepair = u
		}
	}
	if genInitial == nil || genInitial.Calls != 3 { // bible + ch1 + ch2
		t.Fatalf("expected 3 initial-cause generate calls (bible, ch1, ch2), got %+v", genInitial)
	}
	if genRepair == nil || genRepair.Calls != 1 { // continuity's ch1 regeneration
		t.Fatalf("expected 1 repair-cause generate call (continuity regeneration), got %+v", genRepair)
	}
	if sumInitial == nil || sumInitial.Calls != 2 { // ch1's and ch2's first summaries
		t.Fatalf("expected 2 initial-cause summary calls, got %+v", sumInitial)
	}
	if sumRepair == nil || sumRepair.Calls != 1 { // ch1's re-summary after the fix
		t.Fatalf("expected 1 repair-cause summary call, got %+v", sumRepair)
	}
}

func TestGenerateScriptValidationPointFixesRepeatedNGramInsteadOfRegeneratingChapter(t *testing.T) {
	twoChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "close", TargetWords: 40, Description: "ending"},
	}}

	// The phrase sits inside otherwise-different sentences (not identical
	// whole sentences), so the deterministic dedup pass leaves it alone
	// and the repeated_ngram violation reaches the point-fix path. Chapter
	// 2 holds the phrase twice (in two different sentences) on top of
	// chapter 1's one use, so its second occurrence is the excess one.
	phrase := "the ledger never once balanced correctly"
	ch1Text := "My name is Dana Whitfield and I am forty-two years old. You will never run this place, she said without flinching. " +
		fillerText(30, 11) + " Somehow " + phrase + " no matter how hard she tried."
	ch2Text := fillerText(20, 12) + " Even twice, " + phrase + " despite every audit she ran. " +
		fillerText(10, 15) + " Still, " + phrase + " every single year."
	pointFixedSentence := "Even twice, the numbers refused to add up despite every audit she ran."

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(ch1Text)), resp("summary one"),
		resp(chapterJSONFromText(ch2Text)), resp("summary two"),
		resp(chapterJSONFromText(pointFixedSentence)), // the one cheap point-fix rewrite call
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), twoChapters)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Status != story.StatusAccepted {
		t.Fatalf("expected acceptance after the repeated phrase was point-fixed, got %q", script.Status)
	}
	if script.Chapters[1].Summary != "summary two" {
		t.Fatalf("expected chapter 2's summary to be untouched — a point-fix doesn't regenerate the chapter or its summary, got %q", script.Chapters[1].Summary)
	}
	// bible + (ch1 + sum1) + (ch2 + sum2) + 1 point-fix call = 6 — no extra
	// full chapter regeneration call.
	if len(mainClient.Calls) != 6 {
		t.Fatalf("expected exactly 6 calls (no full chapter regeneration), got %d: %+v", len(mainClient.Calls), mainClient.Calls)
	}
}

func TestGenerateScriptValidationFullyRegeneratesChapterWhenTooManyViolations(t *testing.T) {
	twoChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "close", TargetWords: 40, Description: "ending"},
	}}

	// Nine distinct 6-word phrases, each wrapped in 3 differently-worded
	// sentences (so the deterministic dedup pass can't collapse any of
	// them as exact duplicates) — every phrase's 3rd occurrence exceeds
	// the repeated_ngram limit of 2, so chapter 2 alone racks up 9
	// separate violations, one more than maxViolationsForPointFix (8),
	// forcing a full regeneration instead of nine point-fix calls.
	numberWords := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight"}
	wrappers := []string{"She said %s before dinner.", "He noted %s during the meeting.", "They wrote %s in the ledger."}
	var sb strings.Builder
	for _, w := range numberWords {
		phrase := fmt.Sprintf("phrase number %s repeats twice already", w)
		for _, wr := range wrappers {
			sb.WriteString(fmt.Sprintf(wr, phrase))
			sb.WriteString(" ")
		}
	}
	ch1Text := "My name is Dana Whitfield and I am forty-two years old. You will never run this place, she said without flinching. " + fillerText(40, 19)
	ch2BadText := fillerText(10, 20) + " " + sb.String()
	ch2GoodText := fillerText(40, 21)

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(chapterJSONFromText(ch1Text)), resp("summary one"),
		resp(chapterJSONFromText(ch2BadText)), resp("summary two"),
		resp(chapterJSONFromText(ch2GoodText)), resp("summary two fixed"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), twoChapters)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Chapters[1].Summary != "summary two fixed" {
		t.Fatalf("expected chapter 2 to be fully regenerated (>8 violations exceeds the point-fix threshold), got %q", script.Chapters[1].Summary)
	}
}

func TestGenerateContinuityFixPersistsEarlierFixesEvenIfALaterOneFails(t *testing.T) {
	threeChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "pivot", TargetWords: 40, Description: "middle"},
		{Index: 3, Beat: "close", TargetWords: 40, Description: "ending"},
	}}

	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
		resp(chapterJSON(40, 2)), resp("summary two"),
		resp(chapterJSON(40, 3)), resp("summary three"),
		resp(hookChapterJSON(40, 99)), resp("summary one fixed"), // fixing chapter 1 succeeds
	).WithError(errors.New("429: rate limited, retries exhausted")) // fixing chapter 2 then fails

	continuityClient := llm.NewFakeClient(resp(`{"fixes": [
		{"chapter_index": 1, "issue": "x", "instruction": "y"},
		{"chapter_index": 2, "issue": "x", "instruction": "y"}
	]}`))
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), threeChapters)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err == nil {
		t.Fatalf("expected an error when fixing chapter 2 fails")
	}

	pending, lerr := st.ListScripts(context.Background(), store.ListFilter{Status: story.StatusPending, Limit: 1})
	if lerr != nil || len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending script, got %v (err=%v)", pending, lerr)
	}
	saved, gerr := st.GetScript(context.Background(), pending[0].ID)
	if gerr != nil {
		t.Fatalf("GetScript: %v", gerr)
	}
	if saved.Chapters[0].Summary != "summary one fixed" {
		t.Fatalf("expected chapter 1's continuity fix to be persisted even though chapter 2's fix later failed, got %+v", saved.Chapters[0])
	}
}

func TestGenerateContinuityTooManyFixesReturnsError(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("s1"),
		resp(chapterJSON(40, 2)), resp("s2"),
		resp(chapterJSON(40, 3)), resp("s3"),
	)
	continuityClient := llm.NewFakeClient(resp(manyFixesJSON(6)))
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxContinuityFixes = 5
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, testChapters())

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if !errors.Is(err, continuity.ErrTooManyFixes) {
		t.Fatalf("expected continuity.ErrTooManyFixes, got %v", err)
	}
}

func TestGenerateReviewBelowThresholdRegeneratesWeakChapterThenAccepts(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("s1"),
		resp(chapterJSON(40, 2)), resp("s2"),
		resp(chapterJSON(40, 3)), resp("s3"),
		resp(hookChapterJSON(41, 4)), resp("s1 fixed"), // weak-chapter regeneration of chapter 1
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(lowReviewJSON(1)), resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxReviewRetries = 1
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Status != story.StatusAccepted {
		t.Fatalf("expected eventual acceptance, got %q", script.Status)
	}
	if len(reviewClient.Calls) != 2 {
		t.Fatalf("expected 2 review calls (fail then pass), got %d", len(reviewClient.Calls))
	}
	if len(script.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(script.Chapters))
	}
}

func TestGenerateReviewExhaustsRetriesEndsRejectedButSaved(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("s1"),
		resp(chapterJSON(40, 2)), resp("s2"),
		resp(chapterJSON(40, 3)), resp("s3"),
		resp(hookChapterJSON(41, 4)), resp("s1 retry"), // one weak-chapter regeneration round
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(lowReviewJSON(1)), resp(lowReviewJSON(1)))
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxReviewRetries = 1
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if script.Status != story.StatusRejected {
		t.Fatalf("expected rejected status, got %q", script.Status)
	}

	got, err := st.GetScript(context.Background(), script.ID)
	if err != nil {
		t.Fatalf("expected a rejected script to still be saved, GetScript: %v", err)
	}
	if got.Status != story.StatusRejected {
		t.Fatalf("expected persisted status rejected, got %q", got.Status)
	}

	names, err := st.RecentUsedNames(context.Background(), 30)
	if err != nil {
		t.Fatalf("RecentUsedNames: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected a rejected script to not record acceptance content, got %v", names)
	}
}

func TestOrchestratorRegenerateChapterUpdatesOnlyThatChapter(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("s1"),
		resp(chapterJSON(40, 2)), resp("s2"),
		resp(chapterJSON(40, 3)), resp("s3"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	originalCh2Text := script.Chapters[1].Text

	regenClient := llm.NewFakeClient(resp(hookChapterJSON(40, 99)), resp("regenerated summary"))
	o2 := newTestOrchestrator(t, regenClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	updated, err := o2.RegenerateChapter(context.Background(), script.ID, 1)
	if err != nil {
		t.Fatalf("RegenerateChapter: %v", err)
	}
	if updated.Chapters[0].Summary != "regenerated summary" {
		t.Fatalf("expected chapter 1's summary to be replaced, got %+v", updated.Chapters[0])
	}
	if updated.Chapters[1].Text != originalCh2Text {
		t.Fatalf("expected chapter 2 to be untouched by a chapter-1 regeneration")
	}

	persisted, err := st.GetScript(context.Background(), script.ID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if persisted.Chapters[0].Summary != "regenerated summary" {
		t.Fatalf("expected the regeneration to be persisted, got %+v", persisted.Chapters[0])
	}
}

func TestOrchestratorRegenerateChapterUnknownScriptReturnsError(t *testing.T) {
	mainClient := llm.NewFakeClient()
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	_, err := o.RegenerateChapter(context.Background(), "missing", 1)
	if err == nil {
		t.Fatalf("expected an error for an unknown script id")
	}
}

func TestOrchestratorRegenerateChapterUnknownIndexReturnsError(t *testing.T) {
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("s1"),
		resp(chapterJSON(40, 2)), resp("s2"),
		resp(chapterJSON(40, 3)), resp("s3"),
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())
	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, err := o.RegenerateChapter(context.Background(), script.ID, 99); err == nil {
		t.Fatalf("expected an error for an unknown chapter index")
	}
}

func TestGenerateSavesProgressAfterEachChapterAndResumeContinues(t *testing.T) {
	twoChapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"},
		{Index: 2, Beat: "close", TargetWords: 40, Description: "ending"},
	}}

	// First run: chapter 1 succeeds, then chapter 2's LLM call fails outright
	// (simulating a rate limit or provider outage mid-script).
	mainClient := llm.NewFakeClient(
		resp(goodBible()),
		resp(hookChapterJSON(40, 1)), resp("summary one"),
	).WithError(errors.New("network blip"))
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), twoChapters)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err == nil {
		t.Fatalf("expected an error when chapter 2's LLM call fails")
	}

	pending, err := st.ListScripts(context.Background(), store.ListFilter{Status: story.StatusPending, Limit: 1})
	if err != nil {
		t.Fatalf("ListScripts: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending script, got %d", len(pending))
	}
	scriptID := pending[0].ID

	saved, err := st.GetScript(context.Background(), scriptID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if len(saved.Chapters) != 1 || saved.Chapters[0].Index != 1 {
		t.Fatalf("expected chapter 1 to already be persisted after the chapter-2 failure, got %+v", saved.Chapters)
	}

	// Resume with a fresh client that only needs to produce chapter 2 (+
	// its summary), then continuity + review.
	resumeClient := llm.NewFakeClient(resp(chapterJSON(40, 2)), resp("summary two"))
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient(resp(passingReviewJSON()))
	o2 := newTestOrchestrator(t, resumeClient, continuityClient, reviewClient, st, testSettings(), twoChapters)

	final, err := o2.Resume(context.Background(), scriptID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(final.Chapters) != 2 {
		t.Fatalf("expected 2 chapters after resume, got %d", len(final.Chapters))
	}
	if final.Chapters[0].Summary != "summary one" {
		t.Fatalf("expected chapter 1 to be untouched by resume, got %+v", final.Chapters[0])
	}
	if final.Chapters[1].Summary != "summary two" {
		t.Fatalf("expected chapter 2 to be freshly generated by resume, got %+v", final.Chapters[1])
	}
	if final.Status != story.StatusAccepted {
		t.Fatalf("expected acceptance after resume, got %q", final.Status)
	}
	if len(resumeClient.Calls) != 2 {
		t.Fatalf("expected resume to only call the LLM for the missing chapter (2 calls: text+summary), got %d", len(resumeClient.Calls))
	}
}

func TestOrchestratorResumeRejectsNonPendingScript(t *testing.T) {
	st := store.NewMemoryStore()
	script := &story.Script{ID: "s1", Status: story.StatusAccepted, Seed: story.Seed{Profession: "nurse"}}
	if err := st.SaveScript(context.Background(), script); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	o := newTestOrchestrator(t, llm.NewFakeClient(), llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), testChapters())
	if _, err := o.Resume(context.Background(), "s1"); err == nil {
		t.Fatalf("expected an error resuming a non-pending (accepted) script")
	}
}

func TestOrchestratorResumeUnknownScriptReturnsError(t *testing.T) {
	st := store.NewMemoryStore()
	o := newTestOrchestrator(t, llm.NewFakeClient(), llm.NewFakeClient(), llm.NewFakeClient(), st, testSettings(), testChapters())
	if _, err := o.Resume(context.Background(), "missing"); err == nil {
		t.Fatalf("expected an error resuming an unknown script")
	}
}

func TestGenerateUnknownProfessionReturnsErrorImmediately(t *testing.T) {
	mainClient := llm.NewFakeClient()
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, testSettings(), testChapters())

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{Profession: "does_not_exist"})
	if err == nil {
		t.Fatalf("expected an error for an unknown profession")
	}
	if len(mainClient.Calls) != 0 {
		t.Fatalf("expected no LLM calls before the seed draw fails, got %d", len(mainClient.Calls))
	}
}

// TestGenerateRefusesASecondChapterAttemptOnceAlreadyOverBudget proves the
// cost check moved INSIDE each retry loop, not just between chapters: the
// first chapter attempt alone already crosses max_cost_usd, so the second
// (retry) attempt must never be sent at all — a stricter guarantee than
// only checking after a whole chapter finishes, which would let a single
// stubborn chapter's retry loop keep spending unchecked.
func TestGenerateRefusesASecondChapterAttemptOnceAlreadyOverBudget(t *testing.T) {
	oneChapter := config.Chapters{Chapters: []config.ChapterSpec{{Index: 1, Beat: "hook", TargetWords: 40, Description: "opening"}}}

	genResp := func(text string, tokensOut int) llm.Response {
		return llm.Response{Text: text, Provider: "test-provider", Model: "gen-model", TokensOut: tokensOut}
	}
	mainClient := llm.NewFakeClient(
		genResp(goodBible(), 1),
		genResp(chapterJSONFromText(fillerText(40, 1)+" zzz_banned_phrase."), 5), // fails validation, would normally retry
	)
	continuityClient := llm.NewFakeClient()
	reviewClient := llm.NewFakeClient()
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxChapterRetries = 2
	settings.MaxCostUSD = 5.0 // bible($1) + attempt 1($5) = $6, already over before attempt 2
	pricing := config.Pricing{Models: map[string]config.ModelPricing{
		"gen-model": {InputPerMillion: 0, OutputPerMillion: 1_000_000},
	}}
	o := newTestOrchestratorWithPricing(t, mainClient, continuityClient, reviewClient, st, settings, oneChapter, pricing)

	_, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if !errors.Is(err, ErrCostLimitExceeded) {
		t.Fatalf("expected ErrCostLimitExceeded, got %v", err)
	}
	if len(mainClient.Calls) != 2 {
		t.Fatalf("expected exactly 2 calls (bible + the one over-budget chapter attempt, no retry), got %d", len(mainClient.Calls))
	}
}

// TestRunScriptValidationGivesUpAfterMaxRoundsReturningSentinel builds a
// script whose chapter 6 is missing the refrain phrase entirely — an
// unlocatable violation (no Phrase to point-fix, see
// ValidateRefrainPhrasePlacement's "doesn't appear at all" case), so every
// round falls straight to a full chapter regeneration. The regenerated
// text queued here never includes the phrase either, so it can never
// converge — exactly the "still failing after N rounds" case
// ErrValidationRoundsExhausted exists for.
func TestRunScriptValidationGivesUpAfterMaxRoundsReturningSentinel(t *testing.T) {
	chapters := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 6, Beat: "pivot", TargetWords: 40, Description: "plant"},
	}}

	// Each round's full regeneration is 2 calls (chapter body, then its
	// summary) — 2 rounds needs 4 queued responses.
	regenBody := resp(chapterJSONFromText(fillerText(40, 1)))
	regenSummary := resp("summary")
	mainClient := llm.NewFakeClient(regenBody, regenSummary, regenBody, regenSummary)
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxRepetitionRounds = 2
	o := newTestOrchestrator(t, mainClient, llm.NewFakeClient(), llm.NewFakeClient(), st, settings, chapters)

	script := &story.Script{
		ID: "s1", Status: story.StatusPending,
		Bible: story.Bible{RefrainPhrase: "a receipt never lies"},
		Chapters: []story.Chapter{
			{Index: 6, Beat: "pivot", TargetWords: 40, Text: fillerText(40, 0)}, // no refrain phrase anywhere
		},
	}

	err := o.runScriptValidation(context.Background(), script)
	if !errors.Is(err, ErrValidationRoundsExhausted) {
		t.Fatalf("expected ErrValidationRoundsExhausted, got %v", err)
	}
	if len(mainClient.Calls) != 4 {
		t.Fatalf("expected exactly 4 calls (2 rounds x [regenerate + summary]), got %d", len(mainClient.Calls))
	}
}

// TestGenerateAcceptsWithWarningsWhenValidationRoundsExhausted drives the
// same non-convergent scenario through the FULL pipeline (Generate, not
// just runScriptValidation directly) to verify finishPipeline's handling:
// the script ends up StatusAcceptedWithWarnings (Generate returns no error
// — RecordAcceptance ran cleanly too, since a failure there would have
// surfaced as one), and — the actual cost-saving part — cross-script
// dedup and review never run at all, since there's a real known problem
// this script needs eyes on before either would be money well spent.
func TestGenerateAcceptsWithWarningsWhenValidationRoundsExhausted(t *testing.T) {
	oneChapterAtIndexSix := config.Chapters{Chapters: []config.ChapterSpec{
		{Index: 6, Beat: "pivot", TargetWords: 40, Description: "plant"},
	}}

	bibleNoRefrainInChapter := func() string {
		b, err := json.Marshal(bibleResponse{
			Title:     "T",
			Narrator:  story.Person{Name: "Dana Whitfield", Age: 42, Role: "narrator", City: "Cedar Falls"},
			FamilyLaw: "law", RefrainPhrase: "a receipt never lies", SeededLine: "seed",
			Numbers: map[string]string{},
		})
		if err != nil {
			panic(err)
		}
		return string(b)
	}

	// chapter 6's initial generation (never contains the refrain phrase)
	// + its summary, then 2 rounds of [regenerate + summary] that also
	// never contain it.
	ch6NoRefrain := resp(chapterJSONFromText(fillerText(40, 1)))
	summary := resp("summary")
	mainClient := llm.NewFakeClient(
		resp(bibleNoRefrainInChapter()),
		ch6NoRefrain, summary, // initial chapter 6
		ch6NoRefrain, summary, // round 1 regeneration
		ch6NoRefrain, summary, // round 2 regeneration
	)
	continuityClient := llm.NewFakeClient(resp(noFixesJSON()))
	reviewClient := llm.NewFakeClient() // must never be called
	st := store.NewMemoryStore()

	settings := testSettings()
	settings.MaxRepetitionRounds = 2
	o := newTestOrchestrator(t, mainClient, continuityClient, reviewClient, st, settings, oneChapterAtIndexSix)

	script, err := o.Generate(context.Background(), rand.New(rand.NewSource(1)), Options{})
	if err != nil {
		t.Fatalf("Generate: %v (expected accepted-with-warnings, not an error)", err)
	}
	if script.Status != story.StatusAcceptedWithWarnings {
		t.Fatalf("expected status %q, got %q", story.StatusAcceptedWithWarnings, script.Status)
	}
	if len(reviewClient.Calls) != 0 {
		t.Fatalf("expected review to never run once validation rounds were exhausted, got %d calls", len(reviewClient.Calls))
	}

	saved, err := st.GetScript(context.Background(), script.ID)
	if err != nil {
		t.Fatalf("GetScript: %v", err)
	}
	if saved.Status != story.StatusAcceptedWithWarnings {
		t.Fatalf("expected the saved script's status to also be %q, got %q", story.StatusAcceptedWithWarnings, saved.Status)
	}
}
