// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// verifyDownloaded checks a freshly downloaded file against the strongest
// reference available, returning a *VerificationError on mismatch.
//
//   - If the plan carries an expected content SHA256 (always true for LFS,
//     which covers every multipart download because range requests are only
//     issued for LFS files; and true for any file a mirror annotates with a
//     sha256), verify it regardless of cfg.Verify. A known hash is the
//     strongest check and catches same-size corruption a size check misses.
//   - Otherwise fall back to cfg.Verify:
//     "sha256"/"etag" fetch the server's content hash via HEAD and verify it,
//     falling back to a size check when the server exposes no hash (so neither
//     mode is ever a silent no-op); "size" checks the byte count; "none"
//     skips verification.
func verifyDownloaded(ctx context.Context, httpc *http.Client, cfg Settings, itForIO, it PlanItem, dst, relPath string) error {
	return verifyDownloadedProgress(ctx, httpc, cfg, itForIO, it, dst, relPath, nil)
}

// verifyDownloadedProgress is verifyDownloaded with hashing progress reported
// through onProgress (bytes hashed so far; may be nil). Hashing a multi-GB
// file takes long enough that, without progress, a finished download looks
// stuck at 100% (github issue #88). Hashing stops promptly when ctx is done.
func verifyDownloadedProgress(ctx context.Context, httpc *http.Client, cfg Settings, itForIO, it PlanItem, dst, relPath string, onProgress func(done int64)) error {
	if it.SHA256 != "" {
		return checkSHA256(ctx, dst, it.SHA256, relPath, "sha256", onProgress)
	}

	switch cfg.Verify {
	case "none":
		return nil
	case "sha256", "etag":
		if _, remoteSha, _ := headForETag(ctx, httpc, cfg.Token, itForIO); remoteSha != "" {
			return checkSHA256(ctx, dst, remoteSha, relPath, cfg.Verify, onProgress)
		}
		if it.Size > 0 {
			return checkSize(dst, it.Size, relPath)
		}
		return nil
	case "size":
		if it.Size > 0 {
			return checkSize(dst, it.Size, relPath)
		}
		return nil
	default:
		// Unknown mode: be safe and size-check when we can.
		if it.Size > 0 {
			return checkSize(dst, it.Size, relPath)
		}
		return nil
	}
}

// computeSHA256 computes and returns the SHA256 hash of a file.
func computeSHA256(path string) (string, error) {
	return computeSHA256Ctx(context.Background(), path, nil)
}

// computeSHA256Ctx computes a file's SHA256, stopping when ctx is done and
// reporting bytes hashed through onProgress (may be nil).
func computeSHA256Ctx(ctx context.Context, path string, onProgress func(done int64)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := copyCtx(ctx, h, f, onProgress); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifySHA256 computes the SHA256 of a file and compares it to expected.
func verifySHA256(path string, expected string) error {
	sum, err := computeSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s got %s", expected, sum)
	}
	return nil
}

// checkSHA256 verifies a downloaded file against an expected SHA256 and, on
// mismatch, returns a *VerificationError carrying the repo-relative path so
// callers can errors.As it and report structured detail. relPath is the path
// used for reporting; method labels how the expected hash was obtained
// ("sha256" from the plan, "etag" from a server HEAD).
func checkSHA256(ctx context.Context, path, expected, relPath, method string, onProgress func(done int64)) error {
	sum, err := computeSHA256Ctx(ctx, path, onProgress)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, expected) {
		return &VerificationError{Path: relPath, Expected: expected, Actual: sum, Method: method}
	}
	return nil
}

// checkSize verifies a downloaded file's size against the expected size and, on
// mismatch (or stat failure), returns a *VerificationError.
func checkSize(path string, expected int64, relPath string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return &VerificationError{Path: relPath, Expected: fmt.Sprintf("%d", expected), Actual: "missing", Method: "size"}
	}
	if fi.Size() != expected {
		return &VerificationError{Path: relPath, Expected: fmt.Sprintf("%d", expected), Actual: fmt.Sprintf("%d", fi.Size()), Method: "size"}
	}
	return nil
}

// shouldSkipLocal checks if a file already exists and matches expected hash/size.
// Returns (skip, reason, error).
func shouldSkipLocal(it PlanItem, dst string) (bool, string, error) {
	fi, err := os.Stat(dst)
	if err != nil {
		// no file
		return false, "", nil
	}

	// Quick size check first: if known and different, don't skip
	if it.Size > 0 && fi.Size() != it.Size {
		return false, "", nil
	}

	// LFS with known sha: compute and compare
	if it.LFS && it.SHA256 != "" {
		if err := verifySHA256(dst, it.SHA256); err == nil {
			return true, "sha256 match", nil
		}
		// size matched but sha mismatched -> re-download
		return false, "", nil
	}

	// Non-LFS (or unknown sha): size match is sufficient
	if it.Size > 0 && fi.Size() == it.Size {
		return true, "size match", nil
	}

	return false, "", nil
}

