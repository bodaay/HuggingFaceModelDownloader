// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import "time"

// Options captures all configurable knobs for a download session (v2).
type Options struct {
	Repo      string // "owner/name"
	IsDataset bool   // treat as dataset
	Revision  string // branch/tag/sha
	OutputDir string // base destination directory

	// Filtering
	Filters []string // filter tokens for LFS artifacts (name contains any of these)

	// Layout
	AppendFilterSubdir bool // when exactly one filter is used, append it as a subfolder

	// Concurrency/throughput
	Concurrency        int    // per-file connections for range requests
	MaxActiveDownloads int    // overall files downloading at once
	MultipartThreshold string // size string (e.g. "32MiB")

	// Verification for non-LFS files. Allowed: "none", "size", "etag", "sha256"
	Verify string

	// Retry policy
	Retries        int
	BackoffInitial string // duration string
	BackoffMax     string // duration string

	// Behavior
	DryRun     bool
	PlanFormat string // "table" or "json"
	Resume     bool
	Overwrite  bool

	// Auth
	Token string

	// Logging
	JSON    bool
	Quiet   bool
	Verbose bool

	// Optional progress callback
	Progress ProgressFunc
}

// ProgressEvent describes structured progress / logs.
type ProgressEvent struct {
	Time     time.Time `json:"time"`
	Level    string    `json:"level,omitempty"` // info|warn|error
	Event    string    `json:"event"`           // scan_start|plan_item|file_start|file_progress|file_done|retry|done|error
	Repo     string    `json:"repo,omitempty"`
	Revision string    `json:"revision,omitempty"`
	Path     string    `json:"path,omitempty"`
	Bytes    int64     `json:"bytes,omitempty"`
	Total    int64     `json:"total,omitempty"`
	Attempt  int       `json:"attempt,omitempty"`
	Message  string    `json:"message,omitempty"`
}

// ProgressFunc is called by the downloader with rich progress events.
type ProgressFunc func(ProgressEvent)
