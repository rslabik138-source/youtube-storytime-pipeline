package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/voiceover/internal/kokoro"
)

func newSampleCmd() *cobra.Command {
	var voiceFlag string
	var textFlag string
	var speedFlag float64
	var outFlag string

	cmd := &cobra.Command{
		Use:   "sample",
		Short: "Synthesize one short piece of text for listening (no chunking, no assembly)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if textFlag == "" {
				return fmt.Errorf("voice: --text is required")
			}
			if voiceFlag == "" {
				return fmt.Errorf("voice: --voice is required")
			}
			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}
			speed := cfg.Speed
			if speedFlag > 0 {
				speed = speedFlag
			}

			synth := kokoro.NewClient(cfg.KokoroURL, nil, 3)
			wav, err := synth.Speak(cmd.Context(), textFlag, voiceFlag, speed)
			if err != nil {
				return fmt.Errorf("voice: sample: %w", err)
			}

			out := outFlag
			if out == "" {
				out = filepath.Join(cfg.OutputDir, "samples", voiceFlag+".wav")
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return fmt.Errorf("voice: create %s: %w", filepath.Dir(out), err)
			}
			if err := os.WriteFile(out, wav, 0o644); err != nil {
				return fmt.Errorf("voice: write %s: %w", out, err)
			}
			fmt.Printf("wrote %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&voiceFlag, "voice", "", "voice ID to sample (required)")
	cmd.Flags().StringVar(&textFlag, "text", "", "text to synthesize (required)")
	cmd.Flags().Float64Var(&speedFlag, "speed", 0, "override settings.yaml's speed")
	cmd.Flags().StringVar(&outFlag, "out", "", "output WAV path (default: output/samples/<voice>.wav)")
	return cmd
}
