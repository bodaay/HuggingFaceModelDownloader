// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// RepoInfo represents detailed info about a downloaded repo.
type RepoInfo struct {
	Type         string         `json:"type"`
	Repo         string         `json:"repo"`
	Branch       string         `json:"branch"`
	Commit       string         `json:"commit"`
	TotalFiles   int            `json:"total_files"`
	TotalSize    int64          `json:"total_size"`
	TotalSizeHuman string       `json:"total_size_human"`
	StartedAt    string         `json:"started_at"`
	CompletedAt  string         `json:"completed_at"`
	Command      string         `json:"command"`
	FriendlyPath string         `json:"friendly_path"`
	CachePath    string         `json:"cache_path"`
	Files        []RepoFileInfo `json:"files"`
}

// RepoFileInfo represents a file in the repo.
type RepoFileInfo struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SizeHuman string `json:"size_human"`
	LFS       bool   `json:"lfs"`
	BlobPath  string `json:"blob_path"`
}

func newInfoCmd(ro *RootOpts) *cobra.Command {
	var cacheDir string
	var formatOut string

	cmd := &cobra.Command{
		Use:   "info <repo>",
		Short: "Show detailed info about a downloaded repo",
		Long: `Show detailed information about a specific downloaded repository.

The repo can be specified as:
  - Full name: owner/repo
  - Partial match: repo (if unique)

Examples:
  hfdownloader info TheBloke/Mistral-7B-Instruct-v0.2-GGUF
  hfdownloader info Mistral-7B-GGUF
  hfdownloader info --format json TheBloke/Mistral-7B-Instruct-v0.2-GGUF`,
		Args: cobra.ExactArgs(1),
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

			repoQuery := args[0]
			info, err := findRepoInfo(cacheDir, repoQuery)
			if err != nil {
				return err
			}

			// Output
			if formatOut == "json" || ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}

			// Human-readable output
			printRepoInfo(info)
			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface or HF_HOME)")
	cmd.Flags().StringVar(&formatOut, "format", "text", "Output format: text, json")

	return cmd
}

func findRepoInfo(cacheDir, query string) (*RepoInfo, error) {
	// Search for matching manifest
	var matches []string
	var matchManifests []*hfdownloader.DownloadManifest

	dirs := []string{
		filepath.Join(cacheDir, "models"),
		filepath.Join(cacheDir, "datasets"),
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || info.Name() != hfdownloader.ManifestFilename {
				return nil
			}

			manifest, err := hfdownloader.ReadManifest(path)
			if err != nil {
				return nil
			}

			// Check if query matches
			if matchesRepo(manifest.Repo, query) {
				matches = append(matches, path)
				matchManifests = append(matchManifests, manifest)
			}
			return nil
		})
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no repo matching %q found in cache", query)
	}
	if len(matches) > 1 {
		var names []string
		for _, m := range matchManifests {
			names = append(names, m.Repo)
		}
		return nil, fmt.Errorf("multiple repos match %q: %s\nPlease specify the full repo name", query, strings.Join(names, ", "))
	}

	manifest := matchManifests[0]
	manifestPath := matches[0]
	friendlyPath := filepath.Dir(manifestPath)

	// Build cache path
	cachePath := filepath.Join(cacheDir, manifest.RepoPath)

	// Build file list
	var files []RepoFileInfo
	for _, f := range manifest.Files {
		files = append(files, RepoFileInfo{
			Name:      f.Name,
			Size:      f.Size,
			SizeHuman: humanSize(f.Size),
			LFS:       f.LFS,
			BlobPath:  filepath.Join(cachePath, f.Blob),
		})
	}

	return &RepoInfo{
		Type:           manifest.Type,
		Repo:           manifest.Repo,
		Branch:         manifest.Branch,
		Commit:         manifest.Commit,
		TotalFiles:     manifest.TotalFiles,
		TotalSize:      manifest.TotalSize,
		TotalSizeHuman: humanSize(manifest.TotalSize),
		StartedAt:      manifest.StartedAt.Format("2006-01-02 15:04:05"),
		CompletedAt:    manifest.CompletedAt.Format("2006-01-02 15:04:05"),
		Command:        manifest.Command,
		FriendlyPath:   friendlyPath,
		CachePath:      cachePath,
		Files:          files,
	}, nil
}

func matchesRepo(repo, query string) bool {
	// Exact match
	if strings.EqualFold(repo, query) {
		return true
	}
	// Match repo name only (without owner)
	parts := strings.Split(repo, "/")
	if len(parts) == 2 && strings.EqualFold(parts[1], query) {
		return true
	}
	// Contains match (case-insensitive)
	if strings.Contains(strings.ToLower(repo), strings.ToLower(query)) {
		return true
	}
	return false
}

func printRepoInfo(info *RepoInfo) {
	fmt.Printf("Repository: %s\n", info.Repo)
	fmt.Printf("Type:       %s\n", info.Type)
	fmt.Printf("Branch:     %s\n", info.Branch)
	fmt.Printf("Commit:     %s\n", info.Commit)
	fmt.Printf("Files:      %d\n", info.TotalFiles)
	fmt.Printf("Size:       %s\n", info.TotalSizeHuman)
	fmt.Printf("Downloaded: %s\n", info.CompletedAt)
	fmt.Println()
	fmt.Printf("Friendly path: %s\n", info.FriendlyPath)
	fmt.Printf("Cache path:    %s\n", info.CachePath)

	if info.Command != "" && info.Command != "(regenerated from cache)" {
		fmt.Println()
		fmt.Printf("Command: %s\n", info.Command)
	}

	fmt.Println()
	fmt.Println("Files:")
	fmt.Printf("  %-50s  %10s  %s\n", "NAME", "SIZE", "LFS")
	fmt.Printf("  %-50s  %10s  %s\n", strings.Repeat("-", 50), "----------", "---")
	for _, f := range info.Files {
		name := f.Name
		if len(name) > 50 {
			name = "..." + name[len(name)-47:]
		}
		lfs := ""
		if f.LFS {
			lfs = "yes"
		}
		fmt.Printf("  %-50s  %10s  %s\n", name, f.SizeHuman, lfs)
	}
}
