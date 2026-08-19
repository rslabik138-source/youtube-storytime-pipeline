package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/placeholder/avatar/internal/portrait"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a generated portrait's metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}

			metaPath := filepath.Join(cfg.OutputDir, args[0], "meta.json")
			data, err := os.ReadFile(metaPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("avatar: no meta.json at %s — run `avatar generate %s` first", metaPath, args[0])
				}
				return fmt.Errorf("avatar: read %s: %w", metaPath, err)
			}
			var m portrait.Meta
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("avatar: parse %s: %w", metaPath, err)
			}

			fmt.Printf("id: %s\n", m.ID)
			if m.File != "" {
				fmt.Printf("file: %s\n", m.File)
			}
			if len(m.Files) > 0 {
				fmt.Printf("files: %s\n", strings.Join(m.Files, ", "))
			}
			fmt.Printf("provider: %s (%s)\n", m.Provider, m.Model)
			fmt.Printf("cost: $%.4f\n", m.CostUSD)
			fmt.Printf("prompt: %s\n", m.Prompt)
			return nil
		},
	}
}
