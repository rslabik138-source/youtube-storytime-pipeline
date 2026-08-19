package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/placeholder/scenario/internal/store"
	"github.com/placeholder/scenario/internal/story"
)

func newListCmd() *cobra.Command {
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List generated scripts",
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

			summaries, err := st.ListScripts(cmd.Context(), store.ListFilter{Status: story.Status(status), Limit: limit})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tPROFESSION\tSTATUS\tWORDS\tQUALITY")
			for _, s := range summaries {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%.1f\n", s.ID, s.Title, s.Profession, s.Status, s.WordCount, s.QualityMean)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "filter by status: accepted|rejected|pending")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum results (0 = no limit)")
	return cmd
}
