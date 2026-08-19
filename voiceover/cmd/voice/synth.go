package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/placeholder/voiceover/internal/assemble"
	"github.com/placeholder/voiceover/internal/chunk"
	"github.com/placeholder/voiceover/internal/kokoro"
)

// synthesizeAll runs chunks through synth with up to concurrency calls in
// flight at once, preserving chunk order in the returned slice regardless
// of completion order. The first chunk to fail cancels every other
// in-flight call (via ctx) instead of letting them run to a result nobody
// will use.
func synthesizeAll(ctx context.Context, synth kokoro.Synthesizer, chunks []chunk.Chunk, voice string, speed float64, concurrency int, logger *slog.Logger) ([]assemble.PieceAudio, error) {
	if concurrency <= 0 {
		concurrency = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pieces := make([]assemble.PieceAudio, len(chunks))
	errs := make([]error, len(chunks))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c chunk.Chunk) {
			defer wg.Done()
			defer func() { <-sem }()

			wav, err := synth.Speak(ctx, c.Text, voice, speed)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			pieces[i] = assemble.PieceAudio{Chunk: c, WAV: wav}
			logger.Info("synthesized chunk", "index", i+1, "of", len(chunks))
		}(i, c)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("voice: synthesize chunk %d: %w", i, err)
		}
	}
	return pieces, nil
}
