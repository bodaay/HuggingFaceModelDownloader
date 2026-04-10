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

// PruneResult contains statistics from a prune operation.
type PruneResult struct {
	ReposScanned      int
	IncompleteRemoved int
	TempRemoved       int
	OrphanedRemoved   int
	SpaceFreed        int64
	Errors            []error
}

// Prune removes stale incomplete downloads, leftover temp files, and orphaned
// blobs from every repo in the cache.
//
// Three categories of garbage are cleaned up per repo:
//  1. *.incomplete files whose owning process is no longer alive (stale downloads).
//  2. tmp-* files in the blobs directory (partial downloads that were never moved).
//  3. Blob files that are not referenced by any snapshot symlink (orphaned blobs).
func (c *HFCache) Prune() (*PruneResult, error) {
	result := &PruneResult{}

	hubDir := c.HubDir()
	if _, err := os.Stat(hubDir); errors.Is(err, os.ErrNotExist) {
		return result, nil
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

		result.ReposScanned++

		repoDir, err := c.Repo(owner+"/"+name, repoType)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("parse repo %s: %w", entry.Name(), err))
			continue
		}

		inc, tmp, orph, freed, err := c.pruneRepo(repoDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("prune %s: %w", entry.Name(), err))
			continue
		}

		result.IncompleteRemoved += inc
		result.TempRemoved += tmp
		result.OrphanedRemoved += orph
		result.SpaceFreed += freed
	}

	return result, nil
}

// pruneRepo removes stale incomplete files, tmp-* files, and orphaned blobs for
// a single repo.  Returns (incompleteRemoved, tempRemoved, orphanedRemoved, spaceFreed, error).
func (c *HFCache) pruneRepo(repoDir *RepoDir) (int, int, int, int64, error) {
	blobsDir := repoDir.BlobsDir()
	if _, err := os.Stat(blobsDir); errors.Is(err, os.ErrNotExist) {
		return 0, 0, 0, 0, nil
	}

	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("read blobs dir: %w", err)
	}

	var incomplete, temp, orphaned int
	var freed int64

	// Step 1: Remove stale *.incomplete files (and their *.incomplete.meta companions).
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Match .incomplete but not .incomplete.meta (which ends with .meta, not .incomplete)
		if !strings.HasSuffix(name, ".incomplete") {
			continue
		}
		sha256 := strings.TrimSuffix(name, ".incomplete")
		status, _, _ := repoDir.CheckBlob(sha256)
		if status == BlobDownloading {
			// An active process still owns this file; leave it alone.
			continue
		}
		incPath := repoDir.IncompletePath(sha256)
		if info, statErr := os.Stat(incPath); statErr == nil {
			freed += info.Size()
		}
		if removeErr := repoDir.CleanupIncomplete(sha256); removeErr == nil {
			incomplete++
		}
	}

	// Step 2: Remove tmp-* files.  These are transient download targets created
	// by the downloader before the file is moved to its final blob path.  They
	// have no associated meta file, so we rely on the caller having verified that
	// no jobs are active before calling Prune.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "tmp-") {
			continue
		}
		tmpPath := filepath.Join(blobsDir, name)
		if info, statErr := os.Stat(tmpPath); statErr == nil {
			freed += info.Size()
		}
		if removeErr := os.Remove(tmpPath); removeErr == nil {
			temp++
		}
	}

	// Step 3: Remove blobs not referenced by any snapshot symlink (orphaned blobs).
	// First build the set of referenced blob filenames.
	referenced, walkErr := collectReferencedBlobs(repoDir.SnapshotsDir())
	if walkErr != nil {
		return incomplete, temp, 0, freed, fmt.Errorf("scan snapshots: %w", walkErr)
	}

	// Re-read the directory; steps 1 & 2 may have removed entries.
	entries, err = os.ReadDir(blobsDir)
	if err != nil {
		return incomplete, temp, 0, freed, fmt.Errorf("re-read blobs dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip incomplete-related and tmp files (already handled above).
		if strings.Contains(name, ".incomplete") || strings.HasPrefix(name, "tmp-") {
			continue
		}
		if !referenced[name] {
			blobPath := filepath.Join(blobsDir, name)
			if info, statErr := os.Stat(blobPath); statErr == nil {
				freed += info.Size()
			}
			if removeErr := os.Remove(blobPath); removeErr == nil {
				orphaned++
			}
		}
	}

	return incomplete, temp, orphaned, freed, nil
}

// collectReferencedBlobs walks the snapshots directory and returns the set of
// blob filenames (sha256 hashes) targeted by snapshot symlinks.
// Any walk errors are returned so the caller can track them.
func collectReferencedBlobs(snapshotsDir string) (map[string]bool, error) {
	referenced := make(map[string]bool)
	if _, err := os.Stat(snapshotsDir); errors.Is(err, os.ErrNotExist) {
		return referenced, nil
	}

	// filepath.Walk uses os.Lstat, so symlinks are reported with ModeSymlink set.
	err := filepath.Walk(snapshotsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		referenced[filepath.Base(filepath.Clean(target))] = true
		return nil
	})

	return referenced, err
}
