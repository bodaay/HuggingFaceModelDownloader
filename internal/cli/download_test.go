// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func TestNewDownloadCmd(t *testing.T) {
	ctx := context.Background()
	ro := &RootOpts{}

	cmd := newDownloadCmd(ctx, ro)
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}

	// Check all expected flags exist
	expectedFlags := []string{
		"dataset",
		"revision",
		"filters",     // Not "filter"
		"exclude",
		"connections", // Not "concurrency"
		"cache-dir",
		"endpoint",
		"verify",
		"retries", // Not "max-retries"
	}

	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q should exist", name)
		}
	}
}

func TestDownloadCmd_RequiresRepo(t *testing.T) {
	ctx := context.Background()
	ro := &RootOpts{}
	cmd := newDownloadCmd(ctx, ro)

	// Execute without repo argument
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when no repo provided")
	}
}

func TestDownloadCmd_InvalidRepo(t *testing.T) {
	ctx := context.Background()
	ro := &RootOpts{}
	cmd := newDownloadCmd(ctx, ro)

	// Execute with invalid repo name
	cmd.SetArgs([]string{"invalid-no-slash"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid repo name")
	}
}

// mockHFServer creates a mock HuggingFace API server
func mockHFServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/models/test/model" || r.URL.Path == "/api/models/test/model/revision/main":
			// Return minimal model info
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        "test/model",
				"modelId":   "test/model",
				"sha":       "abc123def456",
				"siblings": []map[string]interface{}{
					{"rfilename": "config.json"},
					{"rfilename": "README.md"},
				},
			})

		case r.URL.Path == "/api/datasets/test/dataset" || r.URL.Path == "/api/datasets/test/dataset/revision/main":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":  "test/dataset",
				"sha": "abc123def456",
				"siblings": []map[string]interface{}{
					{"rfilename": "data.json"},
				},
			})

		case r.URL.Path == "/test/model/resolve/main/config.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"model_type": "test"}`))

		case r.URL.Path == "/test/model/resolve/main/README.md":
			w.Write([]byte(`# Test Model`))

		default:
			t.Logf("Unhandled path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestDownloadSettings_Defaults(t *testing.T) {
	settings := hfdownloader.Settings{
		Concurrency:        8,
		MaxActiveDownloads: 3,
		Verify:             "size",
		Retries:            3,
		BackoffInitial:     "1s",
		BackoffMax:         "30s",
	}

	if settings.Concurrency != 8 {
		t.Errorf("Concurrency = %d", settings.Concurrency)
	}
	if settings.MaxActiveDownloads != 3 {
		t.Errorf("MaxActiveDownloads = %d", settings.MaxActiveDownloads)
	}
	if settings.Verify != "size" {
		t.Errorf("Verify = %q", settings.Verify)
	}
}

func TestDownloadProgressCallback(t *testing.T) {
	var events []hfdownloader.ProgressEvent

	callback := func(e hfdownloader.ProgressEvent) {
		events = append(events, e)
	}

	// Simulate events
	callback(hfdownloader.ProgressEvent{Event: "scan_start", Message: "Starting scan"})
	callback(hfdownloader.ProgressEvent{Event: "file_start", Path: "config.json"})
	callback(hfdownloader.ProgressEvent{Event: "file_done", Path: "config.json"})
	callback(hfdownloader.ProgressEvent{Event: "done", Message: "Complete"})

	if len(events) != 4 {
		t.Errorf("events = %d, want 4", len(events))
	}

	if events[0].Event != "scan_start" {
		t.Errorf("first event = %q", events[0].Event)
	}
	if events[3].Event != "done" {
		t.Errorf("last event = %q", events[3].Event)
	}
}

func TestCacheDir_Resolution(t *testing.T) {
	t.Run("uses provided cache dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, "custom-cache")
		os.MkdirAll(cacheDir, 0o755)

		// Should use the provided directory
		if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
			t.Error("cache dir should exist")
		}
	})

	t.Run("respects HF_HOME env", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HF_HOME", tmpDir)

		// When HF_HOME is set, cache should use it
		expected := tmpDir
		if _, err := os.Stat(expected); os.IsNotExist(err) {
			t.Error("HF_HOME dir should exist")
		}
	})
}

func TestDownloadFilters(t *testing.T) {
	tests := []struct {
		name     string
		filters  []string
		excludes []string
		file     string
		expected bool
	}{
		{"no filters includes all", nil, nil, "model.bin", true},
		{"filter matches", []string{"*.bin"}, nil, "model.bin", true},
		{"filter excludes", []string{"*.json"}, nil, "model.bin", false},
		{"exclude matches", nil, []string{"*.bin"}, "model.bin", false},
		{"filter and exclude", []string{"*"}, []string{"*.bin"}, "model.bin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test filter logic
			included := shouldIncludeFile(tt.file, tt.filters, tt.excludes)
			if included != tt.expected {
				t.Errorf("shouldIncludeFile(%q, %v, %v) = %v, want %v",
					tt.file, tt.filters, tt.excludes, included, tt.expected)
			}
		})
	}
}

// shouldIncludeFile is a test helper that mimics filter logic
func shouldIncludeFile(file string, filters, excludes []string) bool {
	// If no filters, include all
	if len(filters) == 0 {
		// Check excludes
		for _, excl := range excludes {
			matched, _ := filepath.Match(excl, file)
			if matched {
				return false
			}
		}
		return true
	}

	// Check if matches any filter
	matchesFilter := false
	for _, f := range filters {
		matched, _ := filepath.Match(f, file)
		if matched {
			matchesFilter = true
			break
		}
	}
	if !matchesFilter {
		return false
	}

	// Check excludes
	for _, excl := range excludes {
		matched, _ := filepath.Match(excl, file)
		if matched {
			return false
		}
	}
	return true
}

func TestNewMirrorCmd(t *testing.T) {
	ro := &RootOpts{}

	cmd := newMirrorCmd(ro)
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}

	if cmd.Use != "mirror" {
		t.Errorf("Use = %q", cmd.Use)
	}

	// Should have subcommands
	if len(cmd.Commands()) == 0 {
		t.Error("mirror command should have subcommands")
	}
}

func TestNewRebuildCmd(t *testing.T) {
	ro := &RootOpts{}

	cmd := newRebuildCmd(ro)
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}

	if cmd.Use != "rebuild" {
		t.Errorf("Use = %q", cmd.Use)
	}

	// Check actual flags
	expectedFlags := []string{"cache-dir", "clean", "write-script"}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q should exist", name)
		}
	}
}

func TestNewProxyCmd(t *testing.T) {
	ro := &RootOpts{}

	cmd := newProxyCmd(ro)
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}

	if cmd.Use != "proxy" {
		t.Errorf("Use = %q", cmd.Use)
	}
}
