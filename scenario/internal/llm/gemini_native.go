package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// geminiNativeClient talks to Gemini's own generateContent endpoint
// directly instead of going through the OpenAI-compatibility layer.
// Confirmed against the real API: the OpenAI-compat endpoint accepts
// reasoning_effort but never reports back how many tokens it actually
// spent thinking (completion_tokens_details.reasoning_tokens is simply
// absent from its responses) — the native endpoint's usageMetadata.
// thoughtsTokenCount does report it, and thinkingConfig.thinkingBudget
// takes the real numeric budget instead of a bucketed low/medium/high
// level. Use this for a provider where accurate thinking-token visibility
// (or, for a model that supports it, an actual thinkingBudget: 0) matters
// more than staying on the shared OpenAI-compatible code path.
type geminiNativeClient struct {
	name    string
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewGeminiNativeClient builds a Client against Gemini's native
// generateContent API. baseURL is the same OpenAI-compat base already used
// for this provider in settings.yaml (e.g.
// "https://generativelanguage.googleapis.com/v1beta/openai") — a trailing
// "/openai" segment is stripped automatically to get the native base,
// so no separate config field is needed.
func NewGeminiNativeClient(name, baseURL, apiKey string) Client {
	return &geminiNativeClient{
		name: name, baseURL: nativeBaseURL(baseURL), apiKey: apiKey, http: http.DefaultClient,
	}
}

func nativeBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	return strings.TrimSuffix(trimmed, "/openai")
}

type geminiContentPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string              `json:"role,omitempty"`
	Parts []geminiContentPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     float64               `json:"temperature,omitempty"`
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiContentPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *geminiNativeClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	body := geminiRequest{
		Contents: []geminiContent{{Role: "user", Parts: []geminiContentPart{{Text: prompt}}}},
	}
	if opts.System != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiContentPart{{Text: opts.System}}}
	}

	genCfg := &geminiGenerationConfig{
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxTokens,
	}
	if opts.HasThinkingBudget {
		genCfg.ThinkingConfig = &geminiThinkingConfig{ThinkingBudget: opts.ThinkingBudgetTokens}
	}
	body.GenerationConfig = genCfg

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, opts.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-goog-api-key", c.apiKey)
	}

	httpClient := c.http
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("llm: %s request: %w", c.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("llm: read response: %w", err)
	}

	var parsed geminiResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(respBody)
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
			detail = parsed.Error.Message
		}
		return Response{}, classifyStatus(resp.StatusCode, detail)
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, fmt.Errorf("llm: parse response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return Response{}, fmt.Errorf("llm: %s returned no candidates", c.name)
	}

	var text strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}

	return Response{
		Text:     text.String(),
		TokensIn: parsed.UsageMetadata.PromptTokenCount,
		// TokensOut is the full billable output — visible text plus
		// thinking, matching how Gemini actually bills and matching
		// Response.ThinkingTokens' contract ("already included in
		// TokensOut, broken out separately").
		TokensOut:      parsed.UsageMetadata.CandidatesTokenCount + parsed.UsageMetadata.ThoughtsTokenCount,
		ThinkingTokens: parsed.UsageMetadata.ThoughtsTokenCount,
		Provider:       c.name,
		Model:          opts.Model,
	}, nil
}
