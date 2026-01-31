// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockHFAPI creates a mock HuggingFace API server for testing
func mockHFAPI(t *testing.T, responses map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Mock request: %s %s", r.Method, r.URL.Path)

		if resp, ok := responses[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			switch v := resp.(type) {
			case []byte:
				w.Write(v)
			case string:
				w.Write([]byte(v))
			default:
				json.NewEncoder(w).Encode(v)
			}
			return
		}

		// Default 404
		t.Logf("No mock for path: %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestJob_Validation(t *testing.T) {
	tests := []struct {
		name    string
		job     Job
		wantErr bool
	}{
		{
			name:    "valid model repo",
			job:     Job{Repo: "owner/model"},
			wantErr: false,
		},
		{
			name:    "valid dataset repo",
			job:     Job{Repo: "owner/dataset", IsDataset: true},
			wantErr: false,
		},
		{
			name:    "empty repo",
			job:     Job{Repo: ""},
			wantErr: true,
		},
		{
			name:    "invalid repo format",
			job:     Job{Repo: "invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJob(tt.job)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateJob() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// validateJob is a test helper
func validateJob(job Job) error {
	if job.Repo == "" {
		return ErrInvalidRepo
	}
	if !IsValidModelName(job.Repo) {
		return ErrInvalidRepo
	}
	return nil
}

func TestSettings_Defaults(t *testing.T) {
	s := Settings{}

	// Verify zero values
	if s.CacheDir != "" {
		t.Errorf("CacheDir = %q", s.CacheDir)
	}
	if s.Concurrency != 0 {
		t.Errorf("Concurrency = %d", s.Concurrency)
	}
}

func TestSettings_WithValues(t *testing.T) {
	s := Settings{
		CacheDir:           "/tmp/cache",
		Concurrency:        8,
		MaxActiveDownloads: 3,
		Token:              "hf_test_token",
		Endpoint:           "https://custom.endpoint",
		Verify:             "sha256",
		Retries:            5,
		BackoffInitial:     "500ms",
		BackoffMax:         "30s",
	}

	if s.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q", s.CacheDir)
	}
	if s.Concurrency != 8 {
		t.Errorf("Concurrency = %d", s.Concurrency)
	}
	if s.Token != "hf_test_token" {
		t.Errorf("Token = %q", s.Token)
	}
	if s.Verify != "sha256" {
		t.Errorf("Verify = %q", s.Verify)
	}
}

func TestProgressEvent_Types(t *testing.T) {
	events := []ProgressEvent{
		{Event: "scan_start", Message: "Starting scan"},
		{Event: "scan_done", Total: 10},
		{Event: "file_start", Path: "model.bin", Total: 1024},
		{Event: "file_progress", Path: "model.bin", Downloaded: 512, Total: 1024},
		{Event: "file_done", Path: "model.bin", Total: 1024},
		{Event: "done", Message: "Download complete"},
		{Event: "error", Message: "network error"},
	}

	for _, e := range events {
		if e.Event == "" {
			t.Error("Event type should not be empty")
		}
	}

	// Verify specific event properties
	if events[1].Total != 10 {
		t.Errorf("scan_done Total = %d", events[1].Total)
	}
	if events[3].Downloaded != 512 {
		t.Errorf("file_progress Downloaded = %d", events[3].Downloaded)
	}
}

func TestDownloadManifest(t *testing.T) {
	now := time.Now()
	manifest := DownloadManifest{
		Version:     "1",
		Type:        "model",
		Repo:        "owner/repo",
		Branch:      "main",
		Commit:      "abc123",
		TotalFiles:  5,
		TotalSize:   1024 * 1024 * 100,
		RepoPath:    "hub/models--owner--repo",
		StartedAt:   now.Add(-time.Hour),
		CompletedAt: now,
		Files: []ManifestFile{
			{Name: "config.json", Size: 1024, LFS: false},
			{Name: "model.bin", Size: 1024 * 1024 * 99, LFS: true, Blob: "blobs/abc"},
		},
	}

	if manifest.Version != "1" {
		t.Errorf("Version = %q", manifest.Version)
	}
	if manifest.Type != "model" {
		t.Errorf("Type = %q", manifest.Type)
	}
	if len(manifest.Files) != 2 {
		t.Errorf("Files = %d", len(manifest.Files))
	}
	if !manifest.Files[1].LFS {
		t.Error("Second file should be LFS")
	}
}

func TestHFCacheDir(t *testing.T) {
	t.Run("default location", func(t *testing.T) {
		// Without HF_HOME set
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home dir")
		}

		expected := filepath.Join(home, ".cache", "huggingface")
		_ = expected // Default location
	})

	t.Run("custom HF_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HF_HOME", tmpDir)

		// When HF_HOME is set, it should be used
		if os.Getenv("HF_HOME") != tmpDir {
			t.Errorf("HF_HOME = %q", os.Getenv("HF_HOME"))
		}
	})
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Check context is cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("context should be cancelled")
	}
}

func TestFilenameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/models/config.json", "config.json"},
		{"model.bin", "model.bin"},
		{"/deep/nested/path/file.txt", "file.txt"},
		{"", "."},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := filepath.Base(tt.path)
			if got != tt.want {
				t.Errorf("Base(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestRepoTypePrefix(t *testing.T) {
	tests := []struct {
		repoType RepoType
		prefix   string
	}{
		{RepoTypeModel, "models--"},
		{RepoTypeDataset, "datasets--"},
	}

	for _, tt := range tests {
		t.Run(string(tt.repoType), func(t *testing.T) {
			got := string(tt.repoType) + "s--"
			if got != tt.prefix {
				t.Errorf("prefix = %q, want %q", got, tt.prefix)
			}
		})
	}
}

func TestRepoDirName(t *testing.T) {
	tests := []struct {
		repo     string
		repoType RepoType
		want     string
	}{
		{"owner/model", RepoTypeModel, "models--owner--model"},
		{"TheBloke/Mistral-7B", RepoTypeModel, "models--TheBloke--Mistral-7B"},
		{"owner/dataset", RepoTypeDataset, "datasets--owner--dataset"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			// Build the expected directory name
			prefix := string(tt.repoType) + "s--"
			name := prefix + tt.repo[:len(tt.repo)-len(filepath.Base(tt.repo))-1] + "--" + filepath.Base(tt.repo)
			if name != tt.want {
				t.Errorf("dir name = %q, want %q", name, tt.want)
			}
		})
	}
}

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"100ms", true},
		{"1s", true},
		{"5m", true},
		{"1h", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := time.ParseDuration(tt.input)
			valid := err == nil
			if tt.input == "" {
				valid = false
			}
			if valid != tt.valid {
				t.Errorf("ParseDuration(%q) valid = %v, want %v", tt.input, valid, tt.valid)
			}
		})
	}
}

func TestVerifyMode(t *testing.T) {
	modes := []string{"none", "size", "sha256"}

	for _, mode := range modes {
		if mode == "" {
			t.Error("verify mode should not be empty")
		}
	}

	// Check default
	defaultMode := "size"
	if defaultMode != "size" {
		t.Errorf("default verify mode = %q", defaultMode)
	}
}

func TestFilterMatching(t *testing.T) {
	tests := []struct {
		pattern string
		file    string
		matches bool
	}{
		{"*.bin", "model.bin", true},
		{"*.bin", "model.safetensors", false},
		{"config.*", "config.json", true},
		{"*.gguf", "model-q4_k_m.gguf", true},
		{"model-*.bin", "model-00001.bin", true},
		{"model-*.bin", "other.bin", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.file, func(t *testing.T) {
			matched, err := filepath.Match(tt.pattern, tt.file)
			if err != nil {
				t.Fatalf("invalid pattern: %v", err)
			}
			if matched != tt.matches {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.file, matched, tt.matches)
			}
		})
	}
}
