package main

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/placeholder/thumbnail/internal/config"
	"github.com/placeholder/thumbnail/internal/facepicker"
	"github.com/placeholder/thumbnail/internal/manifest"
	"github.com/placeholder/thumbnail/internal/openerpicker"
	"github.com/placeholder/thumbnail/internal/render"
	"github.com/placeholder/thumbnail/internal/textgen"
	"github.com/placeholder/thumbnail/internal/thumb"
)

// defaultVariants matches the workflow this module is built around: a
// human picks the best of several candidates (see newGenerateCmd's --variants
// help) rather than trusting one automated result.
const defaultVariants = 4

// faceHistoryFileName and openerHistoryFileName live directly under
// OutputDir (not per-script) — rotation of both the face and the opening
// construction is a channel-wide property, not something scoped to one
// video.
const (
	faceHistoryFileName   = "face_history.json"
	openerHistoryFileName = "opener_history.json"
)

// openerAvoidRecent / openerHistoryKeep: never reuse an opening construction
// within the last 3 thumbnails, and remember the last 20 so that window is
// always available even across many runs.
const (
	openerAvoidRecent   = 3
	openerHistoryRecent = 5
	openerHistoryKeep   = 20
)

// mobileContrastFloor is the RMS-contrast (0-255) below which text is
// treated as unreadable at phone-feed size — calibrated conservatively, a
// clean high-stroke thumbnail scores well above it. See render.MobileCheck.
const mobileContrastFloor = 34.0

func newGenerateCmd() *cobra.Command {
	var faceFlag string
	var portraitFlag string
	var bgFlag string
	var dryRun bool
	var variants int
	var mobileCheck bool

	cmd := &cobra.Command{
		Use:   "generate <id>",
		Short: "Generate thumbnail text + composite it onto a standing portrait",
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
				return fmt.Errorf("thumb: load scenario bundle at %s (did you run `gen export %s --format bundle --out %s`?): %w",
					bundleDir, id, bundleDir, err)
			}

			promptTmpl, err := loadThumbnailPromptTemplate(rootFlags.promptsDir)
			if err != nil {
				return err
			}

			openers, err := loadOpeners(rootFlags.configDir)
			if err != nil {
				return err
			}

			outDir := filepath.Join(cfg.OutputDir, id)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("thumb: create %s: %w", outDir, err)
			}

			// Opener rotation is channel-wide: its history lives under
			// OutputDir, not the per-id outDir. picker walks it to choose a
			// construction not used in the last few thumbnails.
			openerHistory := openerpicker.History{Path: filepath.Join(cfg.OutputDir, openerHistoryFileName)}
			recentIDs, err := openerHistory.Last(openerHistoryRecent)
			if err != nil {
				return err
			}

			if dryRun {
				opener, err := openerpicker.Pick(openers, recentIDs, openerAvoidRecent)
				if err != nil {
					return err
				}
				prompt, err := textgen.Render(promptTmpl, *m, openerData(openers, opener, recentIDs), nil)
				if err != nil {
					return err
				}
				promptPath := filepath.Join(outDir, "prompt.txt")
				if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
					return fmt.Errorf("thumb: write %s: %w", promptPath, err)
				}
				fmt.Printf("wrote %s (opener: %s, no API call, no render)\n", promptPath, opener.ID)
				return nil
			}

			if variants <= 0 {
				variants = defaultVariants
			}

			// Portrait source: --portrait pins the video's own avatar cutout
			// (so the thumbnail face matches the person in the video), which
			// bypasses the stock faces.yaml library and its rotation entirely.
			// Without it, fall back to the rotation-aware stock face picker.
			var portraitPNG []byte
			faceID := "video-avatar"
			if portraitFlag != "" {
				portraitPNG, err = os.ReadFile(portraitFlag)
				if err != nil {
					return fmt.Errorf("thumb: read --portrait %s: %w", portraitFlag, err)
				}
			} else {
				faces, err := loadFaces(rootFlags.configDir)
				if err != nil {
					return err
				}
				face, err := pickFace(faces, faceFlag, m.Narrator.Sex, m.Narrator.Age, cfg.OutputDir)
				if err != nil {
					return err
				}
				portraitPNG, err = os.ReadFile(face.File)
				if err != nil {
					return fmt.Errorf("thumb: read face portrait %s (face %q from faces.yaml): %w", face.File, face.ID, err)
				}
				faceID = face.ID
			}

			key, err := apiKey(cfg)
			if err != nil {
				return err
			}
			pricing, err := loadPricing(rootFlags.configDir)
			if err != nil {
				return err
			}
			costPerCall, _ := pricing.CostFor(cfg.TextModel)
			if err := thumb.CheckBudget(costPerCall, variants, cfg.MaxCostUSD); err != nil {
				return err
			}

			htmlTmpl, err := loadThumbnailHTMLTemplate(rootFlags.templatesDir)
			if err != nil {
				return err
			}

			client := textgen.NewClient(cfg.BaseURL, key)
			renderer := render.ChromedpRenderer{
				ExecPath: cfg.ChromePath,
				Timeout:  time.Duration(cfg.RenderTimeoutSec) * time.Second,
			}
			portraitURI := render.EncodePortrait(portraitPNG)

			var backgroundURI template.URL
			if bgFlag != "" {
				bgBytes, err := os.ReadFile(bgFlag)
				if err != nil {
					return fmt.Errorf("thumb: read --bg %s: %w", bgFlag, err)
				}
				backgroundURI = render.EncodeImage(bgBytes)
			}

			var meta thumb.Meta
			meta.ID = id
			meta.FaceID = faceID
			meta.TextModel = cfg.TextModel

			// recent tracks opener IDs used so far, most-recent-first: the
			// persisted history seeds it, and each variant prepends its own
			// pick so variants within one run also vary their opening.
			recent := append([]string(nil), recentIDs...)

			for i := 1; i <= variants; i++ {
				opener, err := openerpicker.Pick(openers, recent, openerAvoidRecent)
				if err != nil {
					return err
				}

				result, err := textgen.Generate(cmd.Context(), client, promptTmpl, *m, cfg.TextModel, openerData(openers, opener, recent))
				if err != nil {
					return fmt.Errorf("thumb: variant %d: generate text: %w", i, err)
				}

				lines := make([]render.LineView, len(result.Text.Lines))
				for j, l := range result.Text.Lines {
					lines[j] = render.LineView{Text: l.Text, Color: l.Color}
				}
				html, err := render.BuildHTML(htmlTmpl, render.ViewData{
					Lines: lines, FinalLine: result.Text.FinalLine,
					PortraitDataURI: portraitURI, BackgroundDataURI: backgroundURI,
					BadgeEnabled: cfg.Badge.Enabled, BadgeText: cfg.Badge.Text,
				})
				if err != nil {
					return fmt.Errorf("thumb: variant %d: build html: %w", i, err)
				}

				png, err := renderer.Render(cmd.Context(), html)
				if err != nil {
					return fmt.Errorf("thumb: variant %d: render: %w", i, err)
				}

				fileName := fmt.Sprintf("variant-%d.png", i)
				if err := os.WriteFile(filepath.Join(outDir, fileName), png, 0o644); err != nil {
					return fmt.Errorf("thumb: write %s: %w", fileName, err)
				}

				if mobileCheck {
					if err := writeMobileCheck(outDir, i, png, result.Text); err != nil {
						return fmt.Errorf("thumb: variant %d: mobile-check: %w", i, err)
					}
				}

				meta.Variants = append(meta.Variants, thumb.VariantMeta{
					File: fileName, OpenerID: opener.ID, Lines: result.Text.Lines, FinalLine: result.Text.FinalLine,
				})
				if callCost, ok := pricing.CostFor(result.Model); ok {
					meta.CostUSD += callCost
				}

				// Persist and advance rotation only after a variant fully
				// succeeds, so a mid-run failure doesn't burn an opener.
				if err := openerHistory.Record(opener.ID, openerHistoryKeep); err != nil {
					return err
				}
				recent = append([]string{opener.ID}, recent...)
			}

			metaJSON, err := meta.JSON()
			if err != nil {
				return err
			}
			metaPath := filepath.Join(outDir, "meta.json")
			if err := os.WriteFile(metaPath, metaJSON, 0o644); err != nil {
				return fmt.Errorf("thumb: write %s: %w", metaPath, err)
			}

			fmt.Printf("wrote %d variant(s) to %s (face: %s, cost: $%.4f)\n", variants, outDir, faceID, meta.CostUSD)
			return nil
		},
	}

	cmd.Flags().StringVar(&faceFlag, "face", "", "pin to this face id from faces.yaml instead of picking automatically")
	cmd.Flags().StringVar(&portraitFlag, "portrait", "", "path to the video's own avatar cutout PNG to use as the thumbnail face (matches the person in the video); bypasses the stock faces.yaml library")
	cmd.Flags().StringVar(&bgFlag, "bg", "", "path to a background image (a frame from the video's clip) rendered full-frame and blurred behind the text; omit for a flat dark backdrop")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "write the thumbnail-text prompt to prompt.txt without calling any API or rendering")
	cmd.Flags().IntVar(&variants, "variants", defaultVariants, "number of different texts to generate on the same portrait")
	cmd.Flags().BoolVar(&mobileCheck, "mobile-check", false, "also downscale each variant to phone-feed size (168x94) and flag any whose text is unreadable there")

	return cmd
}

// openerData builds the textgen.Opener fed into the prompt: the chosen
// construction line 1 must follow, plus the human-readable patterns of the
// recently-used openers so the model has an explicit avoid-list.
func openerData(lib config.OpenerLibrary, chosen config.Opener, recentIDs []string) textgen.Opener {
	var recentPatterns []string
	for _, id := range recentIDs {
		if o, ok := lib.ByID(id); ok {
			recentPatterns = append(recentPatterns, o.Pattern)
		}
	}
	return textgen.Opener{ChosenPattern: chosen.Pattern, Recent: recentPatterns}
}

// writeMobileCheck downscales one variant to phone-feed size, writes it as
// variant-N-mobile.png for a human to eyeball, and prints a PASS/WARN line:
// text is flagged when its downscaled contrast falls below the floor or the
// word count is high enough that the composition simply has too many words
// to read at that size (the spec's own diagnosis).
func writeMobileCheck(outDir string, i int, png []byte, text textgen.ThumbnailText) error {
	mobilePNG, contrast, err := render.MobileCheck(png)
	if err != nil {
		return err
	}
	mobileName := fmt.Sprintf("variant-%d-mobile.png", i)
	if err := os.WriteFile(filepath.Join(outDir, mobileName), mobilePNG, 0o644); err != nil {
		return fmt.Errorf("thumb: write %s: %w", mobileName, err)
	}

	words := len(strings.Fields(text.FinalLine))
	for _, l := range text.Lines {
		words += len(strings.Fields(l.Text))
	}

	status := "PASS"
	if contrast < mobileContrastFloor || words > textgen.MaxTotalWords {
		status = "WARN"
	}
	fmt.Printf("  mobile-check variant %d: %s (contrast %.1f, floor %.1f, %d words) -> %s\n",
		i, status, contrast, mobileContrastFloor, words, mobileName)
	return nil
}

// pickFace resolves faceOverride (if set) via an exact faces.yaml lookup,
// otherwise runs the sex/age-matched, rotation-aware facepicker.Pick and
// records the result in outputDir's face history so the next call's
// rotation sees it.
func pickFace(faces config.FaceLibrary, faceOverride, sex string, age int, outputDir string) (config.Face, error) {
	if faceOverride != "" {
		f, ok := faces.ByID(faceOverride)
		if !ok {
			return config.Face{}, fmt.Errorf("thumb: --face %q not found in faces.yaml", faceOverride)
		}
		return f, nil
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return config.Face{}, fmt.Errorf("thumb: create %s: %w", outputDir, err)
	}
	history := facepicker.History{Path: filepath.Join(outputDir, faceHistoryFileName)}
	recent, err := history.Last(2)
	if err != nil {
		return config.Face{}, err
	}
	face, err := facepicker.Pick(faces, sex, age, recent)
	if err != nil {
		return config.Face{}, err
	}
	if err := history.Record(face.ID, 10); err != nil {
		return config.Face{}, err
	}
	return face, nil
}
