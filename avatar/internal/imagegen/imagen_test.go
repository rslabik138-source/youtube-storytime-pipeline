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

func TestImagenGenerateSendsExpectedRequestAndParsesImage(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4E, 0x47, 0xAA, 0xBB}
	var gotBody imagenRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/imagen-test:predict") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(imagenResponse{
			Predictions: []struct {
				BytesBase64Encoded string `json:"bytesBase64Encoded"`
				MimeType           string `json:"mimeType"`
			}{{BytesBase64Encoded: base64.StdEncoding.EncodeToString(wantPNG), MimeType: "image/png"}},
		})
	}))
	defer srv.Close()

	c := NewImagenClient(srv.URL, "imagen-test", "test-key")
	img, err := c.Generate(context.Background(), "a portrait", Options{AspectRatio: "1:1"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if string(img.PNG) != string(wantPNG) {
		t.Fatalf("unexpected PNG bytes: %v", img.PNG)
	}
	if img.Provider != "imagen" || img.Model != "imagen-test" {
		t.Fatalf("unexpected provider/model: %+v", img)
	}

	if len(gotBody.Instances) != 1 || gotBody.Instances[0].Prompt != "a portrait" {
		t.Fatalf("expected the prompt in instances[0], got: %+v", gotBody.Instances)
	}
	if gotBody.Parameters.AspectRatio != "1:1" || gotBody.Parameters.SampleCount != 1 {
		t.Fatalf("unexpected parameters: %+v", gotBody.Parameters)
	}
}

func TestImagenGenerateNoPredictionsReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imagenResponse{})
	}))
	defer srv.Close()

	c := NewImagenClient(srv.URL, "imagen-test", "test-key")
	_, err := c.Generate(context.Background(), "a portrait", Options{})
	if err == nil {
		t.Fatalf("expected an error when the response has no predictions")
	}
}

func TestImagenGenerateErrorStatusReturnsErrorWithMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": 503, "message": "model overloaded"},
		})
	}))
	defer srv.Close()

	c := NewImagenClient(srv.URL, "imagen-test", "test-key")
	_, err := c.Generate(context.Background(), "a portrait", Options{})
	if err == nil {
		t.Fatalf("expected an error for a 503 response")
	}
	if !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("expected the error to include the API's message, got: %v", err)
	}
}
