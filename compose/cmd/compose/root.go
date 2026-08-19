package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/placeholder/compose/internal/config"
)

var rootFlags struct {
	configDir string
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "compose",
		Short:         "Assemble the final composed video for a script",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&rootFlags.configDir, "config-dir", "configs", "directory containing settings.yaml and layout.yaml")

	cmd.AddCommand(newBuildCmd())
	return cmd
}

func loadSettings(dir string) (config.Settings, error) {
	return config.Load(filepath.Join(dir, "settings.yaml"))
}

func loadLayout(dir string) (config.Layout, error) {
	return config.LoadLayout(filepath.Join(dir, "layout.yaml"))
}

func printProgress(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}
