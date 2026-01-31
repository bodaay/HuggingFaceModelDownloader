// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SyncOptions configures the sync operation.
type SyncOptions struct {
	// Clean removes orphaned symlinks in the friendly view
	Clean bool
	// Verbose prints detailed progress
	Verbose bool
}

// SyncResult contains statistics from a sync operation.
type SyncResult struct {
	ReposScanned    int
	SymlinksCreated int
	SymlinksUpdated int
	OrphansRemoved  int
	Errors          []error
}

// Sync regenerates the friendly view (models/, datasets/) from the hub cache.
// It scans all repos in hub/, reads their refs to find current commits,
// and creates symlinks in the friendly view pointing to snapshot files.
func (c *HFCache) Sync(opts SyncOptions) (*SyncResult, error) {
	result := &SyncResult{}

	hubDir := c.HubDir()
	if _, err := os.Stat(hubDir); errors.Is(err, os.ErrNotExist) {
		return result, nil // Nothing to sync
	}

	// Scan hub/ for repo directories
	entries, err := os.ReadDir(hubDir)
	if err != nil {
		return nil, fmt.Errorf("read hub directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Parse repo directory name (models--owner--name or datasets--owner--name)
		repoType, owner, name, ok := parseRepoDirName(entry.Name())
		if !ok {
			continue // Not a valid repo directory
		}

		result.ReposScanned++

		repoDir, err := c.Repo(owner+"/"+name, repoType)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("parse repo %s: %w", entry.Name(), err))
			continue
		}

		// Sync this repo's friendly view
		created, updated, err := c.syncRepoFriendlyView(repoDir, opts)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("sync %s: %w", entry.Name(), err))
			continue
		}

		result.SymlinksCreated += created
		result.SymlinksUpdated += updated
	}

	// Clean orphaned symlinks if requested
	if opts.Clean {
		removed, err := c.cleanOrphanedSymlinks(opts)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("clean orphans: %w", err))
		}
		result.OrphansRemoved = removed
	}

	return result, nil
}

// parseRepoDirName extracts repo type, owner, and name from a hub directory name.
// Format: models--owner--name or datasets--owner--name
func parseRepoDirName(dirName string) (RepoType, string, string, bool) {
	var repoType RepoType
	var rest string

	if strings.HasPrefix(dirName, "models--") {
		repoType = RepoTypeModel
		rest = strings.TrimPrefix(dirName, "models--")
	} else if strings.HasPrefix(dirName, "datasets--") {
		repoType = RepoTypeDataset
		rest = strings.TrimPrefix(dirName, "datasets--")
	} else {
		return "", "", "", false
	}

	// Split owner--name
	parts := strings.SplitN(rest, "--", 2)
	if len(parts) != 2 {
		return "", "", "", false
	}

	return repoType, parts[0], parts[1], true
}

// syncRepoFriendlyView syncs a single repo's friendly view.
// Returns (created, updated, error).
func (c *HFCache) syncRepoFriendlyView(repoDir *RepoDir, opts SyncOptions) (int, int, error) {
	created := 0
	updated := 0

	// Find the current commit from refs
	// Try common refs: main, master
	var commit string
	for _, ref := range []string{"main", "master"} {
		c, err := repoDir.ReadRef(ref)
		if err != nil {
			return 0, 0, fmt.Errorf("read ref %s: %w", ref, err)
		}
		if c != "" {
			commit = c
			break
		}
	}

	if commit == "" {
		// No refs found, try to find any snapshot
		snapshots, err := repoDir.ListSnapshots()
		if err != nil {
			return 0, 0, fmt.Errorf("list snapshots: %w", err)
		}
		if len(snapshots) == 0 {
			return 0, 0, nil // No snapshots, nothing to sync
		}
		commit = snapshots[0] // Use first available snapshot
	}

	// Get snapshot directory
	snapshotDir := repoDir.SnapshotDir(commit)
	if _, err := os.Stat(snapshotDir); errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil // Snapshot doesn't exist
	}

	// Ensure friendly directory exists
	if err := repoDir.EnsureFriendlyDir(); err != nil {
		return 0, 0, fmt.Errorf("ensure friendly dir: %w", err)
	}

	// Walk snapshot and create friendly symlinks
	err := filepath.Walk(snapshotDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path within snapshot
		relPath, err := filepath.Rel(snapshotDir, path)
		if err != nil {
			return err
		}

		// Check if friendly symlink already exists and is correct
		friendlyPath := filepath.Join(repoDir.FriendlyPath(), relPath)
		existingTarget, err := os.Readlink(friendlyPath)

		snapshotPath := repoDir.SnapshotPath(commit, relPath)
		expectedTarget, _ := filepath.Rel(filepath.Dir(friendlyPath), snapshotPath)

		if err == nil && existingTarget == expectedTarget {
			// Symlink exists and is correct
			return nil
		}

		// Create or update symlink
		if err := repoDir.CreateFriendlySymlink(commit, relPath, ""); err != nil {
			return fmt.Errorf("create symlink for %s: %w", relPath, err)
		}

		if errors.Is(err, os.ErrNotExist) {
			created++
		} else {
			updated++
		}

		return nil
	})

	if err != nil {
		return created, updated, fmt.Errorf("walk snapshot: %w", err)
	}

	return created, updated, nil
}

// cleanOrphanedSymlinks removes symlinks in friendly view that point to non-existent files.
func (c *HFCache) cleanOrphanedSymlinks(opts SyncOptions) (int, error) {
	removed := 0

	// Clean models/
	modelsRemoved, err := cleanOrphansInDir(c.ModelsDir())
	if err != nil {
		return removed, fmt.Errorf("clean models: %w", err)
	}
	removed += modelsRemoved

	// Clean datasets/
	datasetsRemoved, err := cleanOrphansInDir(c.DatasetsDir())
	if err != nil {
		return removed, fmt.Errorf("clean datasets: %w", err)
	}
	removed += datasetsRemoved

	return removed, nil
}

// cleanOrphansInDir removes broken symlinks in a directory tree.
func cleanOrphansInDir(dir string) (int, error) {
	removed := 0

	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip errors (e.g., permission denied)
			return nil
		}

		// Check if it's a symlink
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}

		// Check if symlink target exists
		target, err := os.Readlink(path)
		if err != nil {
			return nil // Can't read symlink, skip
		}

		// Resolve relative to symlink location
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}

		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			// Broken symlink, remove it
			if err := os.Remove(path); err != nil {
				return nil // Skip errors
			}
			removed++
		}

		return nil
	})

	if err != nil {
		return removed, err
	}

	// Clean up empty directories
	cleanEmptyDirs(dir)

	return removed, nil
}

// cleanEmptyDirs removes empty directories from bottom up.
func cleanEmptyDirs(dir string) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}

		// Try to remove (will fail if not empty)
		os.Remove(path)
		return nil
	})
}

// ListRepos returns all repositories in the cache.
func (c *HFCache) ListRepos() ([]*RepoDir, error) {
	var repos []*RepoDir

	hubDir := c.HubDir()
	if _, err := os.Stat(hubDir); errors.Is(err, os.ErrNotExist) {
		return repos, nil
	}

	entries, err := os.ReadDir(hubDir)
	if err != nil {
		return nil, fmt.Errorf("read hub directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		repoType, owner, name, ok := parseRepoDirName(entry.Name())
		if !ok {
			continue
		}

		repoDir, err := c.Repo(owner+"/"+name, repoType)
		if err != nil {
			continue
		}

		repos = append(repos, repoDir)
	}

	return repos, nil
}
