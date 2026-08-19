package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/placeholder/avatar/internal/config"
)

var rootFlags struct {
	configDir  string
	promptsDir string
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "avatar",
		Short:         "Generate a single portrait image for a scenario script's narrator",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&rootFlags.configDir, "config-dir", "configs", "directory containing settings.yaml and pricing.yaml")
	cmd.PersistentFlags().StringVar(&rootFlags.promptsDir, "prompts-dir", "prompts", "directory containing avatar.tmpl")

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

func loadAvatarTemplate(dir string) (*template.Template, error) {
	path := filepath.Join(dir, "avatar.tmpl")
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
		return "", fmt.Errorf("avatar: environment variable %s is not set (see settings.yaml's api_key_env)", s.APIKeyEnv)
	}
	return key, nil
}
