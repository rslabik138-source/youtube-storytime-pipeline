package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiGenerateSendsExpectedRequestAndParsesImage(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4E, 0x47, 0x01, 0x02, 0x03}
	var gotBody geminiImageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/test-model:generateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("expected x-goog-api-key header, got %q", r.Header.Get("x-goog-api-key"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		resp := geminiImageResponse{
			Candidates: []struct {
				Content struct {
					Parts []geminiImagePart `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []geminiImagePart `json:"parts"`
				}{Parts: []geminiImagePart{{InlineData: &struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				}{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(wantPNG)}}}}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewGeminiClient(srv.URL, "test-model", "test-key")
	img, err := c.Generate(context.Background(), "a portrait", Options{AspectRatio: "16:9"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if string(img.PNG) != string(wantPNG) {
		t.Fatalf("unexpected PNG bytes: %v", img.PNG)
	}
	if img.Provider != "gemini" || img.Model != "test-model" {
		t.Fatalf("unexpected provider/model: %+v", img)
	}

	if len(gotBody.Contents) != 1 || !strings.Contains(gotBody.Contents[0].Parts[0].Text, "a portrait") {
		t.Fatalf("expected the prompt in the request body, got: %+v", gotBody)
	}
	if !strings.Contains(gotBody.Contents[0].Parts[0].Text, "16:9") {
		t.Fatalf("expected the aspect ratio folded into the prompt, got: %q", gotBody.Contents[0].Parts[0].Text)
	}
	if len(gotBody.GenerationConfig.ResponseModalities) != 1 || gotBody.GenerationConfig.ResponseModalities[0] != "IMAGE" {
		t.Fatalf("expected responseModalities=[IMAGE], got %v", gotBody.GenerationConfig.ResponseModalities)
	}
}

func TestGeminiGenerateNoImageInResponseReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(geminiImageResponse{})
	}))
	defer srv.Close()

	c := NewGeminiClient(srv.URL, "test-model", "test-key")
	_, err := c.Generate(context.Background(), "a portrait", Options{})
	if err == nil {
		t.Fatalf("expected an error when the response has no image data")
	}
}

func TestGeminiGenerateErrorStatusReturnsErrorWithMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": 400, "message": "invalid model"},
		})
	}))
	defer srv.Close()

	c := NewGeminiClient(srv.URL, "bad-model", "test-key")
	_, err := c.Generate(context.Background(), "a portrait", Options{})
	if err == nil {
		t.Fatalf("expected an error for a 400 response")
	}
	if !strings.Contains(err.Error(), "invalid model") {
		t.Fatalf("expected the error to include the API's message, got: %v", err)
	}
}

func TestGeminiGenerateStripsOpenAISuffixFromBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(geminiImageResponse{})
	}))
	defer srv.Close()

	c := NewGeminiClient(srv.URL+"/openai", "test-model", "test-key")
	c.Generate(context.Background(), "x", Options{})
	if !strings.HasPrefix(gotPath, "/models/") {
		t.Fatalf("expected the /openai suffix to be stripped from the base URL, got path %q", gotPath)
	}
}
