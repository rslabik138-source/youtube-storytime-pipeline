package textgen

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// Response is one completed text-generation call.
type Response struct {
	Text      string
	TokensIn  int
	TokensOut int
	Model     string
}

// Client completes a thumbnail-text prompt. The real implementation talks
// to any OpenAI-compatible chat completions endpoint (Google AI Studio's
// included) — see NewClient. Tests use FakeClient instead.
type Client interface {
	Complete(ctx context.Context, prompt, model string) (Response, error)
}

type openAIClient struct {
	sdk *openai.Client
}

// NewClient builds a Client against baseURL using apiKey — the same
// OpenAI-compatible chat completions convention scenario's own LLM client
// uses (github.com/sashabaranov/go-openai), independently constructed here
// since cross-module dependencies are the file contract, not shared Go
// packages.
func NewClient(baseURL, apiKey string) Client {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &openAIClient{sdk: openai.NewClientWithConfig(cfg)}
}

func (c *openAIClient) Complete(ctx context.Context, prompt, model string) (Response, error) {
	resp, err := c.sdk.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    model,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}},
		// The whole response is at most 6 short lines under 40 words total,
		// but gemini-3.5-flash-lite can spend real thinking tokens before
		// writing the visible JSON (observed in scenario's own chapter
		// generation) — 1500 leaves headroom for that, not for the answer
		// itself.
		MaxTokens: 1500,
	})
	if err != nil {
		return Response{}, fmt.Errorf("textgen: complete: %w", err)
	}
	if len(resp.Choices) == 0 {
		return Response{}, fmt.Errorf("textgen: %s returned no choices", model)
	}
	return Response{
		Text: resp.Choices[0].Message.Content, Model: resp.Model,
		TokensIn: resp.Usage.PromptTokens, TokensOut: resp.Usage.CompletionTokens,
	}, nil
}
