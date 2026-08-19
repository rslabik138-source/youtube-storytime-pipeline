package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/placeholder/compose/internal/compose"
	"github.com/placeholder/compose/internal/ffmpeg"
	"github.com/placeholder/compose/internal/rembg"
)

func newBuildCmd() *cobra.Command {
	var noMusic, noSubs bool
	var preview float64

	cmd := &cobra.Command{
		Use:   "build <id>",
		Short: "Render the final composed video for a script id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}
			layout, err := loadLayout(rootFlags.configDir)
			if err != nil {
				return err
			}

			rembgRunner := rembg.CLIRunner{Cmd: cfg.RembgCmd}
			ffmpegRunner := ffmpeg.CLIRunner{FFmpegCmd: cfg.FFmpegCmd, FFprobeCmd: cfg.FFprobeCmd}

			result, err := compose.Build(cmd.Context(), cfg, layout, rembgRunner, ffmpegRunner, compose.Options{
				ID: id, NoMusic: noMusic, NoSubs: noSubs, PreviewSeconds: preview,
			}, printProgress)
			if err != nil {
				return err
			}

			fmt.Printf("wrote %s (%s, %.0fs, encoder: %s)\n", result.OutputPath, "1920x1080", result.DurationSeconds, result.EncoderUsed)
			return nil
		},
	}

	cmd.Flags().BoolVar(&noMusic, "no-music", false, "skip background music entirely (no ducking chain, no music file required)")
	cmd.Flags().BoolVar(&noSubs, "no-subs", false, "skip burning in subtitles")
	cmd.Flags().Float64Var(&preview, "preview", 0, "render only the first N seconds, for checking the composition without a full render")

	return cmd
}
