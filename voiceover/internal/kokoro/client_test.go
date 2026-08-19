package kokoro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSpeakSendsExpectedRequestBody(t *testing.T) {
	var gotBody speechRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("RIFF....WAVEfake"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 3)
	wav, err := c.Speak(context.Background(), "hello world", "af_bella", 1.0)
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if len(wav) == 0 {
		t.Fatalf("expected non-empty WAV bytes")
	}
	if gotBody.Model != "kokoro" || gotBody.Voice != "af_bella" || gotBody.Input != "hello world" ||
		gotBody.ResponseFormat != "wav" || gotBody.Speed != 1.0 {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
}

func TestSpeakRetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("busy"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("wav-bytes"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 3)
	// Keep the test fast: shrink the backoff by using a tiny effective wait
	// isn't directly configurable, so just bound the test with a timeout
	// context instead of asserting on wall-clock time.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wav, err := c.Speak(ctx, "hello", "af_bella", 1.0)
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if string(wav) != "wav-bytes" {
		t.Fatalf("unexpected wav bytes: %q", wav)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", calls)
	}
}

func TestSpeakDoesNotRetryOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad voice id"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 3)
	_, err := c.Speak(context.Background(), "hello", "unknown_voice", 1.0)
	if err == nil {
		t.Fatalf("expected an error for a 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected the error to mention the status code, got: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 attempt (4xx isn't retryable), got %d", calls)
	}
}

func TestSpeakExhaustsRetriesAndReturnsLastError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("still busy"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.Speak(ctx, "hello", "af_bella", 1.0)
	if err == nil {
		t.Fatalf("expected an error after exhausting retries")
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected exactly 3 attempts (maxRetries), got %d", calls)
	}
}

func TestSpeakRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first attempt

	_, err := c.Speak(ctx, "hello", "af_bella", 1.0)
	if err == nil {
		t.Fatalf("expected an error for an already-cancelled context")
	}
}

// TestVoicesParsesRealKokoroFastAPIShape uses raw JSON captured from an
// actual running Kokoro-FastAPI instance, not our own voicesResponse
// struct re-serialized — a mock built from the same (wrong) struct that
// caused the original bug would never have caught it. The real endpoint
// returns objects ({"id":"...","name":"..."}), not a plain string array.
func TestVoicesParsesRealKokoroFastAPIShape(t *testing.T) {
	const realResponse = `{"voices":[{"id":"af_alloy","name":"af_alloy"},{"id":"af_bella","name":"af_bella"},{"id":"am_adam","name":"am_adam"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(realResponse))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 3)
	voices, err := c.Voices(context.Background())
	if err != nil {
		t.Fatalf("Voices: %v", err)
	}
	if len(voices) != 3 || voices[0] != "af_alloy" || voices[1] != "af_bella" || voices[2] != "am_adam" {
		t.Fatalf("unexpected voices: %v", voices)
	}
}

func TestVoicesParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/voices" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(voicesResponse{Voices: []voiceEntry{{ID: "af_bella"}, {ID: "am_adam"}}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 3)
	voices, err := c.Voices(context.Background())
	if err != nil {
		t.Fatalf("Voices: %v", err)
	}
	if len(voices) != 2 || voices[0] != "af_bella" || voices[1] != "am_adam" {
		t.Fatalf("unexpected voices: %v", voices)
	}
}

func TestVoicesUnexpectedStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil, 1)
	if _, err := c.Voices(context.Background()); err == nil {
		t.Fatalf("expected an error for a 500 response")
	}
}
