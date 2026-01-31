// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// SyncOutput is the JSON output format for sync results.
type SyncOutput struct {
	ReposScanned    int      `json:"repos_scanned"`
	SymlinksCreated int      `json:"symlinks_created"`
	SymlinksUpdated int      `json:"symlinks_updated"`
	OrphansRemoved  int      `json:"orphans_removed,omitempty"`
	Errors          []string `json:"errors,omitempty"`
}

func newRebuildCmd(ro *RootOpts) *cobra.Command {
	var cacheDir string
	var clean bool
	var writeScript bool

	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Regenerate friendly view (models/, datasets/) from hub cache",
		Long: `Rebuild regenerates the friendly view symlinks from the hub cache.

This is useful after:
  - Manual changes to the hub cache
  - Downloading files with the official HuggingFace Python library
  - Recovering from interrupted downloads
  - Cleaning up orphaned symlinks

The friendly view provides human-readable paths:
  models/{owner}/{repo}/{file}
  datasets/{owner}/{repo}/{file}

These symlink to the actual files in:
  hub/models--{owner}--{repo}/snapshots/{commit}/{file}

A standalone rebuild.sh script is automatically written to the cache directory
during downloads. Use --write-script to manually update this script.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine cache directory: CLI flag > config file > HF_HOME > default
			if cacheDir == "" {
				if cfg := loadConfigMap(); cfg != nil {
					if v, ok := cfg["cache-dir"].(string); ok && v != "" {
						cacheDir = v
					}
				}
			}
			if cacheDir == "" {
				cacheDir = hfdownloader.DefaultCacheDir()
			}

			cache := hfdownloader.NewHFCache(cacheDir, hfdownloader.DefaultStaleTimeout)

			// Write/update the standalone script if requested
			if writeScript {
				scriptPath, err := cache.WriteRebuildScript()
				if err != nil {
					return fmt.Errorf("write script: %w", err)
				}
				if !ro.Quiet && !ro.JSONOut {
					fmt.Printf("Wrote rebuild script: %s\n", scriptPath)
				}
			}

			opts := hfdownloader.SyncOptions{
				Clean:   clean,
				Verbose: ro.Verbose,
			}

			if !ro.Quiet && !ro.JSONOut {
				fmt.Printf("Rebuilding friendly view from: %s\n", cacheDir)
				if clean {
					fmt.Println("Cleaning orphaned symlinks...")
				}
			}

			result, err := cache.Sync(opts)
			if err != nil {
				return fmt.Errorf("rebuild failed: %w", err)
			}

			// Output results
			if ro.JSONOut {
				out := SyncOutput{
					ReposScanned:    result.ReposScanned,
					SymlinksCreated: result.SymlinksCreated,
					SymlinksUpdated: result.SymlinksUpdated,
					OrphansRemoved:  result.OrphansRemoved,
				}
				for _, e := range result.Errors {
					out.Errors = append(out.Errors, e.Error())
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			if !ro.Quiet {
				fmt.Printf("\nRebuild complete:\n")
				fmt.Printf("  Repos scanned:     %d\n", result.ReposScanned)
				fmt.Printf("  Symlinks created:  %d\n", result.SymlinksCreated)
				fmt.Printf("  Symlinks updated:  %d\n", result.SymlinksUpdated)
				if clean {
					fmt.Printf("  Orphans removed:   %d\n", result.OrphansRemoved)
				}
				if len(result.Errors) > 0 {
					fmt.Printf("  Errors:            %d\n", len(result.Errors))
					for _, e := range result.Errors {
						fmt.Printf("    - %s\n", e)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface or HF_HOME)")
	cmd.Flags().BoolVar(&clean, "clean", false, "Remove orphaned symlinks (broken symlinks pointing to deleted files)")
	cmd.Flags().BoolVar(&writeScript, "write-script", false, "Write/update the standalone rebuild.sh script")

	return cmd
}
