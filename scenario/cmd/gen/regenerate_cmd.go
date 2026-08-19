package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

func newRegenerateCmd() *cobra.Command {
	var chapter int
	var provider string

	cmd := &cobra.Command{
		Use:   "regenerate <id>",
		Short: "Regenerate a single chapter of an existing script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if chapter <= 0 {
				return fmt.Errorf("--chapter N is required")
			}

			cfg, err := loadConfig(rootFlags.configDir)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
			orch, st, err := buildOrchestrator(cfg, rootFlags.dbPath, rootFlags.promptsDir, provider, logger)
			if err != nil {
				return err
			}
			defer st.Close()

			script, err := orch.RegenerateChapter(cmd.Context(), args[0], chapter)
			if err != nil {
				return err
			}

			fmt.Printf("regenerated chapter %d of script %s\n", chapter, script.ID)
			fmt.Printf("words now: %d\n", script.WordCount)
			return nil
		},
	}

	cmd.Flags().IntVar(&chapter, "chapter", 0, "chapter index to regenerate (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "use only this provider instead of the full failover chain")
	return cmd
}
