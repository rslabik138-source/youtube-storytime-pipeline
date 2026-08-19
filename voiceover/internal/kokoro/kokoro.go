// Package kokoro talks to a local Kokoro-FastAPI instance's
// OpenAI-compatible /v1/audio/speech endpoint. Everything downstream
// (chunk assembly, the CLI) depends on the Synthesizer interface, never on
// *Client directly, so tests run against FakeSynth with no Docker and no
// network call.
package kokoro

import "context"

// Synthesizer turns one chunk of text into WAV audio bytes in a given
// voice, and lists the voices currently available to synthesize with.
type Synthesizer interface {
	// Speak synthesizes text in voice at speed (1.0 = normal pace). Must
	// honor ctx cancellation — a long-running batch over dozens of chunks
	// needs to stop promptly if the caller gives up.
	Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error)
	// Voices lists every voice ID the backend currently serves.
	Voices(ctx context.Context) ([]string, error)
}
