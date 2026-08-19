package main

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	textTemplate "text/template"

	"github.com/spf13/cobra"

	"github.com/placeholder/thumbnail/internal/config"
)

var rootFlags struct {
	configDir    string
	promptsDir   string
	templatesDir string
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "thumb",
		Short:         "Generate a 1280x720 YouTube thumbnail for a scenario script",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&rootFlags.configDir, "config-dir", "configs", "directory containing settings.yaml, faces.yaml, and pricing.yaml")
	cmd.PersistentFlags().StringVar(&rootFlags.promptsDir, "prompts-dir", "prompts", "directory containing thumbnail.tmpl")
	cmd.PersistentFlags().StringVar(&rootFlags.templatesDir, "templates-dir", "templates", "directory containing thumbnail.html")

	cmd.AddCommand(
		newGenerateCmd(),
		newShowCmd(),
	)
	return cmd
}

func loadSettings(dir string) (config.Settings, error) {
	return config.Load(filepath.Join(dir, "settings.yaml"))
}

func loadPricing(dir string) (config.Pricing, error) {
	return config.LoadPricing(filepath.Join(dir, "pricing.yaml"))
}

func loadFaces(dir string) (config.FaceLibrary, error) {
	return config.LoadFaces(filepath.Join(dir, "faces.yaml"))
}

func loadOpeners(dir string) (config.OpenerLibrary, error) {
	return config.LoadOpeners(filepath.Join(dir, "thumbnail_openers.yaml"))
}

func loadThumbnailPromptTemplate(dir string) (*textTemplate.Template, error) {
	path := filepath.Join(dir, "thumbnail.tmpl")
	tmpl, err := textTemplate.ParseFiles(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return tmpl, nil
}

func loadThumbnailHTMLTemplate(dir string) (*template.Template, error) {
	path := filepath.Join(dir, "thumbnail.html")
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return tmpl, nil
}

// apiKey reads settings.APIKeyEnv from the environment, with a clear error
// (never the key value itself) if it's unset.
func apiKey(s config.Settings) (string, error) {
	key := os.Getenv(s.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("thumb: environment variable %s is not set (see settings.yaml's api_key_env)", s.APIKeyEnv)
	}
	return key, nil
}
