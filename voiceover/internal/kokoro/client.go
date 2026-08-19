package kokoro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the real Synthesizer, talking to Kokoro-FastAPI over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
	maxRetries int
}

// NewClient builds a Client against baseURL (e.g. "http://localhost:8880").
// httpClient nil defaults to http.DefaultClient. maxRetries <= 0 defaults
// to 3 total attempts — one bad chunk out of dozens in a long script
// shouldn't fail the whole render over a transient 5xx.
func NewClient(baseURL string, httpClient *http.Client, maxRetries int) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient, maxRetries: maxRetries}
}

type speechRequest struct {
	Model          string  `json:"model"`
	Voice          string  `json:"voice"`
	Input          string  `json:"input"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed"`
}

// Speak synthesizes text in voice at speed, retrying a 5xx response or a
// transport-level error with a linear backoff (attempt * 500ms) up to
// maxRetries attempts total. A 4xx is never retried — that's a request
// problem (bad voice ID, malformed input), not a transient one. Honors ctx
// cancellation both between attempts and during the backoff wait.
func (c *Client) Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	if speed <= 0 {
		speed = 1.0
	}
	body, err := json.Marshal(speechRequest{Model: "kokoro", Voice: voice, Input: text, ResponseFormat: "wav", Speed: speed})
	if err != nil {
		return nil, fmt.Errorf("kokoro: marshal request: %w", err)
	}
	url := c.baseURL + "/v1/audio/speech"

	var lastErr error
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		wav, retryable, err := c.speakOnce(ctx, url, body)
		if err == nil {
			return wav, nil
		}
		lastErr = err
		if !retryable || attempt == c.maxRetries {
			break
		}

		backoff := time.Duration(attempt) * 500 * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("kokoro: speak (voice=%s, %d attempt(s)): %w", voice, c.maxRetries, lastErr)
}

func (c *Client) speakOnce(ctx context.Context, url string, body []byte) (wav []byte, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request: %w", err) // transport errors (connection refused, timeout) are retryable
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("read response: %w", err)
	}
	switch {
	case resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("server error %d: %s", resp.StatusCode, truncate(respBody, 300))
	case resp.StatusCode >= 300:
		return nil, false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(respBody, 300))
	}
	return respBody, false, nil
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// voiceEntry is one element of the real Kokoro-FastAPI /v1/audio/voices
// response — confirmed against a live instance: {"voices":
// [{"id":"af_alloy","name":"af_alloy"}, ...]}, objects with id/name, NOT
// a plain array of strings.
type voiceEntry struct {
	ID string `json:"id"`
}

type voicesResponse struct {
	Voices []voiceEntry `json:"voices"`
}

// Voices lists every voice Kokoro-FastAPI currently serves, from
// GET /v1/audio/voices — used by `voice list-voices` to diff against
// configs/voices.yaml and flag voices nobody has described yet.
func (c *Client) Voices(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/audio/voices", nil)
	if err != nil {
		return nil, fmt.Errorf("kokoro: build voices request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kokoro: voices request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kokoro: read voices response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kokoro: voices: unexpected status %d: %s", resp.StatusCode, truncate(respBody, 300))
	}

	var parsed voicesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("kokoro: parse voices response: %w", err)
	}
	ids := make([]string, len(parsed.Voices))
	for i, v := range parsed.Voices {
		ids[i] = v.ID
	}
	return ids, nil
}
