// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func TestRootOpts(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		ro := &RootOpts{}
		if ro.Token != "" {
			t.Errorf("Token = %q, want empty", ro.Token)
		}
		if ro.JSONOut {
			t.Error("JSONOut should be false")
		}
		if ro.Quiet {
			t.Error("Quiet should be false")
		}
		if ro.Verbose {
			t.Error("Verbose should be false")
		}
	})

	t.Run("with values", func(t *testing.T) {
		ro := &RootOpts{
			Token:    "test-token",
			JSONOut:  true,
			Quiet:    true,
			Verbose:  true,
			Config:   "/path/to/config",
			LogFile:  "/path/to/log",
			LogLevel: "debug",
		}
		if ro.Token != "test-token" {
			t.Errorf("Token = %q", ro.Token)
		}
		if !ro.JSONOut {
			t.Error("JSONOut should be true")
		}
		if ro.LogLevel != "debug" {
			t.Errorf("LogLevel = %q", ro.LogLevel)
		}
	})
}

func TestSplitComma(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty", "", nil},
		{"single", "q4_k_m", []string{"q4_k_m"}},
		{"multiple", "q4_k_m,q5_k_m", []string{"q4_k_m", "q5_k_m"}},
		{"with spaces", "q4_k_m, q5_k_m , q8_0", []string{"q4_k_m", "q5_k_m", "q8_0"}},
		{"trailing comma", "q4_k_m,", []string{"q4_k_m"}},
		{"leading comma", ",q4_k_m", []string{"q4_k_m"}},
		{"empty parts", "q4_k_m,,q5_k_m", []string{"q4_k_m", "q5_k_m"}},
		{"whitespace only", "  ,  ,  ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitComma(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitComma(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("splitComma(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestBuildCommandString(t *testing.T) {
	// Helper to create default settings
	defaultSettings := func() hfdownloader.Settings {
		return hfdownloader.Settings{
			Concurrency:        8,
			MaxActiveDownloads: 3,
			Verify:             "size",
		}
	}

	t.Run("basic command", func(t *testing.T) {
		job := hfdownloader.Job{
			Repo: "owner/repo",
		}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		if cmd != "hfdownloader download owner/repo" {
			t.Errorf("cmd = %q", cmd)
		}
	})

	t.Run("with dataset flag", func(t *testing.T) {
		job := hfdownloader.Job{
			Repo:      "owner/dataset",
			IsDataset: true,
		}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/dataset --dataset"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with revision", func(t *testing.T) {
		job := hfdownloader.Job{
			Repo:     "owner/repo",
			Revision: "dev",
		}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo -b dev"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with filters", func(t *testing.T) {
		job := hfdownloader.Job{
			Repo:    "owner/repo",
			Filters: []string{"q4_k_m", "q5_k_m"},
		}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo -F q4_k_m -F q5_k_m"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with excludes", func(t *testing.T) {
		job := hfdownloader.Job{
			Repo:     "owner/repo",
			Excludes: []string{".md", "fp16"},
		}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo -E .md -E fp16"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with custom concurrency", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()
		cfg.Concurrency = 16

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo -c 16"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("default concurrency omitted", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with proxy", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()
		cfg.Proxy = &hfdownloader.ProxyConfig{
			URL:      "http://proxy:8080",
			Username: "user", // Should be omitted
			Password: "pass", // Should be omitted
		}

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo --proxy http://proxy:8080"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with legacy output", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()
		cfg.OutputDir = "Models"

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo --legacy"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with custom legacy output", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()
		cfg.OutputDir = "/custom/output"

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo --legacy -o /custom/output"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("with verify mode", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()
		cfg.Verify = "sha256"

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo --verify sha256"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})

	t.Run("default verify mode omitted", func(t *testing.T) {
		job := hfdownloader.Job{Repo: "owner/repo"}
		cfg := defaultSettings()

		cmd := buildCommandString(nil, job, cfg)
		expected := "hfdownloader download owner/repo"
		if cmd != expected {
			t.Errorf("cmd = %q, want %q", cmd, expected)
		}
	})
}

func TestJSONProgress(t *testing.T) {
	var buf bytes.Buffer
	progress := jsonProgress(&buf)

	event := hfdownloader.ProgressEvent{
		Event: "file_start",
		Path:  "model.bin",
		Total: 1024,
	}

	progress(event)

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["event"] != "file_start" {
		t.Errorf("event = %v", result["event"])
	}
	if result["path"] != "model.bin" {
		t.Errorf("path = %v", result["path"])
	}
}

func TestCLIProgress(t *testing.T) {
	ro := &RootOpts{Quiet: true}
	job := hfdownloader.Job{
		Repo:     "owner/repo",
		Revision: "main",
	}

	progress := cliProgress(ro, job)

	// Test various event types
	events := []hfdownloader.ProgressEvent{
		{Event: "scan_start"},
		{Event: "file_start", Path: "model.bin", Total: 1024},
		{Event: "file_done", Path: "model.bin", Message: "done"},
		{Event: "file_done", Path: "skipped.bin", Message: "skip: size match"},
		{Event: "retry", Path: "retry.bin", Attempt: 2, Message: "timeout"},
		{Event: "error", Message: "connection failed"},
		{Event: "done", Message: "completed"},
	}

	// Just ensure no panics
	for _, ev := range events {
		progress(ev)
	}
}

func TestApplySettingsDefaults(t *testing.T) {
	// Note: applySettingsDefaults requires a valid cobra.Command for flag checking.
	// These tests cover the path where no config file exists (early return).

	t.Run("no config file", func(t *testing.T) {
		// Use temp directory without any config
		origHome := os.Getenv("HOME")
		defer os.Setenv("HOME", origHome)

		tmpDir := t.TempDir()
		os.Setenv("HOME", tmpDir)

		ro := &RootOpts{}
		cfg := &hfdownloader.Settings{}

		// When no config file exists and no explicit path is set, returns nil
		err := applySettingsDefaults(nil, ro, cfg)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid config path", func(t *testing.T) {
		ro := &RootOpts{Config: "/nonexistent/path"}
		cfg := &hfdownloader.Settings{}

		err := applySettingsDefaults(nil, ro, cfg)
		if err == nil {
			t.Error("expected error for invalid config path")
		}
	})

	t.Run("invalid JSON content", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "bad.json")
		os.WriteFile(configPath, []byte("{ invalid json"), 0o644)

		ro := &RootOpts{Config: configPath}
		cfg := &hfdownloader.Settings{}

		err := applySettingsDefaults(nil, ro, cfg)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("invalid YAML content", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "bad.yaml")
		os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0o644)

		ro := &RootOpts{Config: configPath}
		cfg := &hfdownloader.Settings{}

		err := applySettingsDefaults(nil, ro, cfg)
		if err == nil {
			t.Error("expected error for invalid YAML")
		}
	})
}

func TestSignalContext(t *testing.T) {
	// Test that signalContext creates a valid context
	ctx, cancel := signalContext(context.Background())
	defer cancel()

	if ctx == nil {
		t.Error("context should not be nil")
	}

	// Cancel should work without panic
	cancel()
}
