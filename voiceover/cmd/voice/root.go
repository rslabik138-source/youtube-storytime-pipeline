package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/voiceover/internal/catalog"
	"github.com/placeholder/voiceover/internal/config"
	"github.com/placeholder/voiceover/internal/store"
)

var rootFlags struct {
	configDir string
	dbPath    string
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "voice",
		Short:         "Synthesize a scenario script's audio via a local Kokoro instance",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&rootFlags.configDir, "config-dir", "configs", "directory containing settings.yaml and voices.yaml")
	cmd.PersistentFlags().StringVar(&rootFlags.dbPath, "db", "", "override settings.yaml's db_path")

	cmd.AddCommand(
		newSpeakCmd(),
		newListVoicesCmd(),
		newSampleCmd(),
		newShowCmd(),
		newStatsCmd(),
	)
	return cmd
}

func loadSettings(dir string) (config.Settings, error) {
	return config.Load(filepath.Join(dir, "settings.yaml"))
}

func loadCatalog(dir string) (catalog.Catalog, error) {
	return catalog.Load(filepath.Join(dir, "voices.yaml"))
}

func openStoreFromSettings(s config.Settings, dbOverride string) (store.Store, error) {
	path := s.DBPath
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
