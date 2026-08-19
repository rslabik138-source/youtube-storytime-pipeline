package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate stats across every recorded voiceover",
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

			s, err := st.Stats(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("total voiceovers: %d\n", s.TotalVoiceovers)
			fmt.Printf("total duration: %.1fs (%s)\n", s.TotalSeconds, formatDuration(s.TotalSeconds))
			fmt.Printf("total size: %.1f MB\n", float64(s.TotalSizeBytes)/1024/1024)
			return nil
		},
	}
}
