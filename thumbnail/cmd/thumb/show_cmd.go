package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/thumbnail/internal/thumb"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a generated thumbnail's metadata",
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
					return fmt.Errorf("thumb: no meta.json at %s — run `thumb generate %s` first", metaPath, args[0])
				}
				return fmt.Errorf("thumb: read %s: %w", metaPath, err)
			}
			var m thumb.Meta
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("thumb: parse %s: %w", metaPath, err)
			}

			fmt.Printf("id: %s\n", m.ID)
			fmt.Printf("face: %s\n", m.FaceID)
			fmt.Printf("text_model: %s\n", m.TextModel)
			fmt.Printf("cost: $%.4f\n", m.CostUSD)
			for _, v := range m.Variants {
				fmt.Printf("\n%s:\n", v.File)
				for _, l := range v.Lines {
					fmt.Printf("  [%s] %s\n", l.Color, l.Text)
				}
				fmt.Printf("  [red plate] %s\n", v.FinalLine)
			}
			return nil
		},
	}
}
