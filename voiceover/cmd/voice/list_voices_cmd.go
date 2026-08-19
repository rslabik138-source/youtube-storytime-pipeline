package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/placeholder/voiceover/internal/kokoro"
)

func newListVoicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-voices",
		Short: "List voices Kokoro currently serves, and flag any not yet described in voices.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}
			cat, err := loadCatalog(rootFlags.configDir)
			if err != nil {
				return err
			}

			synth := kokoro.NewClient(cfg.KokoroURL, nil, 3)
			live, err := synth.Voices(cmd.Context())
			if err != nil {
				return fmt.Errorf("voice: list-voices: %w", err)
			}

			known := make(map[string]bool, len(cat.Voices))
			for _, v := range cat.Voices {
				known[v.ID] = true
			}

			fmt.Println("voices Kokoro currently serves:")
			var undescribed []string
			for _, id := range live {
				marker := "  "
				if !known[id] {
					marker = "* "
					undescribed = append(undescribed, id)
				}
				fmt.Printf("%s%s\n", marker, id)
			}
			if len(undescribed) > 0 {
				fmt.Println()
				fmt.Println("* = not yet described in voices.yaml. Listen to each (voice sample --voice <id> --text \"...\") and add an entry:")
				for _, id := range undescribed {
					fmt.Printf("  %s\n", id)
				}
			}
			return nil
		},
	}
}
