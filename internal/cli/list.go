// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// ListEntry represents a single repo in the list output.
type ListEntry struct {
	Type       string `json:"type"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Files      int    `json:"files"`
	Size       int64  `json:"size"`
	SizeHuman  string `json:"size_human"`
	Downloaded string `json:"downloaded"`
	Path       string `json:"path"`
}

func newListCmd(ro *RootOpts) *cobra.Command {
	var cacheDir string
	var filterType string
	var sortBy string
	var formatOut string
	var scan bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List downloaded models and datasets in the cache",
		Long: `List all models and datasets that have been downloaded to the HuggingFace cache.

By default, information is read from hfd.yaml manifest files created during download.
Use --scan to scan the cache directory structure instead (for repos without manifests).

Examples:
  hfdownloader list                     # List all repos (from manifests)
  hfdownloader list --scan              # Scan cache structure (no manifest needed)
  hfdownloader list --type model        # List only models
  hfdownloader list --type dataset      # List only datasets
  hfdownloader list --sort size         # Sort by size (largest first)
  hfdownloader list --format json       # Output as JSON`,
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

			var entries []ListEntry
			var err error

			if scan {
				entries, err = scanCacheStructure(cacheDir, filterType)
			} else {
				entries, err = scanManifests(cacheDir, filterType)
			}
			if err != nil {
				return err
			}

			// Sort entries
			sortEntries(entries, sortBy)

			// Output
			if formatOut == "json" || ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			// Table output
			if len(entries) == 0 {
				fmt.Println("No downloaded repos found.")
				fmt.Printf("Cache directory: %s\n", cacheDir)
				if !scan {
					fmt.Println("\nNote: Only repos with hfd.yaml manifests are listed.")
					fmt.Println("Use --scan to scan cache structure (for repos downloaded by other tools).")
				}
				return nil
			}

			printTable(entries)
			fmt.Printf("\nTotal: %d repos\n", len(entries))
			if !scan {
				fmt.Println("(Use --scan to include repos without manifests)")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface or HF_HOME)")
	cmd.Flags().StringVar(&filterType, "type", "", "Filter by type: model, dataset")
	cmd.Flags().StringVar(&sortBy, "sort", "name", "Sort by: name, size, date")
	cmd.Flags().StringVar(&formatOut, "format", "table", "Output format: table, json")
	cmd.Flags().BoolVar(&scan, "scan", false, "Scan cache structure instead of reading manifests")

	return cmd
}

// scanCacheStructure scans hub/ directory structure directly (for repos without manifests)
func scanCacheStructure(cacheDir, filterType string) ([]ListEntry, error) {
	var entries []ListEntry

	hubDir := filepath.Join(cacheDir, "hub")
	if _, err := os.Stat(hubDir); os.IsNotExist(err) {
		return entries, nil
	}

	items, err := os.ReadDir(hubDir)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		if !item.IsDir() {
			continue
		}

		name := item.Name()
		var repoType string
		var repoName string

		if strings.HasPrefix(name, "models--") {
			repoType = "model"
			repoName = strings.TrimPrefix(name, "models--")
		} else if strings.HasPrefix(name, "datasets--") {
			repoType = "dataset"
			repoName = strings.TrimPrefix(name, "datasets--")
		} else {
			continue
		}

		// Filter by type if specified
		if filterType != "" && repoType != filterType {
			continue
		}

		// Convert owner--repo to owner/repo
		repoName = strings.Replace(repoName, "--", "/", 1)

		// Get size by walking blobs directory
		blobsDir := filepath.Join(hubDir, name, "blobs")
		var totalSize int64
		var fileCount int
		filepath.Walk(blobsDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				totalSize += info.Size()
				fileCount++
			}
			return nil
		})

		// Try to read commit from refs/main
		var commit string
		refPath := filepath.Join(hubDir, name, "refs", "main")
		if data, err := os.ReadFile(refPath); err == nil {
			commit = strings.TrimSpace(string(data))
		}

		// Get modification time
		info, _ := item.Info()
		var downloaded string
		if info != nil {
			downloaded = info.ModTime().Format("2006-01-02")
		}

		entry := ListEntry{
			Type:       repoType,
			Repo:       repoName,
			Branch:     "main",
			Commit:     shortCommit(commit),
			Files:      fileCount,
			Size:       totalSize,
			SizeHuman:  humanSize(totalSize),
			Downloaded: downloaded,
			Path:       filepath.Join(hubDir, name),
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func scanManifests(cacheDir, filterType string) ([]ListEntry, error) {
	var entries []ListEntry

	// Scan both models/ and datasets/ directories
	dirs := []string{
		filepath.Join(cacheDir, "models"),
		filepath.Join(cacheDir, "datasets"),
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		// Walk looking for hfd.yaml files
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}
			if info.IsDir() {
				return nil
			}
			if info.Name() != hfdownloader.ManifestFilename {
				return nil
			}

			manifest, err := hfdownloader.ReadManifest(path)
			if err != nil {
				return nil // Skip invalid manifests
			}

			// Filter by type if specified
			if filterType != "" && manifest.Type != filterType {
				return nil
			}

			entry := ListEntry{
				Type:       manifest.Type,
				Repo:       manifest.Repo,
				Branch:     manifest.Branch,
				Commit:     shortCommit(manifest.Commit),
				Files:      manifest.TotalFiles,
				Size:       manifest.TotalSize,
				SizeHuman:  humanSize(manifest.TotalSize),
				Downloaded: manifest.CompletedAt.Format("2006-01-02"),
				Path:       filepath.Dir(path),
			}
			entries = append(entries, entry)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return entries, nil
}

func sortEntries(entries []ListEntry, sortBy string) {
	switch strings.ToLower(sortBy) {
	case "size":
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Size > entries[j].Size // Largest first
		})
	case "date":
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Downloaded > entries[j].Downloaded // Newest first
		})
	default: // "name"
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Repo < entries[j].Repo
		})
	}
}

func printTable(entries []ListEntry) {
	// Calculate column widths
	maxRepo := 4 // "REPO"
	for _, e := range entries {
		if len(e.Repo) > maxRepo {
			maxRepo = len(e.Repo)
		}
	}
	if maxRepo > 50 {
		maxRepo = 50
	}

	// Header
	fmt.Printf("%-7s  %-*s  %-7s  %-10s  %5s  %10s  %s\n",
		"TYPE", maxRepo, "REPO", "COMMIT", "DOWNLOADED", "FILES", "SIZE", "BRANCH")
	fmt.Printf("%-7s  %-*s  %-7s  %-10s  %5s  %10s  %s\n",
		"-------", maxRepo, strings.Repeat("-", maxRepo), "-------", "----------", "-----", "----------", "------")

	// Rows
	for _, e := range entries {
		repo := e.Repo
		if len(repo) > maxRepo {
			repo = repo[:maxRepo-3] + "..."
		}
		fmt.Printf("%-7s  %-*s  %-7s  %-10s  %5d  %10s  %s\n",
			e.Type, maxRepo, repo, e.Commit, e.Downloaded, e.Files, e.SizeHuman, e.Branch)
	}
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func humanSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
