package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	var chapter int

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a script's metadata, or one chapter's text",
		Args:  cobra.ExactArgs(1),
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

			script, err := st.GetScript(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if chapter > 0 {
				ch, ok := script.ChapterByIndex(chapter)
				if !ok {
					return fmt.Errorf("script %s has no chapter %d", script.ID, chapter)
				}
				fmt.Printf("chapter %d (%s), %d words:\n\n%s\n", ch.Index, ch.Beat, len(strings.Fields(ch.Text)), ch.DisplayText)
				return nil
			}

			fmt.Printf("id: %s\n", script.ID)
			fmt.Printf("title: %s\n", script.Title)
			fmt.Printf("status: %s\n", script.Status)
			fmt.Printf("profession: %s\n", script.Seed.Profession)
			fmt.Printf("words: %d\n", script.WordCount)
			fmt.Printf("quality mean: %.1f (min %.1f)\n", script.Quality.Mean(), script.Quality.Min())
			fmt.Printf("chapters: %d\n", len(script.Chapters))
			for _, ch := range script.Chapters {
				fmt.Printf("  %2d  %-16s %4d words\n", ch.Index, ch.Beat, len(strings.Fields(ch.Text)))
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&chapter, "chapter", 0, "show only this chapter's text")
	return cmd
}
