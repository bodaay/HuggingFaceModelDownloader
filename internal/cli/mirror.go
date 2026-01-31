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

func newMirrorCmd(ro *RootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mirror",
		Short: "Sync HuggingFace cache between locations",
		Long: `Mirror synchronizes HuggingFace caches between locations.

Use cases:
  - Sync models from office server to local machine
  - Export models to USB for airgapped deployment
  - Backup your model cache to NAS

Targets are named destinations you configure once:
  hfdownloader mirror target add office /mnt/nas/hfcache
  hfdownloader mirror target add airgap /media/usb/hfcache

Then sync using target names:
  hfdownloader mirror diff office     # Show what's different
  hfdownloader mirror push office     # Local → office
  hfdownloader mirror pull office     # Office → local`,
	}

	cmd.AddCommand(newMirrorTargetCmd(ro))
	cmd.AddCommand(newMirrorDiffCmd(ro))
	cmd.AddCommand(newMirrorPushCmd(ro))
	cmd.AddCommand(newMirrorPullCmd(ro))

	return cmd
}

// Target management commands
func newMirrorTargetCmd(ro *RootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Manage mirror targets",
	}

	cmd.AddCommand(newMirrorTargetAddCmd(ro))
	cmd.AddCommand(newMirrorTargetListCmd(ro))
	cmd.AddCommand(newMirrorTargetRemoveCmd(ro))

	return cmd
}

func newMirrorTargetAddCmd(ro *RootOpts) *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "add <name> <path>",
		Short: "Add a mirror target",
		Long: `Add a named mirror target.

Examples:
  hfdownloader mirror target add office /mnt/nas/hfcache
  hfdownloader mirror target add usb /media/usb/hfcache -d "USB for airgapped deployment"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := args[1]

			// Validate path exists or can be created
			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("invalid path: %w", err)
			}

			cfg, err := hfdownloader.LoadTargets("")
			if err != nil {
				return err
			}

			cfg.Add(name, absPath, description)

			if err := cfg.Save(""); err != nil {
				return err
			}

			if !ro.Quiet {
				fmt.Printf("Added target %q: %s\n", name, absPath)
				if description != "" {
					fmt.Printf("  Description: %s\n", description)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Description for the target")

	return cmd
}

func newMirrorTargetListCmd(ro *RootOpts) *cobra.Command {
	var formatOut string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := hfdownloader.LoadTargets("")
			if err != nil {
				return err
			}

			if formatOut == "json" || ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(cfg.Targets)
			}

			if len(cfg.Targets) == 0 {
				fmt.Println("No targets configured.")
				fmt.Println("\nAdd targets with:")
				fmt.Println("  hfdownloader mirror target add <name> <path>")
				return nil
			}

			// Sort by name
			var names []string
			for name := range cfg.Targets {
				names = append(names, name)
			}
			sort.Strings(names)

			fmt.Printf("%-15s  %-50s  %s\n", "NAME", "PATH", "DESCRIPTION")
			fmt.Printf("%-15s  %-50s  %s\n", strings.Repeat("-", 15), strings.Repeat("-", 50), strings.Repeat("-", 20))

			for _, name := range names {
				t := cfg.Targets[name]
				path := t.Path
				if len(path) > 50 {
					path = "..." + path[len(path)-47:]
				}
				fmt.Printf("%-15s  %-50s  %s\n", name, path, t.Description)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&formatOut, "format", "table", "Output format: table, json")

	return cmd
}

func newMirrorTargetRemoveCmd(ro *RootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a mirror target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := hfdownloader.LoadTargets("")
			if err != nil {
				return err
			}

			if !cfg.Remove(name) {
				return fmt.Errorf("target %q not found", name)
			}

			if err := cfg.Save(""); err != nil {
				return err
			}

			if !ro.Quiet {
				fmt.Printf("Removed target %q\n", name)
			}
			return nil
		},
	}

	return cmd
}

// DiffEntry represents a difference between source and target.
type DiffEntry struct {
	Repo       string `json:"repo"`
	Type       string `json:"type"`
	Status     string `json:"status"` // "missing", "outdated", "extra"
	LocalSize  int64  `json:"local_size,omitempty"`
	RemoteSize int64  `json:"remote_size,omitempty"`
}

func newMirrorDiffCmd(ro *RootOpts) *cobra.Command {
	var cacheDir string
	var formatOut string
	var repoFilter string

	cmd := &cobra.Command{
		Use:   "diff <target>",
		Short: "Show differences between local cache and target",
		Long: `Compare local HuggingFace cache with a target.

Shows:
  - missing: repos in local but not in target
  - extra: repos in target but not in local
  - outdated: repos with different commits

Examples:
  hfdownloader mirror diff office
  hfdownloader mirror diff /mnt/nas/hfcache
  hfdownloader mirror diff office --repo TheBloke/Mistral`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRef := args[0]

			// Load targets config
			targetsCfg, err := hfdownloader.LoadTargets("")
			if err != nil {
				return err
			}
			targetPath := targetsCfg.ResolvePath(targetRef)

			// Local cache: CLI flag > config file > HF_HOME > default
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

			// Scan both caches
			localEntries, err := scanCacheStructure(cacheDir, "")
			if err != nil {
				return fmt.Errorf("scan local cache: %w", err)
			}

			targetEntries, err := scanCacheStructure(targetPath, "")
			if err != nil {
				return fmt.Errorf("scan target cache: %w", err)
			}

			// Build maps
			localMap := make(map[string]ListEntry)
			for _, e := range localEntries {
				if repoFilter == "" || strings.Contains(strings.ToLower(e.Repo), strings.ToLower(repoFilter)) {
					localMap[e.Repo] = e
				}
			}

			targetMap := make(map[string]ListEntry)
			for _, e := range targetEntries {
				if repoFilter == "" || strings.Contains(strings.ToLower(e.Repo), strings.ToLower(repoFilter)) {
					targetMap[e.Repo] = e
				}
			}

			// Calculate diff
			var diffs []DiffEntry

			// Missing in target
			for repo, local := range localMap {
				if _, ok := targetMap[repo]; !ok {
					diffs = append(diffs, DiffEntry{
						Repo:      repo,
						Type:      local.Type,
						Status:    "missing",
						LocalSize: local.Size,
					})
				}
			}

			// Extra in target (not in local)
			for repo, remote := range targetMap {
				if _, ok := localMap[repo]; !ok {
					diffs = append(diffs, DiffEntry{
						Repo:       repo,
						Type:       remote.Type,
						Status:     "extra",
						RemoteSize: remote.Size,
					})
				}
			}

			// Outdated (different commits)
			for repo, local := range localMap {
				if remote, ok := targetMap[repo]; ok {
					if local.Commit != remote.Commit && local.Commit != "" && remote.Commit != "" {
						diffs = append(diffs, DiffEntry{
							Repo:       repo,
							Type:       local.Type,
							Status:     "outdated",
							LocalSize:  local.Size,
							RemoteSize: remote.Size,
						})
					}
				}
			}

			// Sort by status, then repo
			sort.Slice(diffs, func(i, j int) bool {
				if diffs[i].Status != diffs[j].Status {
					return diffs[i].Status < diffs[j].Status
				}
				return diffs[i].Repo < diffs[j].Repo
			})

			// Output
			if formatOut == "json" || ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(diffs)
			}

			if len(diffs) == 0 {
				fmt.Println("Local cache and target are in sync.")
				return nil
			}

			fmt.Printf("Comparing local (%s) with target (%s):\n\n", cacheDir, targetPath)
			fmt.Printf("%-10s  %-7s  %-40s  %s\n", "STATUS", "TYPE", "REPO", "SIZE")
			fmt.Printf("%-10s  %-7s  %-40s  %s\n", strings.Repeat("-", 10), "-------", strings.Repeat("-", 40), "----------")

			var missingSize, extraSize int64
			var missingCount, extraCount, outdatedCount int

			for _, d := range diffs {
				repo := d.Repo
				if len(repo) > 40 {
					repo = "..." + repo[len(repo)-37:]
				}

				var sizeStr string
				switch d.Status {
				case "missing":
					sizeStr = humanSize(d.LocalSize)
					missingSize += d.LocalSize
					missingCount++
				case "extra":
					sizeStr = humanSize(d.RemoteSize)
					extraSize += d.RemoteSize
					extraCount++
				case "outdated":
					sizeStr = humanSize(d.LocalSize)
					outdatedCount++
				}

				fmt.Printf("%-10s  %-7s  %-40s  %s\n", d.Status, d.Type, repo, sizeStr)
			}

			fmt.Println()
			fmt.Printf("Summary: %d missing (%s to push), %d extra, %d outdated\n",
				missingCount, humanSize(missingSize), extraCount, outdatedCount)

			return nil
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Local HuggingFace cache directory")
	cmd.Flags().StringVar(&formatOut, "format", "table", "Output format: table, json")
	cmd.Flags().StringVar(&repoFilter, "repo", "", "Filter by repo name (partial match)")

	return cmd
}

func newMirrorPushCmd(ro *RootOpts) *cobra.Command {
	var cacheDir string
	var repoFilter string
	var dryRun bool
	var verify bool
	var deleteExtra bool
	var force bool

	cmd := &cobra.Command{
		Use:   "push <target>",
		Short: "Push local repos to target (local → target)",
		Long: `Copy repos from local cache to target.

Only copies repos that are missing in target.
Use --repo to filter specific repos.
Use --force to re-copy incomplete or outdated repos.

Examples:
  hfdownloader mirror push office                  # Push all missing repos
  hfdownloader mirror push office --repo Mistral   # Push repos matching "Mistral"
  hfdownloader mirror push office --dry-run        # Show what would be copied
  hfdownloader mirror push office --verify         # Verify integrity after copy
  hfdownloader mirror push office --delete         # Remove repos from target not in local
  hfdownloader mirror push office --force          # Re-copy incomplete repos`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRef := args[0]

			targetsCfg, err := hfdownloader.LoadTargets("")
			if err != nil {
				return err
			}
			targetPath := targetsCfg.ResolvePath(targetRef)

			// CLI flag > config file > HF_HOME > default
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

			return mirrorSync(cacheDir, targetPath, repoFilter, dryRun, verify, deleteExtra, force, ro.Quiet)
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Local HuggingFace cache directory")
	cmd.Flags().StringVar(&repoFilter, "repo", "", "Filter by repo name (partial match)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be copied without copying")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify blob integrity after copy (SHA256)")
	cmd.Flags().BoolVar(&deleteExtra, "delete", false, "Delete repos in target that are not in source")
	cmd.Flags().BoolVar(&force, "force", false, "Re-copy incomplete or outdated repos")

	return cmd
}

func newMirrorPullCmd(ro *RootOpts) *cobra.Command {
	var cacheDir string
	var repoFilter string
	var dryRun bool
	var verify bool
	var deleteExtra bool
	var force bool

	cmd := &cobra.Command{
		Use:   "pull <target>",
		Short: "Pull repos from target to local (target → local)",
		Long: `Copy repos from target to local cache.

Only copies repos that are missing locally.
Use --repo to filter specific repos.
Use --force to re-copy incomplete or outdated repos.

Examples:
  hfdownloader mirror pull office                  # Pull all missing repos
  hfdownloader mirror pull office --repo Mistral   # Pull repos matching "Mistral"
  hfdownloader mirror pull office --dry-run        # Show what would be copied
  hfdownloader mirror pull office --verify         # Verify integrity after copy
  hfdownloader mirror pull office --delete         # Remove local repos not in target
  hfdownloader mirror pull office --force          # Re-copy incomplete repos`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRef := args[0]

			targetsCfg, err := hfdownloader.LoadTargets("")
			if err != nil {
				return err
			}
			targetPath := targetsCfg.ResolvePath(targetRef)

			// CLI flag > config file > HF_HOME > default
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

			// Pull is just push in reverse
			return mirrorSync(targetPath, cacheDir, repoFilter, dryRun, verify, deleteExtra, force, ro.Quiet)
		},
	}

	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Local HuggingFace cache directory")
	cmd.Flags().StringVar(&repoFilter, "repo", "", "Filter by repo name (partial match)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be copied without copying")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify blob integrity after copy (SHA256)")
	cmd.Flags().BoolVar(&deleteExtra, "delete", false, "Delete local repos that are not in target")
	cmd.Flags().BoolVar(&force, "force", false, "Re-copy incomplete or outdated repos")

	return cmd
}

// syncEntry represents a repo to be synced with its reason.
type syncEntry struct {
	ListEntry
	Reason string // "missing", "incomplete", "outdated"
}

// mirrorSync copies repos from source to destination.
func mirrorSync(srcCache, dstCache, repoFilter string, dryRun, verify, deleteExtra, force, quiet bool) error {
	// Scan source
	srcEntries, err := scanCacheStructure(srcCache, "")
	if err != nil {
		return fmt.Errorf("scan source: %w", err)
	}

	// Scan destination
	dstEntries, err := scanCacheStructure(dstCache, "")
	if err != nil {
		// Destination might not exist yet
		dstEntries = nil
	}

	srcMap := make(map[string]ListEntry)
	for _, e := range srcEntries {
		srcMap[e.Repo] = e
	}

	dstMap := make(map[string]ListEntry)
	for _, e := range dstEntries {
		dstMap[e.Repo] = e
	}

	// Find repos to copy
	var toCopy []syncEntry
	for _, e := range srcEntries {
		if repoFilter != "" && !strings.Contains(strings.ToLower(e.Repo), strings.ToLower(repoFilter)) {
			continue
		}

		if _, ok := dstMap[e.Repo]; !ok {
			// Missing in destination
			toCopy = append(toCopy, syncEntry{ListEntry: e, Reason: "missing"})
		} else if force {
			// Check if destination is incomplete or outdated
			relPath, _ := filepath.Rel(srcCache, e.Path)
			dstPath := filepath.Join(dstCache, relPath)
			needsUpdate, reason := compareRepoIntegrity(e.Path, dstPath)
			if needsUpdate && reason != "missing" {
				toCopy = append(toCopy, syncEntry{ListEntry: e, Reason: reason})
			}
		}
	}

	// Find repos to delete (in dst but not in src)
	var toDelete []ListEntry
	if deleteExtra {
		for _, e := range dstEntries {
			if repoFilter != "" && !strings.Contains(strings.ToLower(e.Repo), strings.ToLower(repoFilter)) {
				continue
			}
			if _, ok := srcMap[e.Repo]; !ok {
				toDelete = append(toDelete, e)
			}
		}
	}

	if len(toCopy) == 0 && len(toDelete) == 0 {
		if !quiet {
			fmt.Println("Nothing to do - destination is in sync.")
		}
		return nil
	}

	// Calculate total size
	var totalCopySize int64
	for _, e := range toCopy {
		totalCopySize += e.Size
	}

	var totalDeleteSize int64
	for _, e := range toDelete {
		totalDeleteSize += e.Size
	}

	if !quiet {
		if len(toCopy) > 0 {
			fmt.Printf("Will copy %d repos (%s):\n", len(toCopy), humanSize(totalCopySize))
			for _, e := range toCopy {
				if e.Reason != "missing" {
					fmt.Printf("  + %s (%s) [%s]\n", e.Repo, humanSize(e.Size), e.Reason)
				} else {
					fmt.Printf("  + %s (%s)\n", e.Repo, humanSize(e.Size))
				}
			}
		}
		if len(toDelete) > 0 {
			fmt.Printf("Will delete %d repos (%s):\n", len(toDelete), humanSize(totalDeleteSize))
			for _, e := range toDelete {
				fmt.Printf("  - %s (%s)\n", e.Repo, humanSize(e.Size))
			}
		}
		fmt.Println()
	}

	if dryRun {
		fmt.Println("Dry run - no changes made.")
		return nil
	}

	// Copy each repo
	for i, e := range toCopy {
		if !quiet {
			if e.Reason != "missing" {
				fmt.Printf("[%d/%d] Copying %s (%s)...\n", i+1, len(toCopy), e.Repo, e.Reason)
			} else {
				fmt.Printf("[%d/%d] Copying %s...\n", i+1, len(toCopy), e.Repo)
			}
		}

		// For incomplete/outdated repos, remove destination first
		if e.Reason == "incomplete" || e.Reason == "outdated" {
			relPath, _ := filepath.Rel(srcCache, e.Path)
			dstPath := filepath.Join(dstCache, relPath)
			os.RemoveAll(dstPath)
		}

		if err := copyRepoCache(e.Path, srcCache, dstCache); err != nil {
			return fmt.Errorf("copy %s: %w", e.Repo, err)
		}

		// Verify if requested
		if verify {
			if !quiet {
				fmt.Printf("  Verifying...")
			}
			if err := verifyRepoCache(e.Path, srcCache, dstCache); err != nil {
				return fmt.Errorf("verify %s: %w", e.Repo, err)
			}
			if !quiet {
				fmt.Printf(" OK\n")
			}
		}
	}

	// Delete extra repos
	for i, e := range toDelete {
		if !quiet {
			fmt.Printf("[%d/%d] Deleting %s...\n", i+1, len(toDelete), e.Repo)
		}

		if err := os.RemoveAll(e.Path); err != nil {
			return fmt.Errorf("delete %s: %w", e.Repo, err)
		}
	}

	if !quiet {
		var summary []string
		if len(toCopy) > 0 {
			summary = append(summary, fmt.Sprintf("copied %d repos (%s)", len(toCopy), humanSize(totalCopySize)))
		}
		if len(toDelete) > 0 {
			summary = append(summary, fmt.Sprintf("deleted %d repos (%s)", len(toDelete), humanSize(totalDeleteSize)))
		}
		fmt.Printf("\nDone. %s.\n", strings.Join(summary, ", "))
	}

	return nil
}

// copyRepoCache copies a repo from source to destination cache.
func copyRepoCache(repoPath, srcCache, dstCache string) error {
	// Get relative path from source cache
	relPath, err := filepath.Rel(srcCache, repoPath)
	if err != nil {
		return err
	}

	dstPath := filepath.Join(dstCache, relPath)

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	// Use cp -r for simplicity (cross-platform would need more work)
	// For now, walk and copy
	return filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}

		dst := filepath.Join(dstPath, rel)

		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode())
		}

		// Check if it's a symlink
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Remove existing symlink if exists
			os.Remove(dst)
			return os.Symlink(link, dst)
		}

		// Copy file
		return copyFile(path, dst)
	})
}

// copyFile copies a single file.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

// verifyRepoCache verifies that a copied repo matches the source.
// It compares blob file sizes (blob filenames are SHA256, so same name = same content).
func verifyRepoCache(repoPath, srcCache, dstCache string) error {
	relPath, err := filepath.Rel(srcCache, repoPath)
	if err != nil {
		return err
	}

	dstPath := filepath.Join(dstCache, relPath)
	blobsDir := filepath.Join(repoPath, "blobs")

	// Walk source blobs and verify they exist in destination with same size
	return filepath.Walk(blobsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}

		dstFile := filepath.Join(dstPath, rel)
		dstInfo, err := os.Stat(dstFile)
		if err != nil {
			return fmt.Errorf("missing blob %s: %w", rel, err)
		}

		if dstInfo.Size() != info.Size() {
			return fmt.Errorf("size mismatch for %s: src=%d dst=%d", rel, info.Size(), dstInfo.Size())
		}

		return nil
	})
}

// RepoIntegrity represents the integrity status of a repo.
type RepoIntegrity struct {
	Complete     bool     `json:"complete"`
	HasRefs      bool     `json:"has_refs"`
	HasBlobs     bool     `json:"has_blobs"`
	HasSnapshots bool     `json:"has_snapshots"`
	BlobCount    int      `json:"blob_count"`
	MissingBlobs []string `json:"missing_blobs,omitempty"`
	Errors       []string `json:"errors,omitempty"`
}

// checkRepoIntegrity checks if a repo cache is complete and valid.
func checkRepoIntegrity(repoPath string) RepoIntegrity {
	result := RepoIntegrity{Complete: true}

	// Check refs directory
	refsDir := filepath.Join(repoPath, "refs")
	if entries, err := os.ReadDir(refsDir); err == nil && len(entries) > 0 {
		result.HasRefs = true
	} else {
		result.Complete = false
		result.Errors = append(result.Errors, "missing or empty refs/")
	}

	// Check blobs directory
	blobsDir := filepath.Join(repoPath, "blobs")
	if entries, err := os.ReadDir(blobsDir); err == nil && len(entries) > 0 {
		result.HasBlobs = true
		result.BlobCount = len(entries)
	} else {
		result.Complete = false
		result.Errors = append(result.Errors, "missing or empty blobs/")
	}

	// Check snapshots directory
	snapshotsDir := filepath.Join(repoPath, "snapshots")
	if entries, err := os.ReadDir(snapshotsDir); err == nil && len(entries) > 0 {
		result.HasSnapshots = true

		// Check if snapshot symlinks point to existing blobs
		for _, entry := range entries {
			snapshotPath := filepath.Join(snapshotsDir, entry.Name())
			filepath.Walk(snapshotPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.Mode()&os.ModeSymlink != 0 {
					target, err := os.Readlink(path)
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("broken symlink: %s", path))
						result.Complete = false
						return nil
					}
					// Resolve relative symlink
					absTarget := filepath.Join(filepath.Dir(path), target)
					if _, err := os.Stat(absTarget); os.IsNotExist(err) {
						blobName := filepath.Base(target)
						result.MissingBlobs = append(result.MissingBlobs, blobName)
						result.Complete = false
					}
				}
				return nil
			})
		}
	} else {
		result.Complete = false
		result.Errors = append(result.Errors, "missing or empty snapshots/")
	}

	return result
}

// compareRepoIntegrity compares source and destination repos.
// Returns true if destination needs to be updated.
func compareRepoIntegrity(srcPath, dstPath string) (needsUpdate bool, reason string) {
	// Check if destination exists
	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		return true, "missing"
	}

	srcIntegrity := checkRepoIntegrity(srcPath)
	dstIntegrity := checkRepoIntegrity(dstPath)

	// If destination is incomplete, it needs update
	if !dstIntegrity.Complete {
		return true, "incomplete"
	}

	// If source has more blobs, destination needs update
	if srcIntegrity.BlobCount > dstIntegrity.BlobCount {
		return true, "outdated"
	}

	return false, ""
}
