// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// DefaultConfig returns the default configuration.
func DefaultConfig() map[string]any {
	return map[string]any{
		"output":              "Storage",
		"connections":         8,
		"max-active":          3,
		"multipart-threshold": "32MiB",
		"verify":              "size",
		"retries":             4,
		"backoff-initial":     "400ms",
		"backoff-max":         "10s",
		"token":               "",
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())

	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var (
		force      bool
		useYAML    bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file",
		Long: `Creates a default configuration file at ~/.config/hfdownloader.json (or .yaml)

The configuration file sets default values for all command flags.
CLI flags always override config file values.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("could not find home directory: %w", err)
			}

			configDir := filepath.Join(home, ".config")
			ext := ".json"
			if useYAML {
				ext = ".yaml"
			}
			configPath := filepath.Join(configDir, "hfdownloader"+ext)

			// Check if file exists
			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("config file already exists: %s\nUse --force to overwrite", configPath)
			}

			// Create config directory if needed
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				return fmt.Errorf("could not create config directory: %w", err)
			}

			// Write config
			cfg := DefaultConfig()
			var data []byte
			if useYAML {
				data, err = yaml.Marshal(cfg)
			} else {
				data, err = json.MarshalIndent(cfg, "", "  ")
			}
			if err != nil {
				return err
			}

			if err := os.WriteFile(configPath, data, 0o644); err != nil {
				return fmt.Errorf("could not write config file: %w", err)
			}

			fmt.Printf("✓ Created config file: %s\n", configPath)
			fmt.Println()
			fmt.Println("Edit this file to set your defaults. For example:")
			fmt.Println("  - Set your HuggingFace token")
			fmt.Println("  - Change default output directory")
			fmt.Println("  - Adjust connection settings")

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing config file")
	cmd.Flags().BoolVar(&useYAML, "yaml", false, "Create YAML config instead of JSON")

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := os.UserHomeDir()
			configPath := filepath.Join(home, ".config", "hfdownloader.json")

			if _, err := os.Stat(configPath); err != nil {
				fmt.Println("No config file found.")
				fmt.Printf("Run 'hfdownloader config init' to create one at:\n  %s\n", configPath)
				return nil
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				return err
			}

			fmt.Printf("Config file: %s\n\n", configPath)
			fmt.Println(string(data))

			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Run: func(cmd *cobra.Command, args []string) {
			home, _ := os.UserHomeDir()
			configPath := filepath.Join(home, ".config", "hfdownloader.json")
			fmt.Println(configPath)
		},
	}
}

