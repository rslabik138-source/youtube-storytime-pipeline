package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GeminiClient generates images via Gemini's native generateContent
// endpoint with image output — the same endpoint shape scenario's
// internal/llm uses for text (generativelanguage.googleapis.com, native
// non-OpenAI-compat path, x-goog-api-key header), just with
// generationConfig.responseModalities:["IMAGE"] and a response part
// shaped as inlineData (base64 PNG bytes) instead of text.
//
// NOTE: the exact model ID for Gemini image generation changes as Google
// ships new versions — settings.yaml's gemini_model is the single place to
// update it, not this file.
type GeminiClient struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// NewGeminiClient builds a client against baseURL (the same
// generativelanguage.googleapis.com base scenario's settings.yaml already
// uses — a trailing "/openai" is stripped automatically, same convention
// as scenario's llm.NewGeminiNativeClient) and model (e.g.
// "gemini-2.5-flash-image").
func NewGeminiClient(baseURL, model, apiKey string) *GeminiClient {
	return &GeminiClient{baseURL: nativeBaseURL(baseURL), model: model, apiKey: apiKey, http: http.DefaultClient}
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

type geminiImageGenConfig struct {
	ResponseModalities []string `json:"responseModalities"`
}

type geminiImageRequest struct {
	Contents         []geminiContent      `json:"contents"`
	GenerationConfig geminiImageGenConfig `json:"generationConfig"`
}

type geminiImagePart struct {
	Text       string `json:"text,omitempty"`
	InlineData *struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"` // base64
	} `json:"inlineData,omitempty"`
}

type geminiImageResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiImagePart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate calls Gemini's generateContent with image output. Aspect ratio
// isn't a generateContent-level parameter for native image output, so it's
// folded into the prompt text instead of a request field.
func (c *GeminiClient) Generate(ctx context.Context, prompt string, opts Options) (Image, error) {
	fullPrompt := prompt
	if opts.AspectRatio != "" {
		fullPrompt = fmt.Sprintf("%s\nImage aspect ratio: %s.", prompt, opts.AspectRatio)
	}

	body := geminiImageRequest{
		Contents:         []geminiContent{{Role: "user", Parts: []geminiContentPart{{Text: fullPrompt}}}},
		GenerationConfig: geminiImageGenConfig{ResponseModalities: []string{"IMAGE"}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: gemini: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-goog-api-key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: gemini: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: gemini: read response: %w", err)
	}

	var parsed geminiImageResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(respBody)
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
			detail = parsed.Error.Message
		}
		return Image{}, fmt.Errorf("imagegen: gemini: status %d: %s", resp.StatusCode, detail)
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Image{}, fmt.Errorf("imagegen: gemini: parse response: %w", err)
	}

	for _, cand := range parsed.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				png, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return Image{}, fmt.Errorf("imagegen: gemini: decode image data: %w", err)
				}
				return Image{PNG: png, Provider: "gemini", Model: c.model}, nil
			}
		}
	}
	return Image{}, fmt.Errorf("imagegen: gemini: response contained no image data")
}
