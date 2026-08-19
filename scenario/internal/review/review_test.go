package review

import (
	"context"
	"errors"
	"strings"
	"testing"
	"text/template"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("review").Parse(
		"Profession: {{.Seed.Profession}}\nNarrator: {{.Bible.Narrator.Name}}\n---\n{{.FullText}}\n",
	)
	if err != nil {
		t.Fatalf("parse test template: %v", err)
	}
	return tmpl
}

func testScript() *story.Script {
	return &story.Script{
		Seed:  story.Seed{Profession: "nurse"},
		Bible: story.Bible{Narrator: story.Person{Name: "Dana Whitfield"}},
		Chapters: []story.Chapter{
			{Index: 1, Beat: "hook", Text: "I kept the ledger the way I kept everything: exact."},
			{Index: 14, Beat: "the_refusal", Text: "I put the folder down and did not press charges."},
		},
	}
}

func highScores() story.QualityScores {
	return story.QualityScores{
		HookStrength: 8, ProfessionCausality: 8, Restraint: 7.5, SceneNotSummary: 8,
		PlantingPayoff: 7, RefusalPresent: 9, AISmell: 8, Comment: "solid",
	}
}

func TestReviewPopulatesUsageFromTheUnderlyingCall(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{
		Text:     `{"scores": {"hook_strength": 8, "profession_causality": 8, "restraint": 8, "scene_not_summary": 8, "planting_payoff": 8, "refusal_present": 8, "ai_smell": 8, "comment": "x"}, "weak_chapters": []}`,
		Provider: "google-ai-studio", Model: "gemini-3.5-flash-lite", TokensIn: 900, TokensOut: 30, ThinkingTokens: 0,
	})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	result, err := reviewer.Review(context.Background(), testScript())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if result.Usage.Provider != "google-ai-studio" || result.Usage.Model != "gemini-3.5-flash-lite" {
		t.Fatalf("expected Usage to carry provider/model, got %+v", result.Usage)
	}
	if result.Usage.TokensIn != 900 || result.Usage.TokensOut != 30 {
		t.Fatalf("expected Usage to carry token counts, got %+v", result.Usage)
	}
}

func TestReviewSendsReviewRole(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"scores": {
		"hook_strength": 8, "profession_causality": 8, "restraint": 8,
		"scene_not_summary": 8, "planting_payoff": 8, "refusal_present": 8,
		"ai_smell": 8, "comment": "x"
	}, "weak_chapters": []}`})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	if _, err := reviewer.Review(context.Background(), testScript()); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if client.Calls[0].Opts.Role != llm.RoleReview {
		t.Fatalf("expected Role to be RoleReview, got %q", client.Calls[0].Opts.Role)
	}
}

func TestReviewParsesScores(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"scores": {
		"hook_strength": 8, "profession_causality": 7.5, "restraint": 8,
		"scene_not_summary": 7, "planting_payoff": 6.5, "refusal_present": 9,
		"ai_smell": 8, "comment": "strong hook, clean refusal"
	}, "weak_chapters": []}`})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	result, err := reviewer.Review(context.Background(), testScript())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if result.Scores.HookStrength != 8 || result.Scores.RefusalPresent != 9 {
		t.Fatalf("unexpected scores: %+v", result.Scores)
	}
	if result.Scores.Comment != "strong hook, clean refusal" {
		t.Fatalf("unexpected comment: %q", result.Scores.Comment)
	}
}

func TestReviewParsesWeakChapters(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"scores": {
		"hook_strength": 4, "profession_causality": 7, "restraint": 7,
		"scene_not_summary": 6, "planting_payoff": 6, "refusal_present": 8,
		"ai_smell": 7, "comment": "weak hook"
	}, "weak_chapters": [
		{"index": 1, "axis": "hook_strength", "reason": "opens with backstory instead of the inciting scene"}
	]}`})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	result, err := reviewer.Review(context.Background(), testScript())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(result.WeakChapters) != 1 || result.WeakChapters[0].Index != 1 {
		t.Fatalf("unexpected weak chapters: %+v", result.WeakChapters)
	}
}

func TestReviewStripsFencesAndPreamble(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{
		Text: "Here are the scores:\n```json\n{\"scores\": {" +
			"\"hook_strength\": 8, \"profession_causality\": 8, \"restraint\": 8, " +
			"\"scene_not_summary\": 8, \"planting_payoff\": 8, \"refusal_present\": 8, " +
			"\"ai_smell\": 8, \"comment\": \"ok\"}, \"weak_chapters\": []}\n```\nHope that helps!",
	})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	result, err := reviewer.Review(context.Background(), testScript())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if result.Scores.Mean() != 8 {
		t.Fatalf("expected mean 8, got %v", result.Scores.Mean())
	}
}

func TestReviewRejectsOutOfRangeScore(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"scores": {
		"hook_strength": 15, "profession_causality": 8, "restraint": 8,
		"scene_not_summary": 8, "planting_payoff": 8, "refusal_present": 8,
		"ai_smell": 8, "comment": "x"
	}, "weak_chapters": []}`})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	if _, err := reviewer.Review(context.Background(), testScript()); err == nil {
		t.Fatalf("expected an error for an out-of-range score")
	}
}

func TestReviewRejectsNegativeScore(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"scores": {
		"hook_strength": 8, "profession_causality": 8, "restraint": -1,
		"scene_not_summary": 8, "planting_payoff": 8, "refusal_present": 8,
		"ai_smell": 8, "comment": "x"
	}, "weak_chapters": []}`})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	if _, err := reviewer.Review(context.Background(), testScript()); err == nil {
		t.Fatalf("expected an error for a negative score")
	}
}

func TestReviewPropagatesLLMError(t *testing.T) {
	client := llm.NewFakeClient().WithError(errors.New("provider unavailable"))
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	_, err := reviewer.Review(context.Background(), testScript())
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected the underlying LLM error to be wrapped, got %v", err)
	}
}

func TestReviewInvalidJSONReturnsError(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: "no scores here"})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	if _, err := reviewer.Review(context.Background(), testScript()); err == nil {
		t.Fatalf("expected an error when no JSON object is present")
	}
}

func TestReviewRendersSeedAndBibleAndFullTextIntoPrompt(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"scores": {
		"hook_strength": 8, "profession_causality": 8, "restraint": 8,
		"scene_not_summary": 8, "planting_payoff": 8, "refusal_present": 8,
		"ai_smell": 8, "comment": "x"
	}, "weak_chapters": []}`})
	reviewer := NewReviewer(client, testTemplate(t), "test-model")

	if _, err := reviewer.Review(context.Background(), testScript()); err != nil {
		t.Fatalf("Review: %v", err)
	}

	if len(client.Calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(client.Calls))
	}
	prompt := client.Calls[0].Prompt
	if !strings.Contains(prompt, "nurse") {
		t.Fatalf("expected prompt to contain the profession, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Dana Whitfield") {
		t.Fatalf("expected prompt to contain the narrator's name, got: %s", prompt)
	}
	if !strings.Contains(prompt, "did not press charges") {
		t.Fatalf("expected prompt to contain chapter 14's text, got: %s", prompt)
	}
	if client.Calls[0].Opts.Model != "test-model" {
		t.Fatalf("expected model to be passed through, got %q", client.Calls[0].Opts.Model)
	}
}

func TestResultPassesRequiresMeanAndMinThreshold(t *testing.T) {
	threshold := config.QualityThreshold{MeanMin: 7.0, AxisMin: 5.0}

	passing := Result{Scores: highScores()}
	if !passing.Passes(threshold) {
		t.Fatalf("expected high scores to pass: mean=%v min=%v", passing.Scores.Mean(), passing.Scores.Min())
	}

	lowMean := Result{Scores: story.QualityScores{
		HookStrength: 5, ProfessionCausality: 5, Restraint: 5, SceneNotSummary: 5,
		PlantingPayoff: 5, RefusalPresent: 5, AISmell: 5,
	}}
	if lowMean.Passes(threshold) {
		t.Fatalf("expected mean-of-5 scores to fail a mean_min of 7.0")
	}

	oneWeakAxis := Result{Scores: story.QualityScores{
		HookStrength: 9, ProfessionCausality: 9, Restraint: 9, SceneNotSummary: 9,
		PlantingPayoff: 9, RefusalPresent: 9, AISmell: 3, // below AxisMin despite a high mean
	}}
	if oneWeakAxis.Passes(threshold) {
		t.Fatalf("expected a single axis below axis_min to fail even with a high mean")
	}
}
