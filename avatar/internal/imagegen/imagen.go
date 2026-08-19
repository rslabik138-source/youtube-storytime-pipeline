package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ImagenClient generates images via Imagen's :predict endpoint (a
// different shape than Gemini's generateContent: instances/parameters in,
// base64 predictions out) — the documented fallback per the brief, used
// only when the primary Gemini path fails.
//
// NOTE: the exact model ID (e.g. "imagen-4.0-fast-generate-001") changes
// as Google ships new versions — settings.yaml's imagen_model is the
// single place to update it, not this file.
type ImagenClient struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// NewImagenClient builds a client against baseURL (same base as
// GeminiClient) and model.
func NewImagenClient(baseURL, model, apiKey string) *ImagenClient {
	return &ImagenClient{baseURL: nativeBaseURL(baseURL), model: model, apiKey: apiKey, http: http.DefaultClient}
}

type imagenInstance struct {
	Prompt string `json:"prompt"`
}

type imagenParameters struct {
	SampleCount int    `json:"sampleCount"`
	AspectRatio string `json:"aspectRatio,omitempty"`
}

type imagenRequest struct {
	Instances  []imagenInstance `json:"instances"`
	Parameters imagenParameters `json:"parameters"`
}

type imagenResponse struct {
	Predictions []struct {
		BytesBase64Encoded string `json:"bytesBase64Encoded"`
		MimeType           string `json:"mimeType"`
	} `json:"predictions"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *ImagenClient) Generate(ctx context.Context, prompt string, opts Options) (Image, error) {
	body := imagenRequest{
		Instances:  []imagenInstance{{Prompt: prompt}},
		Parameters: imagenParameters{SampleCount: 1, AspectRatio: opts.AspectRatio},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: imagen: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:predict", c.baseURL, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: imagen: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-goog-api-key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: imagen: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: imagen: read response: %w", err)
	}

	var parsed imagenResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(respBody)
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
			detail = parsed.Error.Message
		}
		return Image{}, fmt.Errorf("imagegen: imagen: status %d: %s", resp.StatusCode, detail)
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Image{}, fmt.Errorf("imagegen: imagen: parse response: %w", err)
	}
	if len(parsed.Predictions) == 0 || parsed.Predictions[0].BytesBase64Encoded == "" {
		return Image{}, fmt.Errorf("imagegen: imagen: response contained no image data")
	}

	png, err := base64.StdEncoding.DecodeString(parsed.Predictions[0].BytesBase64Encoded)
	if err != nil {
		return Image{}, fmt.Errorf("imagegen: imagen: decode image data: %w", err)
	}
	return Image{PNG: png, Provider: "imagen", Model: c.model}, nil
}
