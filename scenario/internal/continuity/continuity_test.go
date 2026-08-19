package continuity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"text/template"

	"github.com/placeholder/scenario/internal/llm"
	"github.com/placeholder/scenario/internal/story"
)

func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("continuity").Parse(
		"Narrator: {{.Bible.Narrator.Name}}\nRefrain: {{.Bible.RefrainPhrase}}\nCurrent title: {{.CurrentTitle}}\n---\n{{.FullText}}\n",
	)
	if err != nil {
		t.Fatalf("parse test template: %v", err)
	}
	return tmpl
}

func testBible() story.Bible {
	return story.Bible{
		Narrator:      story.Person{Name: "Dana Whitfield", Role: "narrator"},
		RefrainPhrase: "the numbers always add up eventually",
	}
}

// testScript's Summary fields deliberately echo the same key phrases as
// Text — Check defaults to SummaryText (fullContext false), so tests that
// assert on prompt content need those phrases reachable either way without
// special-casing every call site for which text source it's exercising.
func testScript() *story.Script {
	return &story.Script{
		Title: "Seven Years of Silence",
		Bible: testBible(),
		Chapters: []story.Chapter{
			{Index: 1, Beat: "hook", Text: "I kept the ledger the way I kept everything: exact.", Summary: "I kept the ledger exact."},
			{Index: 2, Beat: "pivot", Text: "Three years later the same handwriting appeared on a different form.", Summary: "Three years later, a different form appeared."},
		},
	}
}

func TestCheckParsesFixList(t *testing.T) {
	resp := llm.Response{Text: `{"fixes": [
		{"chapter_index": 5, "issue": "narrator's town changes from Cedar Falls to Millbrook", "instruction": "keep every mention of the town as Cedar Falls"},
		{"chapter_index": 12, "issue": "the antagonist's age shifts from 61 to 58 with no explanation", "instruction": "keep the antagonist's age consistent at 61"}
	]}`}
	client := llm.NewFakeClient(resp)
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(report.Fixes) != 2 {
		t.Fatalf("expected 2 fixes, got %d", len(report.Fixes))
	}
	if report.Fixes[0].ChapterIndex != 5 || report.Fixes[1].ChapterIndex != 12 {
		t.Fatalf("unexpected chapter indexes: %+v", report.Fixes)
	}
}

func TestCheckPopulatesUsageFromTheUnderlyingCall(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{
		Text: `{"fixes": []}`, Provider: "google-ai-studio", Model: "gemini-3.6-flash",
		TokensIn: 500, TokensOut: 120, ThinkingTokens: 80,
	})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Usage.Provider != "google-ai-studio" || report.Usage.Model != "gemini-3.6-flash" {
		t.Fatalf("expected Usage to carry provider/model, got %+v", report.Usage)
	}
	if report.Usage.TokensIn != 500 || report.Usage.TokensOut != 120 || report.Usage.ThinkingTokens != 80 {
		t.Fatalf("expected Usage to carry token counts, got %+v", report.Usage)
	}
}

func TestCheckSendsContinuityRole(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	if _, err := checker.Check(context.Background(), testBible(), testScript()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if client.Calls[0].Opts.Role != llm.RoleContinuity {
		t.Fatalf("expected Role to be RoleContinuity, got %q", client.Calls[0].Opts.Role)
	}
}

func TestCheckNoFixesReturnsEmptyReport(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(report.Fixes) != 0 {
		t.Fatalf("expected no fixes, got %+v", report.Fixes)
	}
}

func TestCheckStripsFencesAndPreamble(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{
		Text: "Here is the continuity report:\n```json\n{\"fixes\": [{\"chapter_index\": 3, \"issue\": \"x\", \"instruction\": \"y\"}]}\n```\nLet me know if you need more detail.",
	})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(report.Fixes) != 1 || report.Fixes[0].ChapterIndex != 3 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestCheckTooManyFixesReturnsErrorButKeepsReport(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": [
		{"chapter_index": 1, "issue": "a", "instruction": "a"},
		{"chapter_index": 2, "issue": "b", "instruction": "b"},
		{"chapter_index": 3, "issue": "c", "instruction": "c"}
	]}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 2, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if !errors.Is(err, ErrTooManyFixes) {
		t.Fatalf("expected ErrTooManyFixes, got %v", err)
	}
	if len(report.Fixes) != 3 {
		t.Fatalf("expected the parsed report to still be returned, got %+v", report)
	}
}

func TestCheckDefaultsMaxFixesToFive(t *testing.T) {
	fixes := `{"fixes": [
		{"chapter_index": 1, "issue": "a", "instruction": "a"},
		{"chapter_index": 2, "issue": "b", "instruction": "b"},
		{"chapter_index": 3, "issue": "c", "instruction": "c"},
		{"chapter_index": 4, "issue": "d", "instruction": "d"},
		{"chapter_index": 5, "issue": "e", "instruction": "e"}
	]}`
	client := llm.NewFakeClient(llm.Response{Text: fixes})
	checker := NewChecker(client, testTemplate(t), "test-model", 0, false) // 0 -> default 5

	_, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("expected exactly 5 fixes to be within the default limit, got error: %v", err)
	}
}

func TestCheckPropagatesLLMError(t *testing.T) {
	client := llm.NewFakeClient().WithError(errors.New("provider unavailable"))
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	_, err := checker.Check(context.Background(), testBible(), testScript())
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected the underlying LLM error to be wrapped, got %v", err)
	}
}

func TestCheckInvalidJSONReturnsError(t *testing.T) {
	bad := llm.Response{Text: "I don't see any continuity problems."}
	client := llm.NewFakeClient(bad, bad, bad) // no JSON at all — retries exhaust, never succeed
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	if _, err := checker.Check(context.Background(), testBible(), testScript()); err == nil {
		t.Fatalf("expected an error when the response has no JSON object")
	}
}

func TestCheckRetriesWithLargerBudgetOnTruncatedJSON(t *testing.T) {
	truncated := llm.Response{Text: `{"fixes": [{"chapter_index": 3, "issue": "cut off mid`, TokensOut: 3000}
	good := llm.Response{Text: `{"fixes": []}`, TokensOut: 50}
	client := llm.NewFakeClient(truncated, good)
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(client.Calls) != 2 {
		t.Fatalf("expected exactly 2 calls (1 truncated, 1 retry with a bigger budget), got %d", len(client.Calls))
	}
	if client.Calls[1].Opts.MaxTokens <= client.Calls[0].Opts.MaxTokens {
		t.Fatalf("expected the retry's MaxTokens to be larger than the first attempt's, got first=%d retry=%d",
			client.Calls[0].Opts.MaxTokens, client.Calls[1].Opts.MaxTokens)
	}
	// Both attempts' tokens should be folded into Usage, not just the
	// successful one's — the truncated attempt still cost real money.
	if report.Usage.TokensOut != 3050 {
		t.Fatalf("expected combined TokensOut of 3050 (both attempts), got %d", report.Usage.TokensOut)
	}
}

func TestCheckExhaustsRetriesOnRepeatedTruncationButKeepsCombinedUsage(t *testing.T) {
	truncated := llm.Response{Text: `{"fixes": [{"chapter_index": 3`, TokensOut: 1000}
	client := llm.NewFakeClient(truncated, truncated, truncated)
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err == nil {
		t.Fatalf("expected an error after all retries stay truncated")
	}
	if len(client.Calls) != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", len(client.Calls))
	}
	if report.Usage.TokensOut != 3000 {
		t.Fatalf("expected all 3 attempts' tokens folded into Usage even on ultimate failure, got %d", report.Usage.TokensOut)
	}
}

func TestCheckMalformedJSONReturnsError(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": [{"chapter_index": "not a number"}]}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	if _, err := checker.Check(context.Background(), testBible(), testScript()); err == nil {
		t.Fatalf("expected an error when chapter_index isn't a number")
	}
}

func TestCheckRendersBibleAndScriptTextIntoPrompt(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	if _, err := checker.Check(context.Background(), testBible(), testScript()); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if len(client.Calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(client.Calls))
	}
	prompt := client.Calls[0].Prompt
	if !strings.Contains(prompt, "Dana Whitfield") {
		t.Fatalf("expected prompt to contain the narrator's name, got: %s", prompt)
	}
	if !strings.Contains(prompt, "kept the ledger") {
		t.Fatalf("expected prompt to contain chapter 1's content, got: %s", prompt)
	}
	if !strings.Contains(prompt, "different form") {
		t.Fatalf("expected prompt to contain chapter 2's content, got: %s", prompt)
	}
	if client.Calls[0].Opts.Model != "test-model" {
		t.Fatalf("expected model to be passed through, got %q", client.Calls[0].Opts.Model)
	}
}

func TestCheckDefaultUsesSummariesNotFullText(t *testing.T) {
	script := testScript()
	script.Chapters[0].Text = "ONLY-IN-FULL-TEXT marker that no summary repeats"
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	if _, err := checker.Check(context.Background(), testBible(), script); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if strings.Contains(client.Calls[0].Prompt, "ONLY-IN-FULL-TEXT") {
		t.Fatalf("expected fullContext=false to use summaries, not Text, got: %s", client.Calls[0].Prompt)
	}
}

func TestCheckFullContextTrueUsesRealText(t *testing.T) {
	script := testScript()
	script.Chapters[0].Text = "ONLY-IN-FULL-TEXT marker that no summary repeats"
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, true)

	if _, err := checker.Check(context.Background(), testBible(), script); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !strings.Contains(client.Calls[0].Prompt, "ONLY-IN-FULL-TEXT") {
		t.Fatalf("expected fullContext=true to use the real chapter text, got: %s", client.Calls[0].Prompt)
	}
}

func TestReportAffectedChaptersDeduplicatesAndSorts(t *testing.T) {
	report := Report{Fixes: []Fix{
		{ChapterIndex: 12}, {ChapterIndex: 3}, {ChapterIndex: 12}, {ChapterIndex: 7},
	}}
	got := report.AffectedChapters()
	want := []int{3, 7, 12}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestReportAffectedChaptersEmpty(t *testing.T) {
	if got := (Report{}).AffectedChapters(); len(got) != 0 {
		t.Fatalf("expected no affected chapters, got %v", got)
	}
}

func TestCheckParsesCorrectedTitle(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{
		Text: `{"fixes": [], "title": "Four Years of Silence"}`,
	})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Title != "Four Years of Silence" {
		t.Fatalf("expected the corrected title, got %q", report.Title)
	}
}

func TestCheckEmptyTitleMeansNoChangeSuggested(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	report, err := checker.Check(context.Background(), testBible(), testScript())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Title != "" {
		t.Fatalf("expected an empty title when the model doesn't suggest a change, got %q", report.Title)
	}
}

func TestCheckRendersCurrentTitleIntoPrompt(t *testing.T) {
	client := llm.NewFakeClient(llm.Response{Text: `{"fixes": []}`})
	checker := NewChecker(client, testTemplate(t), "test-model", 5, false)

	if _, err := checker.Check(context.Background(), testBible(), testScript()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !strings.Contains(client.Calls[0].Prompt, "Seven Years of Silence") {
		t.Fatalf("expected the prompt to carry the script's current title, got: %s", client.Calls[0].Prompt)
	}
}
