package assemble

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/placeholder/voiceover/internal/chunk"
	"github.com/placeholder/voiceover/internal/manifest"
)

func TestMain(m *testing.M) {
	if err := CheckFFmpeg(); err != nil {
		// These tests exec real ffmpeg/ffprobe (no Docker, no network —
		// just the local binaries) to verify Assemble's actual output,
		// per the brief's own "usually caught faster with a real ffmpeg
		// than by mocking it" expectation. Skip cleanly if unavailable
		// rather than failing everything with a confusing error.
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// toneWAV builds a real, valid WAV (16-bit PCM mono, 24kHz) of a quiet
// 220Hz tone lasting d, sandwiched between brief silent padding — a stand-
// in for real synthesized speech. kokoro.FakeSynth's own contract is pure
// silence (fine for kokoro/chunk-duration tests), but trimSilence's whole
// job is stripping silence PADDING around real content: fed pure silence
// end to end, it correctly strips the entire clip, which would make every
// duration assertion here meaningless. A quiet tone in the middle gives
// trimSilence something realistic to trim around.
func toneWAV(d time.Duration) []byte {
	const sampleRate = 24000
	const freq = 220.0
	const amplitude = 6000
	const padding = 30 * time.Millisecond

	total := padding + d + padding
	numSamples := int(total.Seconds() * sampleRate)
	padSamples := int(padding.Seconds() * sampleRate)

	samples := make([]int16, numSamples)
	for i := padSamples; i < numSamples-padSamples && i < numSamples; i++ {
		t := float64(i) / sampleRate
		samples[i] = int16(amplitude * math.Sin(2*math.Pi*freq*t))
	}

	dataSize := len(samples) * 2
	buf := new(bytes.Buffer)
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1))
	binary.Write(buf, binary.LittleEndian, uint16(1))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	binary.Write(buf, binary.LittleEndian, samples)
	return buf.Bytes()
}

func fakePieces(t *testing.T, specs []struct {
	text       string
	chapterIdx int
	pause      time.Duration
}) []PieceAudio {
	t.Helper()
	out := make([]PieceAudio, len(specs))
	for i, s := range specs {
		// ~60ms per character of tone, roughly proportional to text length
		// like real speech would be.
		wav := toneWAV(time.Duration(len(s.text)) * 60 * time.Millisecond)
		out[i] = PieceAudio{
			Chunk: chunk.Chunk{Index: i, Text: s.text, ChapterIdx: s.chapterIdx, PauseAfter: s.pause},
			WAV:   wav,
		}
	}
	return out
}

func TestAssembleWritesValidOutputWAVAt44100MonoS16(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"This is the first chunk of chapter one.", 1, 350 * time.Millisecond},
		{"This is the second chunk, still chapter one.", 1, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "voice.wav")
	timing, err := Assemble(context.Background(), pieces, chapters, outPath, "script-1", "af_bella", Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected a non-empty output file, err=%v", err)
	}
	if timing.TotalSeconds <= 0 {
		t.Fatalf("expected a positive TotalSeconds, got %v", timing.TotalSeconds)
	}

	dur, err := probeDuration(context.Background(), outPath)
	if err != nil {
		t.Fatalf("probeDuration: %v", err)
	}
	if math.Abs(dur-timing.TotalSeconds) > 0.05 {
		t.Fatalf("expected timing.TotalSeconds to match a fresh ffprobe measurement, got %v vs %v", timing.TotalSeconds, dur)
	}
}

func TestAssembleSumOfChunkDurationsWithinToleranceOfTotal(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"First paragraph sentence in chapter one.", 1, 350 * time.Millisecond},
		{"Second paragraph sentence in chapter one.", 1, 700 * time.Millisecond},
		{"First paragraph sentence in chapter two.", 2, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}, {Index: 2, Beat: "pivot"}}

	dir := t.TempDir()
	timing, err := Assemble(context.Background(), pieces, chapters, filepath.Join(dir, "voice.wav"), "script-1", "af_bella", Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	sum := 0.0
	for i, c := range timing.Chunks {
		sum += c.End - c.Start
		if i < len(pieces)-1 {
			sum += pieces[i].Chunk.PauseAfter.Seconds()
		}
	}
	if math.Abs(sum-timing.TotalSeconds) > 0.5 {
		t.Fatalf("expected sum of chunk durations+pauses (%v) within 0.5s of total_seconds (%v)", sum, timing.TotalSeconds)
	}
}

// TestAssembleManyPiecesTriggersBatchedJoin covers join's maxJoinBatch path:
// a script long enough to need more than one filter_complex pass (hit for
// real 2026-08-17 on a 178-chunk script, where a single inline
// -filter_complex graph pushed ffmpeg's command line past what Windows'
// CreateProcess accepts — see joinBatched's doc comment). More pieces than
// maxJoinBatch must still produce one correct, fully-joined output.
func TestAssembleManyPiecesTriggersBatchedJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises real ffmpeg over 3x maxJoinBatch pieces — slow, skip in -short")
	}
	const n = maxJoinBatch*2 + 7 // spans multiple batches, last batch not full
	specs := make([]struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}, n)
	for i := range specs {
		pause := time.Duration(0)
		if i%4 == 0 {
			pause = 120 * time.Millisecond // some real pauses, so batch boundaries land on both crossfades and pauses
		}
		specs[i] = struct {
			text       string
			chapterIdx int
			pause      time.Duration
		}{text: "Short chunk.", chapterIdx: 1, pause: pause}
	}
	pieces := fakePieces(t, specs)
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "voice.wav")
	timing, err := Assemble(context.Background(), pieces, chapters, outPath, "script-1", "af_bella", Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(timing.Chunks) != n {
		t.Fatalf("expected %d chunks in timing.json, got %d", n, len(timing.Chunks))
	}

	dur, err := probeDuration(context.Background(), outPath)
	if err != nil {
		t.Fatalf("probeDuration: %v", err)
	}
	if math.Abs(dur-timing.TotalSeconds) > 0.5 {
		t.Fatalf("expected timing.TotalSeconds (%v) close to a fresh ffprobe measurement (%v)", timing.TotalSeconds, dur)
	}
}

func TestAssembleChapterTimingSpansItsOwnChunksOnly(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"Chapter one chunk A.", 1, 0},
		{"Chapter one chunk B.", 1, 700 * time.Millisecond},
		{"Chapter two chunk A.", 2, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}, {Index: 2, Beat: "pivot"}}

	dir := t.TempDir()
	timing, err := Assemble(context.Background(), pieces, chapters, filepath.Join(dir, "voice.wav"), "script-1", "af_bella", Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(timing.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d: %+v", len(timing.Chapters), timing.Chapters)
	}
	ch1, ch2 := timing.Chapters[0], timing.Chapters[1]
	if ch1.Index != 1 || ch1.Beat != "hook" || ch1.Start != 0 {
		t.Fatalf("unexpected chapter 1 timing: %+v", ch1)
	}
	if ch2.Index != 2 || ch2.Beat != "pivot" {
		t.Fatalf("unexpected chapter 2 timing: %+v", ch2)
	}
	if ch2.Start < ch1.End {
		t.Fatalf("expected chapter 2 to start at or after chapter 1 ends, got ch1.End=%v ch2.Start=%v", ch1.End, ch2.Start)
	}
}

func TestAssembleEmptyPiecesReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, err := Assemble(context.Background(), nil, nil, filepath.Join(dir, "voice.wav"), "s1", "af_bella", Options{})
	if err == nil {
		t.Fatalf("expected an error for zero pieces")
	}
}

func TestAssembleBuiltinStitchAlsoProducesValidOutput(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"Chunk one for builtin stitching.", 1, 0},
		{"Chunk two for builtin stitching.", 1, 350 * time.Millisecond},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "voice.wav")
	timing, err := Assemble(context.Background(), pieces, chapters, outPath, "script-1", "af_bella", Options{Stitch: StitchBuiltin})
	if err != nil {
		t.Fatalf("Assemble (builtin): %v", err)
	}
	if timing.TotalSeconds <= 0 {
		t.Fatalf("expected a positive duration, got %v", timing.TotalSeconds)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected the output file to exist: %v", err)
	}
}

func TestAssembleKeepChunksLeavesChunksDirBehind(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"Only one chunk here.", 1, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	chunksDir := filepath.Join(dir, "chunks")
	_, err := Assemble(context.Background(), pieces, chapters, filepath.Join(dir, "voice.wav"), "s1", "af_bella",
		Options{ChunksDir: chunksDir, KeepChunks: true})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	entries, err := os.ReadDir(chunksDir)
	if err != nil {
		t.Fatalf("expected chunksDir to survive when KeepChunks is true: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected chunksDir to contain leftover files")
	}
}

func TestAssembleWithoutKeepChunksCleansUp(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"Only one chunk here.", 1, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	chunksDir := filepath.Join(dir, "chunks")
	_, err := Assemble(context.Background(), pieces, chapters, filepath.Join(dir, "voice.wav"), "s1", "af_bella",
		Options{ChunksDir: chunksDir, KeepChunks: false})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if _, err := os.Stat(chunksDir); !os.IsNotExist(err) {
		t.Fatalf("expected chunksDir to be removed when KeepChunks is false, stat err=%v", err)
	}
}

func TestTimingJSONRoundTrips(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"One chunk for JSON round trip.", 1, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	timing, err := Assemble(context.Background(), pieces, chapters, filepath.Join(dir, "voice.wav"), "s1", "af_bella", Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	b, err := json.Marshal(timing)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Timing
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != timing.ID || got.Voice != timing.Voice || len(got.Chunks) != len(timing.Chunks) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, timing)
	}
}

func TestSampleSeamsProducesRequestedCount(t *testing.T) {
	pieces := fakePieces(t, []struct {
		text       string
		chapterIdx int
		pause      time.Duration
	}{
		{"First chunk of several for seam sampling.", 1, 0},
		{"Second chunk of several for seam sampling.", 1, 350 * time.Millisecond},
		{"Third chunk of several for seam sampling.", 1, 0},
		{"Fourth chunk of several for seam sampling.", 1, 0},
	})
	chapters := []manifest.Chapter{{Index: 1, Beat: "hook"}}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "voice.wav")
	timing, err := Assemble(context.Background(), pieces, chapters, outPath, "s1", "af_bella", Options{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	seamsDir := filepath.Join(dir, "seams")
	paths, err := SampleSeams(context.Background(), outPath, timing, 3, seamsDir)
	if err != nil {
		t.Fatalf("SampleSeams: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("expected 3 seam samples, got %d", len(paths))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected seam file to exist: %v", err)
		}
	}
}

func TestSampleSeamsTooFewChunksReturnsError(t *testing.T) {
	timing := Timing{Chunks: []ChunkTiming{{Index: 0, Start: 0, End: 1}}}
	if _, err := SampleSeams(context.Background(), "irrelevant.wav", timing, 3, t.TempDir()); err == nil {
		t.Fatalf("expected an error with fewer than 2 chunks")
	}
}

func TestTrimSilenceShortensSilencePaddedAudio(t *testing.T) {
	// toneWAV already pads 30ms of silence on each side of the tone —
	// trimming should measurably shorten it.
	padded := toneWAV(1 * time.Second)
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.wav")
	outPath := filepath.Join(dir, "out.wav")
	if err := os.WriteFile(inPath, padded, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	if err := trimSilence(context.Background(), inPath, outPath); err != nil {
		t.Fatalf("trimSilence: %v", err)
	}

	before, err := probeDuration(context.Background(), inPath)
	if err != nil {
		t.Fatalf("probeDuration(before): %v", err)
	}
	after, err := probeDuration(context.Background(), outPath)
	if err != nil {
		t.Fatalf("probeDuration(after): %v", err)
	}
	if after >= before {
		t.Fatalf("expected trimming to shorten the clip: before=%v after=%v", before, after)
	}
	// Should still keep essentially all of the 1s tone itself, just minus
	// the ~60ms of padding.
	if after < 0.9 {
		t.Fatalf("expected the trimmed clip to still be close to 1s (just padding removed), got %v", after)
	}
}
