package main

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/placeholder/scenario/internal/generate"
	"github.com/placeholder/scenario/internal/store"
	"github.com/placeholder/scenario/internal/story"
)

func newGenerateCmd() *cobra.Command {
	var dryRun bool
	var profession string
	var seedFlag int64
	var provider string
	var resume bool

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new script, or continue one with --resume",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if resume {
				if dryRun || cmd.Flags().Changed("profession") || cmd.Flags().Changed("seed") {
					return fmt.Errorf("--resume can't be combined with --dry-run, --profession, or --seed — those only apply to a fresh script")
				}
			}

			cfg, err := loadConfig(rootFlags.configDir)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
			orch, st, err := buildOrchestrator(cfg, rootFlags.dbPath, rootFlags.promptsDir, provider, logger)
			if err != nil {
				return err
			}
			defer st.Close()

			var script *story.Script
			if resume {
				pending, err := st.ListScripts(cmd.Context(), store.ListFilter{Status: story.StatusPending, Limit: 1})
				if err != nil {
					return err
				}
				if len(pending) == 0 {
					return fmt.Errorf("no pending script to resume")
				}

				before, err := st.GetScript(cmd.Context(), pending[0].ID)
				if err != nil {
					return err
				}
				fmt.Printf("resuming %s (%s, %d/%d chapters so far)\n",
					before.ID, before.Title, len(before.Chapters), len(cfg.Chapters.Chapters))

				script, err = orch.Resume(cmd.Context(), pending[0].ID)
				if err != nil {
					return err
				}
			} else {
				rngSeed := seedFlag
				if rngSeed == 0 {
					rngSeed = time.Now().UnixNano()
				}
				rng := rand.New(rand.NewSource(rngSeed))

				script, err = orch.Generate(cmd.Context(), rng, generate.Options{Profession: profession, DryRun: dryRun})
				if err != nil {
					return err
				}
				fmt.Printf("rng seed: %d\n", rngSeed)
			}

			fmt.Printf("id: %s\n", script.ID)
			fmt.Printf("title: %s\n", script.Title)
			fmt.Printf("profession: %s\n", script.Seed.Profession)
			fmt.Printf("status: %s\n", script.Status)
			fmt.Printf("words: %d\n", script.WordCount)
			if !dryRun {
				fmt.Printf("quality mean: %.1f (min %.1f)\n", script.Quality.Mean(), script.Quality.Min())
			}
			fmt.Println()
			fmt.Println("usage this run:")
			printUsageTable(os.Stdout, cfg.Pricing, script.Usage)
			checkThinkingDisabled(os.Stdout, script.Usage, float64(script.WordCount))
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "seed + bible only — no chapters, no continuity, no review, nothing saved")
	cmd.Flags().StringVar(&profession, "profession", "", "force a specific profession instead of drawing randomly")
	cmd.Flags().Int64Var(&seedFlag, "seed", 0, "RNG seed for reproducible axis draws (0 = random, printed either way)")
	cmd.Flags().StringVar(&provider, "provider", "", "use only this provider (by name in settings.yaml) instead of the full failover chain")
	cmd.Flags().BoolVar(&resume, "resume", false, "continue the most recent pending script from wherever chapter generation left off")
	return cmd
}
