// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"fmt"
	"os"
	"path/filepath"
)

// CopyRepoCache recursively copies a single repo's cache directory from
// srcCache to dstCache, preserving the repo's relative path, directory
// structure and symlinks, and streaming file contents (so large blobs are not
// loaded fully into memory). repoPath must be a directory inside srcCache.
//
// This is the shared implementation behind both the `mirror` CLI command and
// the server's mirror endpoints, which previously carried byte-identical
// copies of it.
func CopyRepoCache(repoPath, srcCache, dstCache string) error {
	relPath, err := filepath.Rel(srcCache, repoPath)
	if err != nil {
		return err
	}

	dstPath := filepath.Join(dstCache, relPath)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	// Files already copied, by size, to recreate hardlinks (hardlink-mode
	// caches: a snapshot entry shares its blob's data) instead of storing the
	// data twice on the target.
	type copied struct {
		info os.FileInfo
		dst  string
	}
	bySize := map[int64][]copied{}

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

		// Recreate symlinks (the HF cache's friendly/snapshot views) verbatim.
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.Remove(dst)
			return os.Symlink(link, dst)
		}

		for _, c := range bySize[info.Size()] {
			if os.SameFile(c.info, info) {
				os.Remove(dst)
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil && os.Link(c.dst, dst) == nil {
					return nil
				}
				break // can't link on the target: copy
			}
		}
		if err := CopyFileStream(path, dst); err != nil {
			return err
		}
		bySize[info.Size()] = append(bySize[info.Size()], copied{info, dst})
		return nil
	})
}

// VerifyRepoCache verifies that every blob under repoPath exists in the
// destination cache (at the same relative location) with byte-identical SHA256
// content. Used by the mirror --verify path; it hashes every blob, so it is
// intentionally opt-in and slow on large repos. A size mismatch is reported
// before the (more expensive) hash comparison.
func VerifyRepoCache(repoPath, srcCache, dstCache string) error {
	relPath, err := filepath.Rel(srcCache, repoPath)
	if err != nil {
		return err
	}

	dstPath := filepath.Join(dstCache, relPath)
	blobsDir := filepath.Join(repoPath, "blobs")

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

		// Fast reject on size before the expensive hash.
		if dstInfo.Size() != info.Size() {
			return fmt.Errorf("size mismatch for %s: src=%d dst=%d", rel, info.Size(), dstInfo.Size())
		}

		same, err := SameFileSHA256(path, dstFile)
		if err != nil {
			return fmt.Errorf("verify %s: %w", rel, err)
		}
		if !same {
			return fmt.Errorf("sha256 mismatch for %s after copy", rel)
		}

		return nil
	})
}
