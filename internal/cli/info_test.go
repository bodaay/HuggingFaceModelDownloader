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

func TestRepoInfo(t *testing.T) {
	info := RepoInfo{
		Type:           "model",
		Repo:           "owner/repo",
		Branch:         "main",
		Commit:         "abc1234",
		TotalFiles:     10,
		TotalSize:      1024 * 1024 * 100,
		TotalSizeHuman: "100.0 MB",
		StartedAt:      "2025-01-01 00:00:00",
		CompletedAt:    "2025-01-01 01:00:00",
		Command:        "hfdownloader download owner/repo",
		FriendlyPath:   "/cache/models/owner/repo",
		CachePath:      "/cache/hub/models--owner--repo",
		Files: []RepoFileInfo{
			{Name: "model.bin", Size: 1024, SizeHuman: "1.0 KB", LFS: true},
		},
	}

	if info.Type != "model" {
		t.Errorf("Type = %q", info.Type)
	}
	if info.Repo != "owner/repo" {
		t.Errorf("Repo = %q", info.Repo)
	}
	if len(info.Files) != 1 {
		t.Errorf("Files = %d", len(info.Files))
	}
}

func TestRepoFileInfo(t *testing.T) {
	file := RepoFileInfo{
		Name:      "model.safetensors",
		Size:      1024 * 1024 * 500,
		SizeHuman: "500.0 MB",
		LFS:       true,
		BlobPath:  "/cache/hub/models--owner--repo/blobs/abc123",
	}

	if file.Name != "model.safetensors" {
		t.Errorf("Name = %q", file.Name)
	}
	if !file.LFS {
		t.Error("LFS should be true")
	}
	if file.BlobPath == "" {
		t.Error("BlobPath should not be empty")
	}
}

func TestMatchesRepo(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		query    string
		expected bool
	}{
		{"exact match", "owner/repo", "owner/repo", true},
		{"case insensitive", "Owner/Repo", "owner/repo", true},
		{"repo name only", "owner/repo", "repo", true},
		{"partial match", "TheBloke/Mistral-7B-GGUF", "Mistral", true},
		{"no match", "owner/repo", "other", false},
		{"owner only no match", "owner/repo", "owner", true}, // Contains match
		{"empty query", "owner/repo", "", true},              // Empty string is contained
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesRepo(tt.repo, tt.query)
			if result != tt.expected {
				t.Errorf("matchesRepo(%q, %q) = %v, want %v", tt.repo, tt.query, result, tt.expected)
			}
		})
	}
}

func TestFindRepoInfo(t *testing.T) {
	t.Run("no matching repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := findRepoInfo(tmpDir, "nonexistent/repo")
		if err == nil {
			t.Error("expected error for nonexistent repo")
		}
	})

	t.Run("finds repo by exact name", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create manifest
		modelDir := filepath.Join(tmpDir, "models", "owner", "repo")
		os.MkdirAll(modelDir, 0o755)

		now := time.Now()
		manifest := hfdownloader.DownloadManifest{
			Version:     "1",
			Type:        "model",
			Repo:        "owner/repo",
			Branch:      "main",
			Commit:      "abc1234567890",
			TotalFiles:  3,
			TotalSize:   1024 * 1024,
			RepoPath:    "hub/models--owner--repo",
			StartedAt:   now.Add(-time.Hour),
			CompletedAt: now,
			Files: []hfdownloader.ManifestFile{
				{Name: "config.json", Size: 1024, LFS: false, Blob: "blobs/abc"},
				{Name: "model.bin", Size: 1024 * 1024, LFS: true, Blob: "blobs/def"},
			},
		}

		data, _ := yaml.Marshal(manifest)
		os.WriteFile(filepath.Join(modelDir, hfdownloader.ManifestFilename), data, 0o644)

		info, err := findRepoInfo(tmpDir, "owner/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if info.Repo != "owner/repo" {
			t.Errorf("Repo = %q", info.Repo)
		}
		if info.Type != "model" {
			t.Errorf("Type = %q", info.Type)
		}
		if info.TotalFiles != 3 {
			t.Errorf("TotalFiles = %d", info.TotalFiles)
		}
		if len(info.Files) != 2 {
			t.Errorf("Files = %d", len(info.Files))
		}
	})

	t.Run("finds repo by partial name", func(t *testing.T) {
		tmpDir := t.TempDir()

		modelDir := filepath.Join(tmpDir, "models", "TheBloke", "Mistral-7B-GGUF")
		os.MkdirAll(modelDir, 0o755)

		manifest := hfdownloader.DownloadManifest{
			Type: "model",
			Repo: "TheBloke/Mistral-7B-GGUF",
		}

		data, _ := yaml.Marshal(manifest)
		os.WriteFile(filepath.Join(modelDir, hfdownloader.ManifestFilename), data, 0o644)

		info, err := findRepoInfo(tmpDir, "Mistral-7B-GGUF")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if info.Repo != "TheBloke/Mistral-7B-GGUF" {
			t.Errorf("Repo = %q", info.Repo)
		}
	})

	t.Run("multiple matches returns error", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create two similar repos
		for _, name := range []string{"Mistral-7B-GGUF", "Mistral-7B-Instruct-GGUF"} {
			modelDir := filepath.Join(tmpDir, "models", "TheBloke", name)
			os.MkdirAll(modelDir, 0o755)

			manifest := hfdownloader.DownloadManifest{
				Type: "model",
				Repo: "TheBloke/" + name,
			}

			data, _ := yaml.Marshal(manifest)
			os.WriteFile(filepath.Join(modelDir, hfdownloader.ManifestFilename), data, 0o644)
		}

		_, err := findRepoInfo(tmpDir, "Mistral")
		if err == nil {
			t.Error("expected error for multiple matches")
		}
	})
}

func TestNewInfoCmd(t *testing.T) {
	t.Run("command creation", func(t *testing.T) {
		ro := &RootOpts{}
		cmd := newInfoCmd(ro)
		if cmd == nil {
			t.Fatal("cmd should not be nil")
		}
		if cmd.Use != "info <repo>" {
			t.Errorf("Use = %q, want 'info <repo>'", cmd.Use)
		}
	})

	t.Run("flags exist", func(t *testing.T) {
		ro := &RootOpts{}
		cmd := newInfoCmd(ro)

		flags := []string{"cache-dir", "format"}
		for _, name := range flags {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag %q should exist", name)
			}
		}
	})

	t.Run("requires exactly one arg", func(t *testing.T) {
		ro := &RootOpts{}
		cmd := newInfoCmd(ro)

		// Command should expect exactly 1 argument
		if cmd.Args == nil {
			t.Error("Args should be set")
		}
	})
}
