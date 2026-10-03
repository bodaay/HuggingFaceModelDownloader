// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// unsafeRepoPath reports whether a relative path returned by the repo tree API
// would escape the repository root if joined onto a local directory. The path
// list is remote-controlled (and the endpoint is operator-configurable via
// --endpoint, so a malicious or MITM'd mirror can return anything), and the
// path flows unchecked into file writes and symlink creation. Anything that is
// absolute, contains a "\\" (Windows separator / drive escape), or normalises
// to "" / ".." / a "../" prefix is rejected to prevent arbitrary-file-write.
func unsafeRepoPath(rel string) bool {
	if rel == "" {
		return true
	}
	if strings.ContainsRune(rel, '\\') || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return true
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return true
	}
	return false
}

// PlanItem represents a single file in the download plan.
type PlanItem struct {
	RelativePath string `json:"path"`
	URL          string `json:"url"`
	LFS          bool   `json:"lfs"`
	SHA256       string `json:"sha256,omitempty"`
	Size         int64  `json:"size"`
	AcceptRanges bool   `json:"acceptRanges"`
	// Subdir holds the matched filter (if any) used when --append-filter-subdir is set.
	Subdir string `json:"subdir,omitempty"`
}

// Plan contains the list of files to download.
type Plan struct {
	Items  []PlanItem `json:"items"`
	Commit string     `json:"commit,omitempty"` // Commit hash for this plan (for HF cache snapshots)
}

// PlanRepo builds the file list without downloading.
func PlanRepo(ctx context.Context, job Job, cfg Settings) (*Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validate(job, cfg); err != nil {
		return nil, err
	}
	if job.Revision == "" {
		job.Revision = "main"
	}
	httpc := buildHTTPClientWithProxy(cfg.Proxy)
	return scanRepo(ctx, httpc, cfg.Token, job, cfg)
}

// scanRepo walks the repo tree and builds a download plan.
func scanRepo(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings) (*Plan, error) {
	var items []PlanItem
	seen := make(map[string]struct{}) // ensure each relative path appears once in the plan

	// Fetch actual commit SHA for the revision
	repoInfo, err := fetchRepoInfo(ctx, httpc, token, cfg.Endpoint, job)
	if err != nil {
		// Fall back to revision name if API call fails (e.g., some mirrors)
		repoInfo = &RepoInfo{SHA: job.Revision}
	}
	commitSHA := repoInfo.SHA
	if commitSHA == "" {
		commitSHA = job.Revision // fallback
	}

	// List and fetch files at the resolved commit rather than the branch name,
	// so a repo updated mid-download (or between a pause and a resume) can't
	// mix file versions under one snapshot.
	pinned := job
	pinned.Revision = commitSHA

	err = walkTree(ctx, httpc, token, cfg.Endpoint, pinned, "", func(n hfNode) error {
		if n.Type != "file" && n.Type != "blob" {
			return nil
		}
		rel := n.Path

		// Reject path-traversal entries before they reach any filesystem
		// operation. Fail the whole plan rather than silently skipping so a
		// tampered tree is loud, not partial.
		if unsafeRepoPath(rel) {
			return fmt.Errorf("refusing unsafe path from repo tree: %q", rel)
		}

		// Deduplicate by relative path
		if _, ok := seen[rel]; ok {
			return nil
		}
		seen[rel] = struct{}{}

		name := filepath.Base(rel)
		nameLower := strings.ToLower(name)
		relLower := strings.ToLower(rel)
		isLFS := n.LFS != nil

		// Check excludes first - if file matches any exclude pattern, skip it
		// Credits: Exclude feature suggested by jeroenkroese (#41)
		for _, ex := range job.Excludes {
			exLower := strings.ToLower(ex)
			if strings.Contains(nameLower, exLower) || strings.Contains(relLower, exLower) {
				return nil // excluded
			}
		}

		// Determine which filter (if any) matches this file name, prefer the longest match
		// Filter matching is case-insensitive (e.g., q4_0 matches Q4_0)
		matchedFilter := ""
		if isLFS && len(job.Filters) > 0 {
			for _, f := range job.Filters {
				fLower := strings.ToLower(f)
				if filterMatches(nameLower, fLower, job.ExactMatch) {
					if len(f) > len(matchedFilter) {
						matchedFilter = f
					}
				}
			}
			// If filters provided and none matched, skip typical large LFS blobs
			if matchedFilter == "" {
				ln := strings.ToLower(name)
				ext := strings.ToLower(filepath.Ext(name))
				if ext == ".bin" || ext == ".act" || ext == ".safetensors" || ext == ".zip" || strings.HasSuffix(ln, ".gguf") || strings.HasSuffix(ln, ".ggml") {
					return nil
				}
			}
		}

		// Build URL and file size
		var urlStr string
		if isLFS {
			urlStr = lfsURL(cfg.Endpoint, pinned, rel)
		} else {
			urlStr = rawURL(cfg.Endpoint, pinned, rel)
		}
		// For LFS files, ALWAYS use LFS.Size (n.Size is the pointer file size, not actual)
		var size int64
		if n.LFS != nil && n.LFS.Size > 0 {
			size = n.LFS.Size
		} else {
			size = n.Size
		}

		// Assume LFS files support range requests (HuggingFace always does)
		// Don't block with HEAD requests during planning - too slow for large repos
		acceptRanges := isLFS

		sha := n.Sha256
		if sha == "" && n.LFS != nil {
			// LFS files have SHA256 in either Sha256 field or Oid field (LFS spec uses oid)
			sha = n.LFS.Sha256
			if sha == "" {
				sha = n.LFS.Oid
			}
		}

		items = append(items, PlanItem{
			RelativePath: rel,
			URL:          urlStr,
			LFS:          isLFS,
			SHA256:       sha,
			Size:         size,
			AcceptRanges: acceptRanges,
			Subdir:       matchedFilter, // empty when no filter matched
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Plan{Items: items, Commit: commitSHA}, nil
}

// filterMatches reports whether filter fLower matches the file name nameLower
// (both already lowercased). In substring mode (the default) it uses a plain
// substring check. In exact mode it matches when fLower equals either the whole
// file name (with or without its extension) or a single delimiter-bounded
// segment of the name. So "q6_k" matches "...-Q6_K.gguf" but not
// "...-Q6_K_XL.gguf" (github issue #78), while a full-name filter such as a
// vision encoder's "...-mmproj-bf16" still matches its file (github issue #84).
func filterMatches(nameLower, fLower string, exact bool) bool {
	if !exact {
		return strings.Contains(nameLower, fLower)
	}
	// Whole-name match: handles filters that are a full file name, e.g. the
	// mmproj/vision-encoder companion whose filter is the name minus ".gguf".
	if fLower == nameLower || fLower == strings.TrimSuffix(nameLower, filepath.Ext(nameLower)) {
		return true
	}
	for _, seg := range strings.FieldsFunc(nameLower, isFilterDelimiter) {
		if seg == fLower {
			return true
		}
	}
	return false
}

// isFilterDelimiter reports whether r separates segments for exact-match
// filtering. Underscores are intentionally NOT delimiters because quantization
// names contain them (e.g. Q6_K, Q4_K_M).
func isFilterDelimiter(r rune) bool {
	return r == '-' || r == '.' || r == ' '
}

// destinationBase returns the base output directory for a job.
func destinationBase(job Job, cfg Settings) string {
	// Always OutputDir/<repo>; per-file filter subdirs are applied in Download().
	return filepath.Join(cfg.OutputDir, job.Repo)
}

// ScanPlan scans a repository and emits plan_item events via the progress callback.
// This is useful for dry-run/preview functionality.
func ScanPlan(ctx context.Context, job Job, cfg Settings, progress ProgressFunc) error {
	plan, err := PlanRepo(ctx, job, cfg)
	if err != nil {
		return err
	}

	if progress != nil {
		for _, item := range plan.Items {
			progress(ProgressEvent{
				Time:     time.Now().UTC(),
				Event:    "plan_item",
				Repo:     job.Repo,
				Revision: job.Revision,
				Path:     item.RelativePath,
				Total:    item.Size,
				IsLFS:    item.LFS,
			})
		}
	}

	return nil
}

// Run is an alias for Download for API compatibility.
func Run(ctx context.Context, job Job, cfg Settings, progress ProgressFunc) error {
	return Download(ctx, job, cfg, progress)
}

