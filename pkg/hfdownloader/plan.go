// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/filtermatch"
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

	// urlJob is the job whose revision appears in tree and file URLs.
	var urlJob Job
	visit := func(n hfNode) error {
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

		// Build URL and file size
		var urlStr string
		if isLFS {
			urlStr = lfsURL(cfg.Endpoint, urlJob, rel)
		} else {
			urlStr = rawURL(cfg.Endpoint, urlJob, rel)
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
		})
		return nil
	}
	walk := func(j Job) error {
		items, seen, urlJob = nil, make(map[string]struct{}), j
		return walkTree(ctx, httpc, token, cfg.Endpoint, j, "", visit)
	}

	err = walk(pinned)
	if err != nil && pinned.Revision != job.Revision && revisionRejected(err) {
		// Some mirrors resolve the commit but only serve branch/tag names.
		err = walk(job)
	}
	if err != nil {
		return nil, err
	}
	if urlJob.Revision != job.Revision && len(items) > 0 && !resolveAcceptsURL(ctx, httpc, token, items[0].URL) {
		// The tree accepted the commit but file downloads don't: fall back
		// to the branch/tag name for file URLs.
		for i := range items {
			if items[i].LFS {
				items[i].URL = lfsURL(cfg.Endpoint, job, items[i].RelativePath)
			} else {
				items[i].URL = rawURL(cfg.Endpoint, job, items[i].RelativePath)
			}
		}
	}
	return &Plan{Items: applyFilters(items, job.Filters, job.ExactMatch), Commit: commitSHA}, nil
}

// applyFilters keeps the items a filtered job should download and records
// the filter each matched (in Subdir, used by AppendFilterSubdir). Filters
// are case-insensitive and matched against the full repo path, so folder
// names select too (diffusers components like "unet", per-quant folders like
// "Q4_K_M/", dataset splits like "validation"). With filters set:
//   - a file matching any filter is kept (the longest matching filter wins);
//   - any other weight or data file (isPayloadFile) is dropped, whatever its
//     format (previously only six weight extensions were, so ONNX/TF/Flax
//     weights, parquet splits and imatrix files came along with every
//     filter);
//   - every other file — configs, tokenizers (tokenizer.model is LFS in
//     Llama/Mistral/Gemma repos), schedulers, docs, images — is kept, in
//     any folder: models need them to load (a diffusers pipeline filtered
//     by "fp16" still needs tokenizer/ and scheduler/).
func applyFilters(items []PlanItem, filters []string, exact bool) []PlanItem {
	var fs, orig []string // lowercased for matching; as given, for Subdir
	for _, list := range filters {
		// A single filter value may itself be a comma-separated list (the
		// analyzer's per-component file lists, sent as one item by the web UI).
		for _, f := range strings.Split(list, ",") {
			if f = strings.TrimSpace(f); f != "" {
				fs = append(fs, strings.ToLower(f))
				orig = append(orig, f)
			}
		}
	}
	if len(fs) == 0 {
		return items
	}

	var out []PlanItem
	for _, it := range items {
		relLower := strings.ToLower(it.RelativePath)
		matched := ""
		for j, f := range fs {
			if filterMatches(relLower, f, exact) && len(f) > len(matched) {
				matched = orig[j]
			}
		}
		switch {
		case matched != "":
			it.Subdir = matched
		case isPayloadFile(it.RelativePath):
			continue
		}
		out = append(out, it)
	}
	return out
}

// payloadExts are extensions of model weights, exported model formats and
// dataset/archive files: what filters choose between.
var payloadExts = map[string]bool{
	// weights and exported model formats
	".safetensors": true, ".bin": true, ".pt": true, ".pth": true, ".ckpt": true,
	".gguf": true, ".ggml": true, ".gguf_file": true, ".onnx": true, ".onnx_data": true,
	".msgpack": true, ".h5": true, ".tflite": true, ".ot": true, ".npz": true, ".npy": true,
	".pb": true, ".mlmodel": true, ".act": true, ".dat": true, // .dat: imatrix data
	// dataset files and archives
	".parquet": true, ".arrow": true, ".jsonl": true, ".csv": true, ".tsv": true,
	".tar": true, ".zip": true, ".gz": true, ".zst": true, ".xz": true, ".bz2": true,
}

// isPayloadFile reports whether a repo file is a weight, export or data
// file — dropped by filters it doesn't match — rather than a supporting file
// such as a tokenizer, config, doc or image.
func isPayloadFile(rel string) bool {
	return payloadExts[strings.ToLower(path.Ext(rel))]
}

// UnmatchedFiltersWarning returns a warning when a filtered job matched no
// file at all — usually a typo or a quant the repo doesn't have — since the
// download would otherwise quietly fetch only metadata.
func UnmatchedFiltersWarning(job Job, plan *Plan) string {
	var given []string
	for _, f := range job.Filters {
		if f = strings.TrimSpace(f); f != "" {
			given = append(given, f)
		}
	}
	if len(given) == 0 {
		return ""
	}
	for _, it := range plan.Items {
		if it.Subdir != "" {
			return ""
		}
	}
	return fmt.Sprintf("no files matched filter(s) %s; only repo metadata will be downloaded (run `hfdownloader analyze %s` to see what is available)",
		strings.Join(given, ","), job.Repo)
}

// revisionRejected reports whether err is an API response meaning the
// requested revision isn't accepted (as opposed to an auth or network error).
func revisionRejected(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusBadRequest)
}

// resolveAcceptsURL probes a file URL with HEAD. It returns false only on a
// definite 400/404, so transient failures never change which URLs are used.
func resolveAcceptsURL(ctx context.Context, httpc *http.Client, token, u string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return true
	}
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return true
	}
	resp.Body.Close()
	return resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusBadRequest
}

// filterMatches reports whether filter fLower matches the repo path relLower
// (both lowercased); see filtermatch.Match for the rules.
func filterMatches(relLower, fLower string, exact bool) bool {
	return filtermatch.Match(relLower, fLower, exact)
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

