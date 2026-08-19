package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serverReturning spins up an httptest server that always answers with the
// given status and body, and returns a Client pointed at it — this
// exercises the real HTTP round trip and JSON decoding through the real
// go-openai client, without ever touching a real provider.
func serverReturning(t *testing.T, status int, body string) Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewOpenAICompatClient("test-provider", srv.URL, "test-key")
}

const successBody = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"created": 1700000000,
	"model": "gemini-test",
	"choices": [{
		"index": 0,
		"message": {"role": "assistant", "content": "hello world"},
		"finish_reason": "stop"
	}],
	"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
}`

func TestOpenAICompatClientComplete(t *testing.T) {
	c := serverReturning(t, http.StatusOK, successBody)

	resp, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-test", MaxTokens: 100})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello world" {
		t.Fatalf("expected text %q, got %q", "hello world", resp.Text)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 5 {
		t.Fatalf("expected tokens in=10 out=5, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
	if resp.Provider != "test-provider" {
		t.Fatalf("expected provider %q, got %q", "test-provider", resp.Provider)
	}
}

func TestOpenAICompatClientErrorClassification(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		wantRate        bool
		wantServer      bool
		wantModelNotFnd bool
	}{
		{"429 classifies as rate limited", http.StatusTooManyRequests, true, false, false},
		{"500 classifies as server error", http.StatusInternalServerError, false, true, false},
		{"503 classifies as server error", http.StatusServiceUnavailable, false, true, false},
		{"404 classifies as model not found", http.StatusNotFound, false, false, true},
		{"400 is not retryable", http.StatusBadRequest, false, false, false},
	}

	errBody := `{"error": {"message": "boom", "type": "invalid_request_error"}}`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := serverReturning(t, tt.status, errBody)
			_, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-test", MaxTokens: 100})
			if err == nil {
				t.Fatalf("expected an error for status %d", tt.status)
			}
			if got := errors.Is(err, ErrRateLimited); got != tt.wantRate {
				t.Fatalf("errors.Is(err, ErrRateLimited) = %v, want %v (err: %v)", got, tt.wantRate, err)
			}
			if got := errors.Is(err, ErrServer); got != tt.wantServer {
				t.Fatalf("errors.Is(err, ErrServer) = %v, want %v (err: %v)", got, tt.wantServer, err)
			}
			if got := errors.Is(err, ErrModelNotFound); got != tt.wantModelNotFnd {
				t.Fatalf("errors.Is(err, ErrModelNotFound) = %v, want %v (err: %v)", got, tt.wantModelNotFnd, err)
			}
		})
	}
}

func TestOpenAICompatClientSendsSystemPrompt(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capturedBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(successBody))
	}))
	defer srv.Close()

	c := NewOpenAICompatClient("test-provider", srv.URL, "test-key")
	_, err := c.Complete(context.Background(), "the prompt", Options{Model: "m", MaxTokens: 10, System: "the system rules"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedBody == "" {
		t.Fatalf("expected a captured request body")
	}
	if !strings.Contains(capturedBody, `"role":"system"`) || !strings.Contains(capturedBody, "the system rules") {
		t.Fatalf("expected a system message in the request body, got %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"role":"user"`) || !strings.Contains(capturedBody, "the prompt") {
		t.Fatalf("expected a user message in the request body, got %s", capturedBody)
	}
}

func TestOpenAICompatClientSendsReasoningEffortViaSDKPath(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capturedBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(successBody))
	}))
	defer srv.Close()

	c := NewOpenAICompatClient("test-provider", srv.URL, "test-key")
	_, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash", ReasoningEffort: "none"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(capturedBody, `"reasoning_effort":"none"`) {
		t.Fatalf("expected reasoning_effort to be sent on the wire via the SDK path, got: %s", capturedBody)
	}
}

const successBodyWithReasoning = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"created": 1700000000,
	"model": "gemini-3.6-flash",
	"choices": [{
		"index": 0,
		"message": {"role": "assistant", "content": "hello world"},
		"finish_reason": "stop"
	}],
	"usage": {
		"prompt_tokens": 10, "completion_tokens": 55, "total_tokens": 65,
		"completion_tokens_details": {"reasoning_tokens": 50}
	}
}`

func TestOpenAICompatClientExtraBodyMergedIntoWireRequest(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capturedBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(successBodyWithReasoning))
	}))
	defer srv.Close()

	c := NewOpenAICompatClient("google-ai-studio", srv.URL, "test-key")
	resp, err := c.Complete(context.Background(), "the prompt", Options{
		Model: "gemini-3.6-flash",
		ExtraBody: map[string]any{
			"google": map[string]any{"thinking_config": map[string]any{"thinking_budget": 0}},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if !strings.Contains(capturedBody, `"thinking_budget":0`) {
		t.Fatalf("expected thinking_budget to be merged into the wire request body, got: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"google"`) {
		t.Fatalf("expected a top-level \"google\" key (not nested under extra_body), got: %s", capturedBody)
	}
	if strings.Contains(capturedBody, `"extra_body"`) {
		t.Fatalf("expected ExtraBody's keys merged at the top level, not wrapped in an \"extra_body\" key, got: %s", capturedBody)
	}

	if resp.Text != "hello world" {
		t.Fatalf("expected text %q, got %q", "hello world", resp.Text)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 55 {
		t.Fatalf("expected tokens in=10 out=55, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
	if resp.ThinkingTokens != 50 {
		t.Fatalf("expected 50 thinking tokens, got %d", resp.ThinkingTokens)
	}
}

func TestOpenAICompatClientThinkingTokensViaSDKPath(t *testing.T) {
	c := serverReturning(t, http.StatusOK, successBodyWithReasoning)

	resp, err := c.Complete(context.Background(), "hi", Options{Model: "gemini-3.6-flash"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.ThinkingTokens != 50 {
		t.Fatalf("expected 50 thinking tokens via the SDK path too, got %d", resp.ThinkingTokens)
	}
}

func TestOpenAICompatClientExtraBodyErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantRate   bool
		wantServer bool
	}{
		{"429 classifies as rate limited", http.StatusTooManyRequests, true, false},
		{"503 classifies as server error", http.StatusServiceUnavailable, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error": {"message": "boom"}}`))
			}))
			defer srv.Close()

			c := NewOpenAICompatClient("test-provider", srv.URL, "test-key")
			_, err := c.Complete(context.Background(), "hi", Options{
				Model:     "m",
				ExtraBody: map[string]any{"google": map[string]any{"thinking_config": map[string]any{"thinking_budget": 0}}},
			})
			if err == nil {
				t.Fatalf("expected an error for status %d", tt.status)
			}
			if got := errors.Is(err, ErrRateLimited); got != tt.wantRate {
				t.Fatalf("errors.Is(err, ErrRateLimited) = %v, want %v (err: %v)", got, tt.wantRate, err)
			}
			if got := errors.Is(err, ErrServer); got != tt.wantServer {
				t.Fatalf("errors.Is(err, ErrServer) = %v, want %v (err: %v)", got, tt.wantServer, err)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Fatalf("expected the error detail from the response body, got %v", err)
			}
		})
	}
}
