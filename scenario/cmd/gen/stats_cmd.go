package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate stats across every stored script, including a cost/token breakdown by role and model",
		Args:  cobra.NoArgs,
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

			stats, err := st.Stats(cmd.Context())
			if err != nil {
				return err
			}

			fmt.Printf("total:       %d\n", stats.TotalScripts)
			fmt.Printf("accepted:    %d\n", stats.AcceptedScripts)
			fmt.Printf("  w/warnings: %d (validation rounds exhausted — review never ran, skim before publishing)\n", stats.AcceptedWithWarningsScripts)
			fmt.Printf("rejected:    %d\n", stats.RejectedScripts)
			fmt.Printf("pending:     %d\n", stats.PendingScripts)
			fmt.Printf("avg words:   %.0f\n", stats.AverageWordCount)
			fmt.Printf("avg quality: %.1f\n", stats.AverageQuality)
			fmt.Println()
			fmt.Println("usage by role and model (across every script):")
			printUsageTable(os.Stdout, cfg.Pricing, stats.Usage)
			checkThinkingDisabled(os.Stdout, stats.Usage, stats.AverageWordCount*float64(stats.TotalScripts))
			return nil
		},
	}
}
