package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/avatar/internal/config"
	"github.com/placeholder/avatar/internal/cutout"
	"github.com/placeholder/avatar/internal/imagegen"
	"github.com/placeholder/avatar/internal/manifest"
	"github.com/placeholder/avatar/internal/portrait"
	"github.com/placeholder/avatar/internal/prompt"
)

func newGenerateCmd() *cobra.Command {
	var providerFlag string
	var dryRun bool
	var variants int
	var noCutout bool

	cmd := &cobra.Command{
		Use:   "generate <id>",
		Short: "Generate a portrait image for one scenario script's narrator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			cfg, err := loadSettings(rootFlags.configDir)
			if err != nil {
				return err
			}

			bundleDir := filepath.Join(cfg.ScenarioBundleDir, id)
			m, err := manifest.Load(bundleDir)
			if err != nil {
				return fmt.Errorf(
					"load scenario bundle at %s (did you run `gen export %s --format bundle --out %s` in the scenario module?): %w",
					bundleDir, id, bundleDir, err)
			}

			tmpl, err := loadAvatarTemplate(rootFlags.promptsDir)
			if err != nil {
				return err
			}
			renderedPrompt, err := prompt.Render(tmpl, *m)
			if err != nil {
				return err
			}

			outDir := filepath.Join(cfg.OutputDir, id)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("avatar: create %s: %w", outDir, err)
			}

			if dryRun {
				promptPath := filepath.Join(outDir, "prompt.txt")
				if err := os.WriteFile(promptPath, []byte(renderedPrompt), 0o644); err != nil {
					return fmt.Errorf("avatar: write %s: %w", promptPath, err)
				}
				fmt.Printf("wrote %s\n", promptPath)
				return nil
			}

			if variants <= 0 {
				variants = 1
			}

			key, err := apiKey(cfg)
			if err != nil {
				return err
			}
			pricing, err := loadPricing(rootFlags.configDir)
			if err != nil {
				return err
			}

			gen, estimateModel, err := buildGenerator(cfg, providerFlag, key)
			if err != nil {
				return err
			}

			// Budget check uses the model that will actually be tried
			// first as the cost estimate (matches settings.yaml's
			// documented behavior) — real cost is recomputed after
			// generation from whichever provider actually served each
			// image, since a failover can mean a cheaper or pricier mix
			// than the estimate assumed.
			estimateCost, _ := pricing.CostFor(estimateModel)
			if err := portrait.CheckBudget(estimateCost, variants, cfg.MaxCostUSD); err != nil {
				return err
			}

			runCutout := cfg.Cutout.IsEnabled() && !noCutout

			opts := imagegen.Options{AspectRatio: cfg.AspectRatio}
			var files []string
			var totalCost float64
			var lastProvider, lastModel string
			for i := 1; i <= variants; i++ {
				img, err := gen.Generate(cmd.Context(), renderedPrompt, opts)
				if err != nil {
					return fmt.Errorf("avatar: generate variant %d: %w", i, err)
				}

				// Matte the background out to a transparent PNG — the only
				// file we keep. rembg is the tool of record (best hair edges);
				// the opaque original is intentionally not written.
				png := img.PNG
				if runCutout {
					cut, err := cutout.RemoveBackground(cmd.Context(),
						cutout.Options{RembgCmd: cfg.Cutout.RembgCmd, Model: cfg.Cutout.Model}, png)
					if err != nil {
						return fmt.Errorf("avatar: cut out variant %d background: %w", i, err)
					}
					png = cut
				}

				fileName := "portrait.png"
				if variants > 1 {
					fileName = fmt.Sprintf("variant-%d.png", i)
				}
				outPath := filepath.Join(outDir, fileName)
				if err := os.WriteFile(outPath, png, 0o644); err != nil {
					return fmt.Errorf("avatar: write %s: %w", outPath, err)
				}
				files = append(files, fileName)

				if cost, ok := pricing.CostFor(img.Model); ok {
					totalCost += cost
				}
				lastProvider, lastModel = img.Provider, img.Model
			}

			meta := portrait.Meta{ID: id, Prompt: renderedPrompt, Provider: lastProvider, Model: lastModel, CostUSD: totalCost}
			if variants > 1 {
				meta.Files = files
			} else {
				meta.File = files[0]
			}
			metaJSON, err := meta.JSON()
			if err != nil {
				return err
			}
			metaPath := filepath.Join(outDir, "meta.json")
			if err := os.WriteFile(metaPath, metaJSON, 0o644); err != nil {
				return fmt.Errorf("avatar: write %s: %w", metaPath, err)
			}

			fmt.Printf("wrote %d image(s) to %s (cost: $%.4f)\n", len(files), outDir, totalCost)
			return nil
		},
	}

	cmd.Flags().StringVar(&providerFlag, "provider", "", "gemini|imagen (default: gemini with imagen fallback)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "build the prompt without calling any API, write to prompt.txt")
	cmd.Flags().IntVar(&variants, "variants", 1, "generate N candidate images (variant-1.png, ...) instead of one portrait.png")
	cmd.Flags().BoolVar(&noCutout, "no-cutout", false, "skip background removal, write the raw opaque image (debug the generation prompt)")
	return cmd
}

// buildGenerator picks the generator for --provider (gemini-only,
// imagen-only, or the default Gemini-primary/Imagen-fallback chain) and
// returns the model whose price should stand in for the pre-flight budget
// estimate — the first one that will actually be tried.
func buildGenerator(cfg config.Settings, providerFlag, key string) (imagegen.Generator, string, error) {
	gemini := imagegen.NewGeminiClient(cfg.BaseURL, cfg.GeminiModel, key)
	imagenC := imagegen.NewImagenClient(cfg.BaseURL, cfg.ImagenModel, key)

	switch providerFlag {
	case "":
		return imagegen.NewFailover(gemini, imagenC), cfg.GeminiModel, nil
	case "gemini":
		return gemini, cfg.GeminiModel, nil
	case "imagen":
		return imagenC, cfg.ImagenModel, nil
	default:
		return nil, "", fmt.Errorf("avatar: unknown --provider %q (must be gemini or imagen)", providerFlag)
	}
}
