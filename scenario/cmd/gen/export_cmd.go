package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/scenario/internal/export"
)

func newExportCmd() *cobra.Command {
	var format string
	var out string
	var ssml bool

	cmd := &cobra.Command{
		Use:   "export <id>",
		Short: "Export a script as tts, timing, meta, or a bundle for the voiceover module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(rootFlags.configDir)
			if err != nil {
				return err
			}
			st, err := openStoreFromConfig(cfg, rootFlags.dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			script, err := st.GetScript(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if format == "bundle" {
				if out == "" {
					return fmt.Errorf("--out is required for --format bundle (it's a directory, not a file)")
				}
				scriptText, manifestJSON, err := export.Bundle(script)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(out, 0o755); err != nil {
					return fmt.Errorf("create %s: %w", out, err)
				}
				scriptPath := filepath.Join(out, "script.txt")
				if err := os.WriteFile(scriptPath, scriptText, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", scriptPath, err)
				}
				manifestPath := filepath.Join(out, "manifest.json")
				if err := os.WriteFile(manifestPath, manifestJSON, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", manifestPath, err)
				}
				fmt.Printf("wrote %s and %s\n", scriptPath, manifestPath)
				return nil
			}

			var data []byte
			switch format {
			case "tts":
				// Plain text by default — Kokoro and most local/offline TTS
				// engines don't support SSML. Pacing lives in the timing
				// manifest's pause_after_ms fields instead; pass --ssml for
				// an engine that does accept <break> tags.
				data = []byte(export.TTS(script, export.TTSOptions{
					ChapterBreakMS:   cfg.Settings.ChapterBreakMS,
					ParagraphBreakMS: cfg.Settings.ParagraphBreakMS,
					SSML:             ssml,
				}))
			case "timing":
				data, err = export.TimingJSON(script, cfg.Settings.WPM, cfg.Settings.ChapterBreakMS, cfg.Settings.ParagraphBreakMS)
				if err != nil {
					return err
				}
			case "meta":
				data, err = export.BuildMeta(script).JSON()
				if err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown --format %q (must be tts, timing, meta, or bundle)", format)
			}

			if out == "" {
				fmt.Println(string(data))
				return nil
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}
			fmt.Printf("wrote %s\n", out)
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "", "tts|timing|meta|bundle (required)")
	cmd.Flags().StringVar(&out, "out", "", "output file path (default: stdout) — for --format bundle, this is a DIRECTORY")
	cmd.Flags().BoolVar(&ssml, "ssml", false, "tts format only: emit <break> tags for a TTS engine that supports SSML (default: plain text, e.g. for Kokoro)")
	cmd.MarkFlagRequired("format")
	return cmd
}
