package textgen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serverReturning(t *testing.T, status int, body string) Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "test-key")
}

const successBody = `{
	"id": "chatcmpl-test",
	"object": "chat.completion",
	"created": 1700000000,
	"model": "gemini-3.5-flash-lite",
	"choices": [{
		"index": 0,
		"message": {"role": "assistant", "content": "{\"lines\":[],\"final_line\":\"x\"}"},
		"finish_reason": "stop"
	}],
	"usage": {"prompt_tokens": 120, "completion_tokens": 30, "total_tokens": 150}
}`

func TestOpenAIClientComplete(t *testing.T) {
	c := serverReturning(t, http.StatusOK, successBody)

	resp, err := c.Complete(context.Background(), "hi", "gemini-3.5-flash-lite")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != `{"lines":[],"final_line":"x"}` {
		t.Fatalf("unexpected text: %q", resp.Text)
	}
	if resp.TokensIn != 120 || resp.TokensOut != 30 {
		t.Fatalf("expected tokens in=120 out=30, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
	if resp.Model != "gemini-3.5-flash-lite" {
		t.Fatalf("unexpected model: %q", resp.Model)
	}
}

func TestOpenAIClientNoChoicesReturnsError(t *testing.T) {
	c := serverReturning(t, http.StatusOK, `{"choices": [], "usage": {}}`)
	if _, err := c.Complete(context.Background(), "hi", "gemini-3.5-flash-lite"); err == nil {
		t.Fatalf("expected an error when the response has no choices")
	}
}

func TestOpenAIClientServerErrorReturnsError(t *testing.T) {
	c := serverReturning(t, http.StatusInternalServerError, `{"error": {"message": "boom"}}`)
	if _, err := c.Complete(context.Background(), "hi", "gemini-3.5-flash-lite"); err == nil {
		t.Fatalf("expected an error on a 500 response")
	}
}
