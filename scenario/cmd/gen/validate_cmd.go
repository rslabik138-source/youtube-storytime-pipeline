package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/placeholder/scenario/internal/story"
)

// newValidateCmd runs every validator (bible + per-chapter + whole-script)
// against an already-saved script straight from the DB — zero LLM calls.
// This is how validator thresholds get calibrated for $0 instead of
// through a full paid run: tweak a threshold, re-run `gen validate`
// against the same saved script, see the violation list change instantly.
func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <id>",
		Short: "Run every validator against an already-saved script, with no LLM calls",
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

			usedNames, err := st.RecentUsedNames(cmd.Context(), 30)
			if err != nil {
				return fmt.Errorf("load recent used names: %w", err)
			}

			chapterCfg := story.DefaultChapterValidatorConfig(cfg.BannedPhrases.Phrases)

			var violations []story.Violation
			violations = append(violations, story.ValidateBible(script.Bible, story.BibleValidatorConfig{UsedNames: usedNames})...)
			violations = append(violations, story.ValidateScript(script, chapterCfg)...)

			printValidationReport(script.ID, violations)
			return nil
		},
	}
}

// printValidationReport groups violations by chapter (0 = bible-level),
// prints each with its severity, and ends with a blocking/warning count so
// a threshold change's effect is visible at a glance.
func printValidationReport(scriptID string, violations []story.Violation) {
	fmt.Printf("validation report for %s\n", scriptID)
	if len(violations) == 0 {
		fmt.Println("  no violations found")
		return
	}

	byChapter := map[int][]story.Violation{}
	var order []int
	for _, v := range violations {
		if _, ok := byChapter[v.Chapter]; !ok {
			order = append(order, v.Chapter)
		}
		byChapter[v.Chapter] = append(byChapter[v.Chapter], v)
	}

	var errorCount, warningCount int
	for _, idx := range order {
		label := fmt.Sprintf("chapter %d", idx)
		if idx == 0 {
			label = "bible / whole-script"
		}
		fmt.Printf("\n%s:\n", label)
		for _, v := range byChapter[idx] {
			fmt.Printf("  [%s] %s: %s\n", v.Severity, v.Rule, v.Message)
			if v.Severity == story.SeverityError {
				errorCount++
			} else {
				warningCount++
			}
		}
	}

	fmt.Printf("\n%d blocking violation(s), %d warning(s)\n", errorCount, warningCount)
}
