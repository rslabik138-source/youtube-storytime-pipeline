package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/store"
)

var rootFlags struct {
	configDir  string
	promptsDir string
	dbPath     string
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "gen",
		Short:         "Generate long-form scripts for the \"underestimated\" channel",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&rootFlags.configDir, "config-dir", "configs", "directory containing the *.yaml config files")
	cmd.PersistentFlags().StringVar(&rootFlags.promptsDir, "prompts-dir", "prompts", "directory containing the *.tmpl prompt templates")
	cmd.PersistentFlags().StringVar(&rootFlags.dbPath, "db", "", "override settings.yaml's db_path")

	cmd.AddCommand(
		newGenerateCmd(),
		newListCmd(),
		newShowCmd(),
		newRegenerateCmd(),
		newExportCmd(),
		newStatsCmd(),
		newValidateCmd(),
	)
	return cmd
}

func loadConfig(dir string) (*config.Config, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("load config from %s: %w", dir, err)
	}
	return cfg, nil
}

func openStoreFromConfig(cfg *config.Config, dbOverride string) (store.Store, error) {
	path := cfg.Settings.DBPath
	if dbOverride != "" {
		path = dbOverride
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory %s: %w", dir, err)
		}
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open store at %s: %w", path, err)
	}
	return st, nil
}
