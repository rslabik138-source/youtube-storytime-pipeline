package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

type openaiCompatClient struct {
	name    string
	baseURL string
	apiKey  string
	sdk     *openai.Client
	http    *http.Client
}

// NewOpenAICompatClient builds a Client against any OpenAI-compatible chat
// completions endpoint. name is a friendly label stamped onto
// Response.Provider and used in failover logs — it has no effect on the
// wire protocol. apiKey may be empty for a key-less endpoint (local Ollama).
func NewOpenAICompatClient(name, baseURL, apiKey string) Client {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &openaiCompatClient{
		name: name, baseURL: baseURL, apiKey: apiKey,
		sdk: openai.NewClientWithConfig(cfg), http: http.DefaultClient,
	}
}

func (c *openaiCompatClient) Complete(ctx context.Context, prompt string, opts Options) (Response, error) {
	// go-openai's ChatCompletionRequest has no field for vendor extensions
	// like Gemini's "google": {"thinking_config": ...} — only ExtraBody
	// needs that, so it's the only case that bypasses the SDK for a raw
	// HTTP call built by hand.
	if len(opts.ExtraBody) > 0 {
		return c.completeRaw(ctx, prompt, opts)
	}

	messages := make([]openai.ChatCompletionMessage, 0, 2)
	if opts.System != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem, Content: opts.System,
		})
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser, Content: prompt,
	})

	// MaxTokens (not the newer MaxCompletionTokens) for the widest
	// compatibility across third-party OpenAI-compatible endpoints — not
	// every compat layer has adopted the newer field yet.
	req := openai.ChatCompletionRequest{
		Model:           opts.Model,
		Messages:        messages,
		MaxTokens:       opts.MaxTokens,
		Temperature:     float32(opts.Temperature),
		ReasoningEffort: opts.ReasoningEffort,
	}

	resp, err := c.sdk.CreateChatCompletion(ctx, req)
	if err != nil {
		return Response{}, classifyError(err)
	}
	if len(resp.Choices) == 0 {
		return Response{}, fmt.Errorf("llm: %s returned no choices", c.name)
	}

	out := Response{
		Text:      resp.Choices[0].Message.Content,
		TokensIn:  resp.Usage.PromptTokens,
		TokensOut: resp.Usage.CompletionTokens,
		Provider:  c.name,
		Model:     resp.Model,
	}
	if resp.Usage.CompletionTokensDetails != nil {
		out.ThinkingTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}
	return out, nil
}

// classifyError maps a 429/5xx API error to ErrRateLimited/ErrServer so
// WithRetry recognizes it as retryable, and a 404 to ErrModelNotFound so
// WithFailover recognizes it as a config problem rather than something a
// different provider can paper over. Anything else (bad request, auth
// failure, context cancellation, ...) passes through unchanged.
func classifyError(err error) error {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return classifyStatus(apiErr.HTTPStatusCode, apiErr.Error())
	}
	return err
}

func classifyStatus(status int, detail string) error {
	switch {
	case status == 429:
		return fmt.Errorf("%w: %s", ErrRateLimited, detail)
	case status == 404:
		return fmt.Errorf("%w: %s", ErrModelNotFound, detail)
	case status >= 500:
		return fmt.Errorf("%w: %s", ErrServer, detail)
	}
	return errors.New(detail)
}

// rawChatCompletionResponse mirrors just the fields Complete needs from an
// OpenAI-compatible chat completion response — used for the ExtraBody path,
// which can't go through go-openai's typed request.
type rawChatCompletionResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens            int `json:"prompt_tokens"`
		CompletionTokens        int `json:"completion_tokens"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// completeRaw builds the request body as a plain map so opts.ExtraBody's
// keys (e.g. "google") can be merged in at the top level, exactly like the
// official OpenAI SDKs' extra_body parameter does on the wire, then POSTs
// it by hand since go-openai's typed request has no room for them.
func (c *openaiCompatClient) completeRaw(ctx context.Context, prompt string, opts Options) (Response, error) {
	messages := make([]map[string]string, 0, 2)
	if opts.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": opts.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})

	body := map[string]any{
		"model":    opts.Model,
		"messages": messages,
	}
	if opts.MaxTokens > 0 {
		body["max_tokens"] = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		body["temperature"] = opts.Temperature
	}
	if opts.ReasoningEffort != "" {
		body["reasoning_effort"] = opts.ReasoningEffort
	}
	for k, v := range opts.ExtraBody {
		body[k] = v
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	url := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(respBody)
		var parsed rawChatCompletionResponse
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
			detail = parsed.Error.Message
		}
		return Response{}, classifyStatus(resp.StatusCode, detail)
	}

	var parsed rawChatCompletionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, fmt.Errorf("llm: parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("llm: %s returned no choices", c.name)
	}

	return Response{
		Text:           parsed.Choices[0].Message.Content,
		TokensIn:       parsed.Usage.PromptTokens,
		TokensOut:      parsed.Usage.CompletionTokens,
		ThinkingTokens: parsed.Usage.CompletionTokensDetails.ReasoningTokens,
		Provider:       c.name,
		Model:          parsed.Model,
	}, nil
}
