// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func TestListEntry(t *testing.T) {
	entry := ListEntry{
		Type:       "model",
		Repo:       "owner/repo",
		Branch:     "main",
		Commit:     "abc1234",
		Files:      10,
		Size:       1024 * 1024 * 100,
		SizeHuman:  "100.0 MB",
		Downloaded: "2025-01-01",
		Path:       "/cache/hub/models--owner--repo",
	}

	if entry.Type != "model" {
		t.Errorf("Type = %q", entry.Type)
	}
	if entry.Repo != "owner/repo" {
		t.Errorf("Repo = %q", entry.Repo)
	}
	if entry.Files != 10 {
		t.Errorf("Files = %d", entry.Files)
	}
}

func TestSortEntries(t *testing.T) {
	entries := []ListEntry{
		{Repo: "z-repo", Size: 100, Downloaded: "2025-01-01"},
		{Repo: "a-repo", Size: 300, Downloaded: "2025-01-03"},
		{Repo: "m-repo", Size: 200, Downloaded: "2025-01-02"},
	}

	t.Run("sort by name", func(t *testing.T) {
		e := make([]ListEntry, len(entries))
		copy(e, entries)
		sortEntries(e, "name")

		if e[0].Repo != "a-repo" {
			t.Errorf("first = %q, want a-repo", e[0].Repo)
		}
		if e[1].Repo != "m-repo" {
			t.Errorf("second = %q, want m-repo", e[1].Repo)
		}
		if e[2].Repo != "z-repo" {
			t.Errorf("third = %q, want z-repo", e[2].Repo)
		}
	})

	t.Run("sort by size", func(t *testing.T) {
		e := make([]ListEntry, len(entries))
		copy(e, entries)
		sortEntries(e, "size")

		if e[0].Repo != "a-repo" { // Largest first
			t.Errorf("first = %q, want a-repo (largest)", e[0].Repo)
		}
		if e[2].Repo != "z-repo" {
			t.Errorf("third = %q, want z-repo (smallest)", e[2].Repo)
		}
	})

	t.Run("sort by date", func(t *testing.T) {
		e := make([]ListEntry, len(entries))
		copy(e, entries)
		sortEntries(e, "date")

		if e[0].Repo != "a-repo" { // Newest first
			t.Errorf("first = %q, want a-repo (newest)", e[0].Repo)
		}
		if e[2].Repo != "z-repo" {
			t.Errorf("third = %q, want z-repo (oldest)", e[2].Repo)
		}
	})

	t.Run("default is name", func(t *testing.T) {
		e := make([]ListEntry, len(entries))
		copy(e, entries)
		sortEntries(e, "unknown")

		if e[0].Repo != "a-repo" {
			t.Errorf("first = %q, want a-repo", e[0].Repo)
		}
	})
}

func TestShortCommit(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abc1234567890", "abc1234"},
		{"abc1234", "abc1234"},
		{"abc123", "abc123"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := shortCommit(tt.input)
			if result != tt.expected {
				t.Errorf("shortCommit(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 512, "512.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 5, "5.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := humanSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestScanCacheStructure(t *testing.T) {
	t.Run("empty cache", func(t *testing.T) {
		tmpDir := t.TempDir()
		entries, err := scanCacheStructure(tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("entries = %d, want 0", len(entries))
		}
	})

	t.Run("with models and datasets", func(t *testing.T) {
		tmpDir := t.TempDir()
		hubDir := filepath.Join(tmpDir, "hub")

		// Create model directory structure
		modelDir := filepath.Join(hubDir, "models--owner--model1")
		blobsDir := filepath.Join(modelDir, "blobs")
		refsDir := filepath.Join(modelDir, "refs")
		os.MkdirAll(blobsDir, 0o755)
		os.MkdirAll(refsDir, 0o755)
		os.WriteFile(filepath.Join(blobsDir, "abc123"), []byte("blob content"), 0o644)
		os.WriteFile(filepath.Join(refsDir, "main"), []byte("abc123def456"), 0o644)

		// Create dataset directory structure
		datasetDir := filepath.Join(hubDir, "datasets--owner--dataset1")
		datasetBlobsDir := filepath.Join(datasetDir, "blobs")
		os.MkdirAll(datasetBlobsDir, 0o755)
		os.WriteFile(filepath.Join(datasetBlobsDir, "def456"), []byte("dataset blob"), 0o644)

		entries, err := scanCacheStructure(tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("entries = %d, want 2", len(entries))
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		tmpDir := t.TempDir()
		hubDir := filepath.Join(tmpDir, "hub")

		// Create model and dataset
		os.MkdirAll(filepath.Join(hubDir, "models--owner--model1", "blobs"), 0o755)
		os.MkdirAll(filepath.Join(hubDir, "datasets--owner--dataset1", "blobs"), 0o755)

		entries, err := scanCacheStructure(tmpDir, "model")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("entries = %d, want 1 (only models)", len(entries))
		}
		if entries[0].Type != "model" {
			t.Errorf("Type = %q, want model", entries[0].Type)
		}
	})

	t.Run("repo name conversion", func(t *testing.T) {
		tmpDir := t.TempDir()
		hubDir := filepath.Join(tmpDir, "hub")

		os.MkdirAll(filepath.Join(hubDir, "models--TheBloke--Mistral-7B-GGUF", "blobs"), 0o755)

		entries, err := scanCacheStructure(tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}
		if entries[0].Repo != "TheBloke/Mistral-7B-GGUF" {
			t.Errorf("Repo = %q, want TheBloke/Mistral-7B-GGUF", entries[0].Repo)
		}
	})
}

func TestScanManifests(t *testing.T) {
	t.Run("empty cache", func(t *testing.T) {
		tmpDir := t.TempDir()
		entries, err := scanManifests(tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("entries = %d, want 0", len(entries))
		}
	})

	t.Run("with manifest files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create models directory with manifest
		modelDir := filepath.Join(tmpDir, "models", "owner", "repo")
		os.MkdirAll(modelDir, 0o755)

		manifest := hfdownloader.DownloadManifest{
			Version:     "1",
			Type:        "model",
			Repo:        "owner/repo",
			Branch:      "main",
			Commit:      "abc1234567890",
			TotalFiles:  5,
			TotalSize:   1024 * 1024,
			CompletedAt: time.Now(),
		}

		data, _ := yaml.Marshal(manifest)
		os.WriteFile(filepath.Join(modelDir, hfdownloader.ManifestFilename), data, 0o644)

		entries, err := scanManifests(tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}

		if entries[0].Repo != "owner/repo" {
			t.Errorf("Repo = %q", entries[0].Repo)
		}
		if entries[0].Type != "model" {
			t.Errorf("Type = %q", entries[0].Type)
		}
		if entries[0].Files != 5 {
			t.Errorf("Files = %d", entries[0].Files)
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create model manifest
		modelDir := filepath.Join(tmpDir, "models", "owner", "model")
		os.MkdirAll(modelDir, 0o755)
		modelManifest := hfdownloader.DownloadManifest{
			Type: "model",
			Repo: "owner/model",
		}
		data, _ := yaml.Marshal(modelManifest)
		os.WriteFile(filepath.Join(modelDir, hfdownloader.ManifestFilename), data, 0o644)

		// Create dataset manifest
		datasetDir := filepath.Join(tmpDir, "datasets", "owner", "dataset")
		os.MkdirAll(datasetDir, 0o755)
		datasetManifest := hfdownloader.DownloadManifest{
			Type: "dataset",
			Repo: "owner/dataset",
		}
		data, _ = yaml.Marshal(datasetManifest)
		os.WriteFile(filepath.Join(datasetDir, hfdownloader.ManifestFilename), data, 0o644)

		// Filter for datasets only
		entries, err := scanManifests(tmpDir, "dataset")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1 (only datasets)", len(entries))
		}
		if entries[0].Type != "dataset" {
			t.Errorf("Type = %q, want dataset", entries[0].Type)
		}
	})
}

func TestNewListCmd(t *testing.T) {
	t.Run("command creation", func(t *testing.T) {
		ro := &RootOpts{}
		cmd := newListCmd(ro)
		if cmd == nil {
			t.Fatal("cmd should not be nil")
		}
		if cmd.Use != "list" {
			t.Errorf("Use = %q, want list", cmd.Use)
		}
	})

	t.Run("flags exist", func(t *testing.T) {
		ro := &RootOpts{}
		cmd := newListCmd(ro)

		flags := []string{"cache-dir", "type", "sort", "format", "scan"}
		for _, name := range flags {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag %q should exist", name)
			}
		}
	})
}
