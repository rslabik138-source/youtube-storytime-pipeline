package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/voiceover/internal/assemble"
	"github.com/placeholder/voiceover/internal/catalog"
	"github.com/placeholder/voiceover/internal/chunk"
	"github.com/placeholder/voiceover/internal/kokoro"
	"github.com/placeholder/voiceover/internal/manifest"
	"github.com/placeholder/voiceover/internal/store"
)

func newSpeakCmd() *cobra.Command {
	var voiceFlag string
	var speedFlag float64
	var dryRun bool
	var stitchFlag string
	var sampleSeams bool

	cmd := &cobra.Command{
		Use:   "speak <id>",
		Short: "Synthesize one scenario script's full audio",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}

			bundleDir := filepath.Join(cfg.ScenarioBundleDir, id)
			bundle, err := manifest.Load(bundleDir)
			if err != nil {
				return fmt.Errorf(
					"load scenario bundle at %s (did you run `gen export %s --format bundle --out %s` in the scenario module?): %w",
					bundleDir, id, bundleDir, err)
			}

			chunks, err := chunk.Split(bundle, chunk.Options{
				MaxChars: cfg.ChunkMaxChars, ParagraphPauseMs: cfg.PauseParagraphMs, ChapterPauseMs: cfg.PauseChapterMs,
			})
			if err != nil {
				return err
			}

			if dryRun {
				return printDryRun(chunks, cfg.Concurrency)
			}

			// Fail fast on a missing ffmpeg/ffprobe before spending any
			// (possibly slow, GPU-bound) Kokoro calls.
			if err := assemble.CheckFFmpeg(); err != nil {
				return err
			}

			st, err := openStoreFromSettings(cfg, rootFlags.dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			voice := voiceFlag
			if voice == "" {
				cat, err := loadCatalog(rootFlags.configDir)
				if err != nil {
					return err
				}
				used, err := st.RecentUsedVoices(cmd.Context(), 3)
				if err != nil {
					return fmt.Errorf("voice: recent used voices: %w", err)
				}
				v, err := catalog.Select(cat, bundle.Manifest.Narrator, used)
				if err != nil {
					return err
				}
				voice = v.ID
				logger.Info("auto-selected voice", "voice", voice, "narrator_sex", bundle.Manifest.Narrator.Sex, "narrator_age", bundle.Manifest.Narrator.Age)
			}

			stitch, err := parseStitchFlag(stitchFlag)
			if err != nil {
				return err
			}

			speed := cfg.Speed
			if speedFlag > 0 {
				speed = speedFlag
			}

			synth := kokoro.NewClient(cfg.KokoroURL, nil, 3)

			logger.Info("synthesizing", "id", id, "voice", voice, "chunks", len(chunks), "concurrency", cfg.Concurrency)
			pieces, err := synthesizeAll(cmd.Context(), synth, chunks, voice, speed, cfg.Concurrency, logger)
			if err != nil {
				return err
			}

			outDir := filepath.Join(cfg.OutputDir, id)
			wavPath := filepath.Join(outDir, "voice.wav")
			chunksDir := filepath.Join(outDir, "chunks")

			timing, err := assemble.Assemble(cmd.Context(), pieces, bundle.Manifest.Chapters, wavPath, id, voice, assemble.Options{
				Stitch: stitch, LoudnessLUFS: cfg.LoudnessLUFS, KeepChunks: cfg.KeepChunks, ChunksDir: chunksDir,
			})
			if err != nil {
				return err
			}

			timingJSON, err := json.MarshalIndent(timing, "", "  ")
			if err != nil {
				return fmt.Errorf("voice: marshal timing.json: %w", err)
			}
			timingPath := filepath.Join(outDir, "timing.json")
			if err := os.WriteFile(timingPath, timingJSON, 0o644); err != nil {
				return fmt.Errorf("voice: write %s: %w", timingPath, err)
			}

			info, err := os.Stat(wavPath)
			if err != nil {
				return fmt.Errorf("voice: stat %s: %w", wavPath, err)
			}
			if err := st.RecordVoiceover(cmd.Context(), store.VoiceoverSummary{
				ScriptID: id, Voice: voice, TotalSeconds: timing.TotalSeconds, SizeBytes: info.Size(),
			}); err != nil {
				return fmt.Errorf("voice: record voiceover: %w", err)
			}

			fmt.Printf("wrote %s (%.1fs) and %s\n", wavPath, timing.TotalSeconds, timingPath)

			if sampleSeams {
				seamsDir := filepath.Join(outDir, "seams")
				paths, err := assemble.SampleSeams(cmd.Context(), wavPath, timing, 3, seamsDir)
				if err != nil {
					return err
				}
				fmt.Println("seam samples (5s before/after each boundary):")
				for _, p := range paths {
					fmt.Println(" ", p)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&voiceFlag, "voice", "", "override automatic voice selection (skips catalog.Select entirely)")
	cmd.Flags().Float64Var(&speedFlag, "speed", 0, "override settings.yaml's speed")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "split into chunks and estimate render time, no Kokoro calls")
	cmd.Flags().StringVar(&stitchFlag, "stitch", "", "builtin|custom (default: custom — see README for why)")
	cmd.Flags().BoolVar(&sampleSeams, "sample-seams", false, "also render 3 random chunk-boundary samples for listening")
	return cmd
}

func parseStitchFlag(s string) (assemble.StitchMode, error) {
	switch s {
	case "", string(assemble.StitchCustom):
		return assemble.StitchCustom, nil
	case string(assemble.StitchBuiltin):
		return assemble.StitchBuiltin, nil
	default:
		return "", fmt.Errorf("voice: unknown --stitch %q (must be builtin or custom)", s)
	}
}

// printDryRun reports chunk count and a ROUGH render-time guess — not a
// measurement. Real per-chunk/total durations only exist after an actual
// render (see assemble.Timing, written from ffprobe on real audio).
func printDryRun(chunks []chunk.Chunk, concurrency int) error {
	if concurrency <= 0 {
		concurrency = 1
	}
	totalChars := 0
	for _, c := range chunks {
		totalChars += len(c.Text)
	}
	const estSecondsPerChunk = 2.0 // rough guess for local GPU synthesis latency per call
	estRenderSeconds := float64(len(chunks)) * estSecondsPerChunk / float64(concurrency)

	fmt.Printf("chunks: %d\n", len(chunks))
	fmt.Printf("total characters: %d\n", totalChars)
	fmt.Printf("estimated render time: ~%.0fs (rough guess based on chunk count and concurrency, not measured — actual time depends on your GPU)\n", estRenderSeconds)
	return nil
}
