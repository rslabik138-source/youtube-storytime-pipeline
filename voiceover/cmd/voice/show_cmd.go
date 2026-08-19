package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a voiceover's metadata: voice, duration, size",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}
			st, err := openStoreFromSettings(cfg, rootFlags.dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			v, err := st.GetVoiceover(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("id: %s\n", v.ScriptID)
			fmt.Printf("voice: %s\n", v.Voice)
			fmt.Printf("created: %s\n", v.CreatedAt.Format(time.RFC3339))
			fmt.Printf("duration: %.1fs (%s)\n", v.TotalSeconds, formatDuration(v.TotalSeconds))
			fmt.Printf("size: %.1f MB\n", float64(v.SizeBytes)/1024/1024)
			return nil
		},
	}
}

func formatDuration(seconds float64) string {
	return time.Duration(seconds * float64(time.Second)).Round(time.Second).String()
}
