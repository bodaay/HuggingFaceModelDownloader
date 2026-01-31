// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"testing"
	"time"
)

func TestDefaultSettings(t *testing.T) {
	cfg := DefaultSettings()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"CacheDir", cfg.CacheDir, ""},
		{"StaleTimeout", cfg.StaleTimeout, "5m"},
		{"Concurrency", cfg.Concurrency, 8},
		{"MaxActiveDownloads", cfg.MaxActiveDownloads, 4},
		{"MultipartThreshold", cfg.MultipartThreshold, "256MiB"},
		{"Verify", cfg.Verify, "size"},
		{"Retries", cfg.Retries, 4},
		{"BackoffInitial", cfg.BackoffInitial, "400ms"},
		{"BackoffMax", cfg.BackoffMax, "10s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestSettings_BuildHFCache(t *testing.T) {
	t.Run("default settings", func(t *testing.T) {
		cfg := DefaultSettings()
		cache, err := cfg.BuildHFCache()
		if err != nil {
			t.Fatalf("BuildHFCache error: %v", err)
		}
		if cache == nil {
			t.Fatal("cache should not be nil")
		}
		if cache.StaleTimeout != DefaultStaleTimeout {
			t.Errorf("StaleTimeout = %v, want %v", cache.StaleTimeout, DefaultStaleTimeout)
		}
	})

	t.Run("custom cache dir", func(t *testing.T) {
		cfg := Settings{CacheDir: "/custom/path"}
		cache, err := cfg.BuildHFCache()
		if err != nil {
			t.Fatalf("BuildHFCache error: %v", err)
		}
		if cache.Root != "/custom/path" {
			t.Errorf("Root = %q, want /custom/path", cache.Root)
		}
	})

	t.Run("custom stale timeout", func(t *testing.T) {
		cfg := Settings{StaleTimeout: "10m"}
		cache, err := cfg.BuildHFCache()
		if err != nil {
			t.Fatalf("BuildHFCache error: %v", err)
		}
		if cache.StaleTimeout != 10*time.Minute {
			t.Errorf("StaleTimeout = %v, want 10m", cache.StaleTimeout)
		}
	})

	t.Run("invalid stale timeout", func(t *testing.T) {
		cfg := Settings{StaleTimeout: "invalid"}
		_, err := cfg.BuildHFCache()
		if err == nil {
			t.Error("expected error for invalid stale timeout")
		}
	})

	t.Run("various duration formats", func(t *testing.T) {
		durations := []struct {
			input    string
			expected time.Duration
		}{
			{"1s", 1 * time.Second},
			{"30s", 30 * time.Second},
			{"1m", 1 * time.Minute},
			{"5m", 5 * time.Minute},
			{"1h", 1 * time.Hour},
			{"1h30m", 90 * time.Minute},
		}

		for _, d := range durations {
			t.Run(d.input, func(t *testing.T) {
				cfg := Settings{StaleTimeout: d.input}
				cache, err := cfg.BuildHFCache()
				if err != nil {
					t.Fatalf("BuildHFCache error: %v", err)
				}
				if cache.StaleTimeout != d.expected {
					t.Errorf("StaleTimeout = %v, want %v", cache.StaleTimeout, d.expected)
				}
			})
		}
	})
}

func TestJob_Fields(t *testing.T) {
	job := Job{
		Repo:               "TheBloke/Mistral-7B-GGUF",
		IsDataset:          false,
		Revision:           "main",
		Filters:            []string{"q4_k_m", "q5_k_m"},
		Excludes:           []string{".md"},
		AppendFilterSubdir: true,
	}

	if job.Repo != "TheBloke/Mistral-7B-GGUF" {
		t.Errorf("Repo = %q", job.Repo)
	}
	if job.IsDataset {
		t.Error("IsDataset should be false")
	}
	if job.Revision != "main" {
		t.Errorf("Revision = %q", job.Revision)
	}
	if len(job.Filters) != 2 {
		t.Errorf("Filters length = %d, want 2", len(job.Filters))
	}
	if len(job.Excludes) != 1 {
		t.Errorf("Excludes length = %d, want 1", len(job.Excludes))
	}
	if !job.AppendFilterSubdir {
		t.Error("AppendFilterSubdir should be true")
	}
}

func TestProgressEvent_Fields(t *testing.T) {
	now := time.Now().UTC()
	event := ProgressEvent{
		Time:       now,
		Level:      "info",
		Event:      "file_progress",
		Repo:       "owner/repo",
		Revision:   "main",
		Path:       "model.gguf",
		Bytes:      1024,
		Total:      4096,
		Downloaded: 2048,
		Attempt:    1,
		Message:    "downloading",
		IsLFS:      true,
	}

	if event.Time != now {
		t.Error("Time mismatch")
	}
	if event.Level != "info" {
		t.Errorf("Level = %q", event.Level)
	}
	if event.Event != "file_progress" {
		t.Errorf("Event = %q", event.Event)
	}
	if event.Repo != "owner/repo" {
		t.Errorf("Repo = %q", event.Repo)
	}
	if event.Bytes != 1024 {
		t.Errorf("Bytes = %d", event.Bytes)
	}
	if event.Total != 4096 {
		t.Errorf("Total = %d", event.Total)
	}
	if event.Downloaded != 2048 {
		t.Errorf("Downloaded = %d", event.Downloaded)
	}
	if !event.IsLFS {
		t.Error("IsLFS should be true")
	}
}

func TestSettings_ProxyConfig(t *testing.T) {
	t.Run("nil proxy", func(t *testing.T) {
		cfg := Settings{}
		if cfg.Proxy != nil {
			t.Error("Proxy should be nil by default")
		}
	})

	t.Run("with proxy config", func(t *testing.T) {
		cfg := Settings{
			Proxy: &ProxyConfig{
				URL:      "http://proxy:8080",
				Username: "user",
				Password: "pass",
			},
		}
		if cfg.Proxy == nil {
			t.Fatal("Proxy should not be nil")
		}
		if cfg.Proxy.URL != "http://proxy:8080" {
			t.Errorf("Proxy.URL = %q", cfg.Proxy.URL)
		}
		if cfg.Proxy.Username != "user" {
			t.Errorf("Proxy.Username = %q", cfg.Proxy.Username)
		}
	})
}

func TestRepoType_Constants(t *testing.T) {
	if RepoTypeModel != "model" {
		t.Errorf("RepoTypeModel = %q, want 'model'", RepoTypeModel)
	}
	if RepoTypeDataset != "dataset" {
		t.Errorf("RepoTypeDataset = %q, want 'dataset'", RepoTypeDataset)
	}
}

func TestJob_DatasetField(t *testing.T) {
	t.Run("model job", func(t *testing.T) {
		job := Job{Repo: "owner/model", IsDataset: false}
		if job.IsDataset {
			t.Error("IsDataset should be false for model")
		}
	})

	t.Run("dataset job", func(t *testing.T) {
		job := Job{Repo: "owner/dataset", IsDataset: true}
		if !job.IsDataset {
			t.Error("IsDataset should be true for dataset")
		}
	})
}

func TestSettings_Endpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"default", ""},
		{"huggingface", "https://huggingface.co"},
		{"mirror", "https://hf-mirror.com"},
		{"enterprise", "https://enterprise.example.com/hf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Settings{Endpoint: tt.endpoint}
			if cfg.Endpoint != tt.endpoint {
				t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, tt.endpoint)
			}
		})
	}
}

func TestSettings_VerifyModes(t *testing.T) {
	modes := []string{"none", "size", "etag", "sha256"}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			cfg := Settings{Verify: mode}
			if cfg.Verify != mode {
				t.Errorf("Verify = %q, want %q", cfg.Verify, mode)
			}
		})
	}
}

func TestProgressFunc(t *testing.T) {
	var events []ProgressEvent

	progress := ProgressFunc(func(e ProgressEvent) {
		events = append(events, e)
	})

	// Simulate events
	progress(ProgressEvent{Event: "file_start", Path: "file1.txt"})
	progress(ProgressEvent{Event: "file_done", Path: "file1.txt"})

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
	if events[0].Event != "file_start" {
		t.Errorf("first event = %q", events[0].Event)
	}
	if events[1].Event != "file_done" {
		t.Errorf("second event = %q", events[1].Event)
	}
}
