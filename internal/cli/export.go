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

func newExportCmd(ro *RootOpts) *cobra.Command {
	var (
		cacheDir  string
		revision  string
		mode      string
		isDataset bool
		filters   []string
	)

	cmd := &cobra.Command{
		Use:   "export <repo> <dest>",
		Short: "Export a downloaded repo as plain files (no re-download)",
		Long: `Export a repo from the HuggingFace cache as plain files in <dest>, in the
repo's own layout — for tools that need real files (LM Studio, Ollama
Modelfiles, llama.cpp, ...). Nothing is downloaded.

Files are copied, so the export is independent of the cache. To save disk
space, --mode hardlink makes real files that share the cache's data (same
drive only; editing one in place changes the cache too).

Also works for caches written without links (Windows before v3.4.0, where
files ended up only as blobs/<sha256>), using the download manifest.

Examples:
  hfdownloader export TheBloke/Mistral-7B-Instruct-v0.2-GGUF ~/lmstudio/models/TheBloke/Mistral-7B
  hfdownloader export unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF ./qwen -F q4_k_m
  hfdownloader export --dataset nyu-mll/glue ./glue --mode copy`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, dest := args[0], args[1]
			if !hfdownloader.IsValidModelName(repo) {
				return fmt.Errorf("invalid repo id %q (expected owner/name)", repo)
			}
			linkMode, err := hfdownloader.ParseLinkMode(mode)
			if err != nil {
				return err
			}

			// Determine cache directory: CLI flag > config file > HF_HOME > default
			if cacheDir == "" {
				if cfg := loadConfigMap(); cfg != nil {
					if v, ok := cfg["cache-dir"].(string); ok && v != "" {
						cacheDir = v
					}
				}
			}
			cache := hfdownloader.NewHFCache(cacheDir, hfdownloader.DefaultStaleTimeout)

			repoType := hfdownloader.RepoTypeModel
			if isDataset {
				repoType = hfdownloader.RepoTypeDataset
			}
			repoDir, err := cache.Repo(repo, repoType)
			if err != nil {
				return err
			}
			if !isDataset {
				// Fall back to a cached dataset of that name.
				if _, err := os.Stat(repoDir.Path()); os.IsNotExist(err) {
					if ds, err := cache.Repo(repo, hfdownloader.RepoTypeDataset); err == nil {
						if _, err := os.Stat(ds.Path()); err == nil {
							repoDir = ds
						}
					}
				}
			}

			res, err := repoDir.Export(dest, hfdownloader.ExportOptions{
				Revision: revision,
				Mode:     linkMode,
				Filters:  filters,
			})
			if res != nil && ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(res); encErr != nil {
					return encErr
				}
			}
			if err != nil {
				return err
			}
			if !ro.JSONOut && !ro.Quiet {
				fmt.Printf("Exported %s (%s) to %s\n", repo, res.Commit, res.Dest)
				fmt.Printf("  Files:      %d (%s)\n", res.Files, humanSize(res.Bytes))
				if res.Hardlinked > 0 {
					fmt.Printf("  Hardlinked: %d (no extra disk space)\n", res.Hardlinked)
				}
				if res.Copied > 0 {
					fmt.Printf("  Copied:     %d\n", res.Copied)
				}
				if res.Symlinked > 0 {
					fmt.Printf("  Symlinked:  %d\n", res.Symlinked)
				}
				if res.Unchanged > 0 {
					fmt.Printf("  Unchanged:  %d (already exported)\n", res.Unchanged)
				}
				if res.Hardlinked > 0 {
					fmt.Println("  Note: hardlinked files share their data with the cache; editing one in place")
					fmt.Println("        changes the cached copy too.")
				}
				if res.FromBlobs {
					fmt.Println("  Note: the cache had no snapshot links; files were found via the download manifest.")
					fmt.Println("        Run `hfdownloader rebuild` to repair the cache for Python/HF tools.")
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface or HF_HOME)")
	cmd.Flags().StringVarP(&revision, "revision", "b", "", "Branch, tag or commit to export (default: main)")
	cmd.Flags().StringVar(&mode, "mode", "copy", "copy (independent files), hardlink (shares the cache's disk space; same drive only), or symlink")
	cmd.Flags().BoolVar(&isDataset, "dataset", false, "The repo is a dataset")
	cmd.Flags().StringSliceVarP(&filters, "filters", "F", nil, "Export only matching weight/data files (exact match, like download -F --exact); other files are always exported")
	return cmd
}
