package textgen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, text ThumbnailText) string {
	t.Helper()
	b, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestGenerateHappyPath(t *testing.T) {
	good := mustJSON(t, validText())
	client := NewFakeClient(Response{Text: good, Model: "gemini-3.5-flash-lite", TokensIn: 100, TokensOut: 50})

	result, err := Generate(context.Background(), client, testTemplate(t), testManifest(), "gemini-3.5-flash-lite", testOpener())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.Text.Lines) != len(validText().Lines) || result.Text.FinalLine != "THEN I FOUND THE RECEIPT" {
		t.Fatalf("unexpected result: %+v", result.Text)
	}
	if result.TokensIn != 100 || result.TokensOut != 50 {
		t.Fatalf("unexpected token accounting: %+v", result)
	}
	if len(client.Prompts) != 1 {
		t.Fatalf("expected exactly 1 call on the happy path, got %d", len(client.Prompts))
	}
}

func TestGenerateStripsMarkdownFences(t *testing.T) {
	good := "```json\n" + mustJSON(t, validText()) + "\n```"
	client := NewFakeClient(Response{Text: good, Model: "gemini-3.5-flash-lite"})

	result, err := Generate(context.Background(), client, testTemplate(t), testManifest(), "gemini-3.5-flash-lite", testOpener())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Text.FinalLine != "THEN I FOUND THE RECEIPT" {
		t.Fatalf("unexpected result: %+v", result.Text)
	}
}

func TestGenerateRetriesOnValidationFailureAndFeedsViolationsBack(t *testing.T) {
	badColor := validText()
	badColor.Lines[0].Color = "blue"

	client := NewFakeClient(
		Response{Text: mustJSON(t, badColor), Model: "gemini-3.5-flash-lite"},
		Response{Text: mustJSON(t, validText()), Model: "gemini-3.5-flash-lite"},
	)

	result, err := Generate(context.Background(), client, testTemplate(t), testManifest(), "gemini-3.5-flash-lite", testOpener())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Text.FinalLine != "THEN I FOUND THE RECEIPT" {
		t.Fatalf("expected the second (valid) attempt's result, got: %+v", result.Text)
	}
	if len(client.Prompts) != 2 {
		t.Fatalf("expected exactly 2 calls, got %d", len(client.Prompts))
	}
	if !strings.Contains(client.Prompts[1], "blue") {
		t.Fatalf("expected the retry prompt to mention the invalid color violation, got: %s", client.Prompts[1])
	}
}

func TestGenerateExhaustsAttemptsReturnsError(t *testing.T) {
	badColor := validText()
	badColor.Lines[0].Color = "blue"
	badJSON := mustJSON(t, badColor)

	client := NewFakeClient()
	for i := 0; i < maxAttempts; i++ {
		client.queue = append(client.queue, fakeResult{resp: Response{Text: badJSON, Model: "gemini-3.5-flash-lite"}})
	}

	_, err := Generate(context.Background(), client, testTemplate(t), testManifest(), "gemini-3.5-flash-lite", testOpener())
	if err == nil {
		t.Fatalf("expected an error after exhausting all attempts")
	}
	if len(client.Prompts) != maxAttempts {
		t.Fatalf("expected exactly %d calls, got %d", maxAttempts, len(client.Prompts))
	}
}

func TestGenerateMalformedJSONRetries(t *testing.T) {
	client := NewFakeClient(
		Response{Text: "not json at all", Model: "gemini-3.5-flash-lite"},
		Response{Text: mustJSON(t, validText()), Model: "gemini-3.5-flash-lite"},
	)

	result, err := Generate(context.Background(), client, testTemplate(t), testManifest(), "gemini-3.5-flash-lite", testOpener())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Text.FinalLine != "THEN I FOUND THE RECEIPT" {
		t.Fatalf("unexpected result: %+v", result.Text)
	}
}
