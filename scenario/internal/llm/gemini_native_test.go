package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func nativeServerReturning(t *testing.T, status int, body string) (Client, func() *http.Request, func() []byte) {
	t.Helper()
	var lastReq *http.Request
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReq = r
		lastBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewGeminiNativeClient("google-ai-studio", srv.URL+"/openai", "test-key")
	return c, func() *http.Request { return lastReq }, func() []byte { return lastBody }
}

const nativeSuccessBody = `{
	"candidates": [{
		"content": {"parts": [{"text": "hello world"}], "role": "model"},
		"finishReason": "STOP"
	}],
	"usageMetadata": {
		"promptTokenCount": 9,
		"candidatesTokenCount": 5,
		"thoughtsTokenCount": 185,
		"totalTokenCount": 199
	}
}`

func TestGeminiNativeClientComplete(t *testing.T) {
	c, _, _ := nativeServerReturning(t, http.StatusOK, nativeSuccessBody)

	resp, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash", MaxTokens: 100})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello world" {
		t.Fatalf("expected text %q, got %q", "hello world", resp.Text)
	}
	if resp.TokensIn != 9 {
		t.Fatalf("expected TokensIn=9, got %d", resp.TokensIn)
	}
	// TokensOut must be visible + thinking combined (5+185=190) — matching
	// Response.ThinkingTokens' contract that it's already included in
	// TokensOut, not additional to it.
	if resp.TokensOut != 190 {
		t.Fatalf("expected TokensOut=190 (candidates + thoughts), got %d", resp.TokensOut)
	}
	if resp.ThinkingTokens != 185 {
		t.Fatalf("expected ThinkingTokens=185, got %d", resp.ThinkingTokens)
	}
	if resp.Provider != "google-ai-studio" {
		t.Fatalf("expected provider %q, got %q", "google-ai-studio", resp.Provider)
	}
}

func TestGeminiNativeClientStripsOpenAISuffixFromBaseURL(t *testing.T) {
	c, lastReq, _ := nativeServerReturning(t, http.StatusOK, nativeSuccessBody)

	if _, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	req := lastReq()
	if req == nil {
		t.Fatalf("expected a request to have been made")
	}
	if strings.Contains(req.URL.Path, "/openai") {
		t.Fatalf("expected the /openai suffix to be stripped from the native URL, got path %q", req.URL.Path)
	}
	if !strings.Contains(req.URL.Path, "gemini-3.6-flash:generateContent") {
		t.Fatalf("expected the model:generateContent path, got %q", req.URL.Path)
	}
}

func TestGeminiNativeClientSendsAPIKeyHeaderNotBearer(t *testing.T) {
	c, lastReq, _ := nativeServerReturning(t, http.StatusOK, nativeSuccessBody)

	if _, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	req := lastReq()
	if got := req.Header.Get("x-goog-api-key"); got != "test-key" {
		t.Fatalf("expected x-goog-api-key header %q, got %q", "test-key", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no Authorization header (native API uses x-goog-api-key), got %q", got)
	}
}

func TestGeminiNativeClientSendsThinkingBudgetWhenSet(t *testing.T) {
	c, _, lastBody := nativeServerReturning(t, http.StatusOK, nativeSuccessBody)

	_, err := c.Complete(context.Background(), "hi", Options{
		Model: "gemini-3.6-flash", HasThinkingBudget: true, ThinkingBudgetTokens: 0,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent geminiRequest
	if err := json.Unmarshal(lastBody(), &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	if sent.GenerationConfig == nil || sent.GenerationConfig.ThinkingConfig == nil {
		t.Fatalf("expected thinkingConfig to be present when HasThinkingBudget is true, got %+v", sent.GenerationConfig)
	}
	if sent.GenerationConfig.ThinkingConfig.ThinkingBudget != 0 {
		t.Fatalf("expected thinkingBudget 0, got %d", sent.GenerationConfig.ThinkingConfig.ThinkingBudget)
	}
}

func TestGeminiNativeClientOmitsThinkingConfigWhenNotSet(t *testing.T) {
	c, _, lastBody := nativeServerReturning(t, http.StatusOK, nativeSuccessBody)

	_, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent geminiRequest
	if err := json.Unmarshal(lastBody(), &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	if sent.GenerationConfig != nil && sent.GenerationConfig.ThinkingConfig != nil {
		t.Fatalf("expected no thinkingConfig when HasThinkingBudget is false, got %+v", sent.GenerationConfig.ThinkingConfig)
	}
}

func TestGeminiNativeClientErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErrStr string
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":{"code":429,"message":"slow down"}}`, "rate limited"},
		{"server error", http.StatusServiceUnavailable, `{"error":{"code":503,"message":"high demand"}}`, "server error"},
		{"model not found", http.StatusNotFound, `{"error":{"code":404,"message":"no such model"}}`, "model not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := nativeServerReturning(t, tt.status, tt.body)
			_, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash"})
			if err == nil || !strings.Contains(err.Error(), tt.wantErrStr) {
				t.Fatalf("expected an error containing %q, got %v", tt.wantErrStr, err)
			}
		})
	}
}
