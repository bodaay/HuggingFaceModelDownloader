// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import "time"

// Job defines what to download from the HuggingFace Hub.
//
// A Job specifies the repository, revision, and optional filters for selecting
// which files to download. The Repo field is required and must be in
// "owner/name" format (e.g., "TheBloke/Mistral-7B-GGUF").
//
// Example:
//
//	job := hfdownloader.Job{
//	    Repo:     "TheBloke/Mistral-7B-GGUF",
//	    Revision: "main",
//	    Filters:  []string{"q4_k_m"},
//	}
type Job struct {
	// Repo is the repository ID in "owner/name" format.
	// This field is required.
	//
	// Examples:
	//   - "TheBloke/Mistral-7B-GGUF"
	//   - "meta-llama/Llama-2-7b"
	//   - "facebook/flores" (dataset)
	Repo string

	// IsDataset indicates this is a dataset repo, not a model.
	// When true, the HuggingFace datasets API is used instead of the models API.
	IsDataset bool

	// Revision is the branch, tag, or commit SHA to download.
	// If empty, defaults to "main".
	//
	// Examples:
	//   - "main" (default branch)
	//   - "v1.0" (tag)
	//   - "abc123" (commit SHA)
	Revision string

	// Filters specify which LFS files to download, matched case-insensitively.
	// If empty, all files are downloaded.
	//
	// Each filter is matched as a substring against file names. A file is
	// included if it contains any of the filter strings.
	//
	// Examples:
	//   - []string{"q4_0"} matches "model.Q4_0.gguf"
	//   - []string{"q4_k_m", "q5_k_m"} matches either quantization
	//   - []string{"gguf"} matches all GGUF files
	Filters []string

	// AppendFilterSubdir puts each filter's matched files in a subdirectory
	// named after the filter. Useful for organizing multiple quantizations.
	//
	// When true, a file matching filter "q4_0" would be saved as:
	//   <output>/<repo>/q4_0/<filename>
	// Instead of:
	//   <output>/<repo>/<filename>
	AppendFilterSubdir bool
}

// Settings configures download behavior.
//
// All fields have sensible defaults. At minimum, you only need to set
// OutputDir for where files should be saved.
//
// Example with defaults:
//
//	cfg := hfdownloader.Settings{
//	    OutputDir: "./Models",
//	}
//
// Example with full configuration:
//
//	cfg := hfdownloader.Settings{
//	    OutputDir:          "./Models",
//	    Concurrency:        8,
//	    MaxActiveDownloads: 4,
//	    MultipartThreshold: "32MiB",
//	    Verify:             "sha256",
//	    Retries:            4,
//	    Token:              os.Getenv("HF_TOKEN"),
//	}
type Settings struct {
	// OutputDir is the base directory for downloads.
	// Files are saved as: <OutputDir>/<owner>/<repo>/<path>
	// If empty, defaults to "Storage".
	OutputDir string

	// Concurrency is the number of parallel HTTP connections per file
	// when using multipart downloads. Higher values can improve speed
	// on fast networks but increase memory usage.
	// If <= 0, defaults to 8.
	Concurrency int

	// MaxActiveDownloads limits how many files download simultaneously.
	// This controls overall parallelism across all files in a job.
	// If <= 0, defaults to GOMAXPROCS (number of CPU cores).
	MaxActiveDownloads int

	// MultipartThreshold is the minimum file size to use multipart downloads.
	// Files smaller than this are downloaded in a single request.
	// Accepts human-readable sizes: "32MiB", "256MB", "1GiB", etc.
	// If empty, defaults to "256MiB".
	MultipartThreshold string

	// Verify specifies how to verify non-LFS files after download.
	// LFS files are always verified by SHA-256 when the hash is available.
	//
	// Options:
	//   - "none": No verification (fastest)
	//   - "size": Verify file size matches expected (default, fast)
	//   - "etag": Compare ETag header from server
	//   - "sha256": Full SHA-256 hash verification (most secure, slower)
	Verify string

	// Retries is the maximum number of retry attempts per HTTP request.
	// Each retry uses exponential backoff with jitter.
	// If <= 0, defaults to 4.
	Retries int

	// BackoffInitial is the initial delay before the first retry.
	// Accepts duration strings: "400ms", "1s", "2s", etc.
	// If empty, defaults to "400ms".
	BackoffInitial string

	// BackoffMax is the maximum delay between retries.
	// The actual delay grows exponentially but caps at this value.
	// If empty, defaults to "10s".
	BackoffMax string

	// Token is the HuggingFace access token for private or gated repos.
	// Get yours at: https://huggingface.co/settings/tokens
	// Can also be set via HF_TOKEN environment variable.
	Token string
}

// ProgressEvent represents a progress update during download.
//
// Events are emitted throughout the download process to allow for
// progress display, logging, or integration with other systems.
//
// The Event field indicates the type of event:
//   - "scan_start": Repository scanning has begun
//   - "plan_item": A file has been added to the download plan
//   - "file_start": Download of a file has started
//   - "file_progress": Periodic progress update during download
//   - "file_done": File download complete (check Message for "skip" info)
//   - "retry": A retry attempt is being made
//   - "error": An error occurred
//   - "done": All downloads complete
type ProgressEvent struct {
	// Time is when the event occurred (UTC).
	Time time.Time `json:"time"`

	// Level is the log level: "debug", "info", "warn", "error".
	// Empty defaults to "info".
	Level string `json:"level,omitempty"`

	// Event is the event type identifier.
	Event string `json:"event"`

	// Repo is the repository being processed.
	Repo string `json:"repo,omitempty"`

	// Revision is the branch/tag/commit being downloaded.
	Revision string `json:"revision,omitempty"`

	// Path is the relative file path within the repository.
	Path string `json:"path,omitempty"`

	// Bytes is the number of bytes in the current progress update.
	// Used in "file_progress" events.
	Bytes int64 `json:"bytes,omitempty"`

	// Total is the total expected size in bytes.
	Total int64 `json:"total,omitempty"`

	// Downloaded is the cumulative bytes downloaded so far.
	Downloaded int64 `json:"downloaded,omitempty"`

	// Attempt is the retry attempt number (1-based).
	// Only set in "retry" events.
	Attempt int `json:"attempt,omitempty"`

	// Message contains additional context or error details.
	// For "file_done" events, may contain "skip (reason)" if skipped.
	Message string `json:"message,omitempty"`

	// IsLFS indicates whether this file is stored in Git LFS.
	IsLFS bool `json:"isLfs,omitempty"`
}

// ProgressFunc is a callback for receiving progress events.
//
// Implement this to display progress in a UI, log events, or track downloads.
// The callback is invoked from multiple goroutines and should be thread-safe.
//
// Example:
//
//	progress := func(e hfdownloader.ProgressEvent) {
//	    switch e.Event {
//	    case "file_start":
//	        fmt.Printf("Downloading: %s\n", e.Path)
//	    case "file_done":
//	        fmt.Printf("Complete: %s\n", e.Path)
//	    case "error":
//	        fmt.Printf("Error: %s\n", e.Message)
//	    }
//	}
type ProgressFunc func(ProgressEvent)

// DefaultSettings returns Settings with sensible defaults filled in.
//
// Use this as a starting point and override specific fields:
//
//	cfg := hfdownloader.DefaultSettings()
//	cfg.OutputDir = "./MyModels"
//	cfg.Token = os.Getenv("HF_TOKEN")
func DefaultSettings() Settings {
	return Settings{
		OutputDir:          "Storage",
		Concurrency:        8,
		MaxActiveDownloads: 4,
		MultipartThreshold: "256MiB",
		Verify:             "size",
		Retries:            4,
		BackoffInitial:     "400ms",
		BackoffMax:         "10s",
	}
}
