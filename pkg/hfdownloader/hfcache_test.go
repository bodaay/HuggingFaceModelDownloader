// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultCacheDir(t *testing.T) {
	t.Run("with HF_HOME set", func(t *testing.T) {
		origHFHome := os.Getenv("HF_HOME")
		defer os.Setenv("HF_HOME", origHFHome)

		os.Setenv("HF_HOME", "/custom/hf/path")
		dir := DefaultCacheDir()
		if dir != "/custom/hf/path" {
			t.Errorf("DefaultCacheDir() = %q, want /custom/hf/path", dir)
		}
	})

	t.Run("without HF_HOME", func(t *testing.T) {
		origHFHome := os.Getenv("HF_HOME")
		defer os.Setenv("HF_HOME", origHFHome)

		os.Unsetenv("HF_HOME")
		dir := DefaultCacheDir()
		if !strings.Contains(dir, ".cache") || !strings.Contains(dir, "huggingface") {
			t.Errorf("DefaultCacheDir() = %q, want to contain .cache/huggingface", dir)
		}
	})
}

func TestNewHFCache(t *testing.T) {
	t.Run("empty root uses default", func(t *testing.T) {
		cache := NewHFCache("", 0)
		if cache.Root == "" {
			t.Error("cache.Root should not be empty when passing empty string")
		}
	})

	t.Run("custom root", func(t *testing.T) {
		cache := NewHFCache("/my/cache", 0)
		if cache.Root != "/my/cache" {
			t.Errorf("cache.Root = %q, want /my/cache", cache.Root)
		}
	})

	t.Run("default stale timeout", func(t *testing.T) {
		cache := NewHFCache("/test", 0)
		if cache.StaleTimeout != DefaultStaleTimeout {
			t.Errorf("StaleTimeout = %v, want %v", cache.StaleTimeout, DefaultStaleTimeout)
		}
	})

	t.Run("custom stale timeout", func(t *testing.T) {
		cache := NewHFCache("/test", 10*time.Minute)
		if cache.StaleTimeout != 10*time.Minute {
			t.Errorf("StaleTimeout = %v, want 10m", cache.StaleTimeout)
		}
	})
}

func TestHFCache_Directories(t *testing.T) {
	t.Run("HubDir default", func(t *testing.T) {
		origHFHubCache := os.Getenv("HF_HUB_CACHE")
		defer os.Setenv("HF_HUB_CACHE", origHFHubCache)
		os.Unsetenv("HF_HUB_CACHE")

		cache := NewHFCache("/root", 0)
		if cache.HubDir() != "/root/hub" {
			t.Errorf("HubDir() = %q, want /root/hub", cache.HubDir())
		}
	})

	t.Run("HubDir with HF_HUB_CACHE", func(t *testing.T) {
		origHFHubCache := os.Getenv("HF_HUB_CACHE")
		defer os.Setenv("HF_HUB_CACHE", origHFHubCache)

		os.Setenv("HF_HUB_CACHE", "/custom/hub")
		cache := NewHFCache("/root", 0)
		if cache.HubDir() != "/custom/hub" {
			t.Errorf("HubDir() = %q, want /custom/hub", cache.HubDir())
		}
	})

	t.Run("ModelsDir", func(t *testing.T) {
		cache := NewHFCache("/root", 0)
		if cache.ModelsDir() != "/root/models" {
			t.Errorf("ModelsDir() = %q, want /root/models", cache.ModelsDir())
		}
	})

	t.Run("DatasetsDir", func(t *testing.T) {
		cache := NewHFCache("/root", 0)
		if cache.DatasetsDir() != "/root/datasets" {
			t.Errorf("DatasetsDir() = %q, want /root/datasets", cache.DatasetsDir())
		}
	})
}

func TestHFCache_Repo(t *testing.T) {
	cache := NewHFCache("/root", 0)

	t.Run("valid model repo", func(t *testing.T) {
		repo, err := cache.Repo("TheBloke/Mistral-7B-GGUF", RepoTypeModel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.RepoID() != "TheBloke/Mistral-7B-GGUF" {
			t.Errorf("RepoID() = %q, want TheBloke/Mistral-7B-GGUF", repo.RepoID())
		}
		if repo.Owner() != "TheBloke" {
			t.Errorf("Owner() = %q, want TheBloke", repo.Owner())
		}
		if repo.Name() != "Mistral-7B-GGUF" {
			t.Errorf("Name() = %q, want Mistral-7B-GGUF", repo.Name())
		}
		if repo.Type() != RepoTypeModel {
			t.Errorf("Type() = %v, want RepoTypeModel", repo.Type())
		}
	})

	t.Run("valid dataset repo", func(t *testing.T) {
		repo, err := cache.Repo("facebook/flores", RepoTypeDataset)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.Type() != RepoTypeDataset {
			t.Errorf("Type() = %v, want RepoTypeDataset", repo.Type())
		}
	})

	t.Run("invalid repo format", func(t *testing.T) {
		_, err := cache.Repo("invalid", RepoTypeModel)
		if err == nil {
			t.Error("expected error for invalid repo format")
		}
	})

	t.Run("empty repo", func(t *testing.T) {
		_, err := cache.Repo("", RepoTypeModel)
		if err == nil {
			t.Error("expected error for empty repo")
		}
	})
}

func TestRepoDir_Paths(t *testing.T) {
	origHFHubCache := os.Getenv("HF_HUB_CACHE")
	defer os.Setenv("HF_HUB_CACHE", origHFHubCache)
	os.Unsetenv("HF_HUB_CACHE")

	cache := NewHFCache("/root", 0)

	t.Run("model paths", func(t *testing.T) {
		repo, _ := cache.Repo("TheBloke/Mistral-7B", RepoTypeModel)

		if repo.Path() != "/root/hub/models--TheBloke--Mistral-7B" {
			t.Errorf("Path() = %q", repo.Path())
		}
		if repo.BlobsDir() != "/root/hub/models--TheBloke--Mistral-7B/blobs" {
			t.Errorf("BlobsDir() = %q", repo.BlobsDir())
		}
		if repo.RefsDir() != "/root/hub/models--TheBloke--Mistral-7B/refs" {
			t.Errorf("RefsDir() = %q", repo.RefsDir())
		}
		if repo.SnapshotsDir() != "/root/hub/models--TheBloke--Mistral-7B/snapshots" {
			t.Errorf("SnapshotsDir() = %q", repo.SnapshotsDir())
		}
		if repo.FriendlyPath() != "/root/models/TheBloke/Mistral-7B" {
			t.Errorf("FriendlyPath() = %q", repo.FriendlyPath())
		}
	})

	t.Run("dataset paths", func(t *testing.T) {
		repo, _ := cache.Repo("facebook/flores", RepoTypeDataset)

		if repo.Path() != "/root/hub/datasets--facebook--flores" {
			t.Errorf("Path() = %q", repo.Path())
		}
		if repo.FriendlyPath() != "/root/datasets/facebook/flores" {
			t.Errorf("FriendlyPath() = %q", repo.FriendlyPath())
		}
	})

	t.Run("blob paths", func(t *testing.T) {
		repo, _ := cache.Repo("owner/name", RepoTypeModel)
		sha := "abc123def456"

		if repo.BlobPath(sha) != "/root/hub/models--owner--name/blobs/abc123def456" {
			t.Errorf("BlobPath() = %q", repo.BlobPath(sha))
		}
		if repo.IncompletePath(sha) != "/root/hub/models--owner--name/blobs/abc123def456.incomplete" {
			t.Errorf("IncompletePath() = %q", repo.IncompletePath(sha))
		}
		if repo.IncompleteMetaPath(sha) != "/root/hub/models--owner--name/blobs/abc123def456.incomplete.meta" {
			t.Errorf("IncompleteMetaPath() = %q", repo.IncompleteMetaPath(sha))
		}
	})

	t.Run("ref and snapshot paths", func(t *testing.T) {
		repo, _ := cache.Repo("owner/name", RepoTypeModel)

		if repo.RefPath("main") != "/root/hub/models--owner--name/refs/main" {
			t.Errorf("RefPath() = %q", repo.RefPath("main"))
		}
		if repo.SnapshotDir("commit123") != "/root/hub/models--owner--name/snapshots/commit123" {
			t.Errorf("SnapshotDir() = %q", repo.SnapshotDir("commit123"))
		}
		if repo.SnapshotPath("commit123", "config.json") != "/root/hub/models--owner--name/snapshots/commit123/config.json" {
			t.Errorf("SnapshotPath() = %q", repo.SnapshotPath("commit123", "config.json"))
		}
	})
}

func TestRepoDir_EnsureDirs(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)

	if err := repo.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error: %v", err)
	}

	// Verify directories exist
	dirs := []string{repo.BlobsDir(), repo.RefsDir(), repo.SnapshotsDir()}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}
}

func TestBlobStatus_String(t *testing.T) {
	tests := []struct {
		status   BlobStatus
		expected string
	}{
		{BlobMissing, "missing"},
		{BlobComplete, "complete"},
		{BlobDownloading, "downloading"},
		{BlobStale, "stale"},
		{BlobStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.status.String() != tt.expected {
				t.Errorf("BlobStatus(%d).String() = %q, want %q", tt.status, tt.status.String(), tt.expected)
			}
		})
	}
}

func TestRepoDir_CheckBlob(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 100*time.Millisecond) // Short stale timeout for testing
	repo, _ := cache.Repo("owner/name", RepoTypeModel)
	repo.EnsureDirs()

	sha := "abc123"

	t.Run("missing blob", func(t *testing.T) {
		status, meta, err := repo.CheckBlob(sha)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != BlobMissing {
			t.Errorf("status = %v, want BlobMissing", status)
		}
		if meta != nil {
			t.Error("meta should be nil for missing blob")
		}
	})

	t.Run("complete blob", func(t *testing.T) {
		// Create complete blob
		blobPath := repo.BlobPath(sha)
		if err := os.WriteFile(blobPath, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(blobPath)

		status, meta, err := repo.CheckBlob(sha)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != BlobComplete {
			t.Errorf("status = %v, want BlobComplete", status)
		}
		if meta != nil {
			t.Error("meta should be nil for complete blob")
		}
	})

	t.Run("stale incomplete", func(t *testing.T) {
		sha2 := "def456"
		// Create incomplete file (no metadata = stale)
		incompletePath := repo.IncompletePath(sha2)
		if err := os.WriteFile(incompletePath, []byte("partial"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(incompletePath)

		status, _, err := repo.CheckBlob(sha2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != BlobStale {
			t.Errorf("status = %v, want BlobStale", status)
		}
	})
}

func TestRepoDir_WriteAndReadRef(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)
	repo.EnsureDirs()

	t.Run("write and read ref", func(t *testing.T) {
		commit := "abc123def456"
		if err := repo.WriteRef("main", commit); err != nil {
			t.Fatalf("WriteRef error: %v", err)
		}

		read, err := repo.ReadRef("main")
		if err != nil {
			t.Fatalf("ReadRef error: %v", err)
		}
		if read != commit {
			t.Errorf("ReadRef() = %q, want %q", read, commit)
		}
	})

	t.Run("read nonexistent ref", func(t *testing.T) {
		read, err := repo.ReadRef("nonexistent")
		if err != nil {
			t.Fatalf("ReadRef error: %v", err)
		}
		if read != "" {
			t.Errorf("ReadRef() = %q, want empty string", read)
		}
	})
}

func TestRepoDir_WriteIncompleteMeta(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)
	repo.EnsureDirs()

	sha := "test123"
	size := int64(1024)

	if err := repo.WriteIncompleteMeta(sha, size); err != nil {
		t.Fatalf("WriteIncompleteMeta error: %v", err)
	}

	// Read and verify metadata
	metaPath := repo.IncompleteMetaPath(sha)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var meta IncompleteMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if meta.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", meta.PID, os.Getpid())
	}
	if meta.Size != size {
		t.Errorf("Size = %d, want %d", meta.Size, size)
	}
	if meta.SHA256 != sha {
		t.Errorf("SHA256 = %q, want %q", meta.SHA256, sha)
	}
}

func TestRepoDir_CleanupIncomplete(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)
	repo.EnsureDirs()

	sha := "cleanup123"

	// Create incomplete files
	incompletePath := repo.IncompletePath(sha)
	metaPath := repo.IncompleteMetaPath(sha)
	os.WriteFile(incompletePath, []byte("partial"), 0644)
	os.WriteFile(metaPath, []byte("{}"), 0644)

	if err := repo.CleanupIncomplete(sha); err != nil {
		t.Fatalf("CleanupIncomplete error: %v", err)
	}

	// Verify files are removed
	if _, err := os.Stat(incompletePath); !os.IsNotExist(err) {
		t.Error("incomplete file should be removed")
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("incomplete meta file should be removed")
	}
}

func TestRepoDir_FinalizeBlob(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)
	repo.EnsureDirs()

	sha := "final123"
	content := []byte("complete data")

	// Create incomplete file
	incompletePath := repo.IncompletePath(sha)
	metaPath := repo.IncompleteMetaPath(sha)
	os.WriteFile(incompletePath, content, 0644)
	os.WriteFile(metaPath, []byte("{}"), 0644)

	if err := repo.FinalizeBlob(sha); err != nil {
		t.Fatalf("FinalizeBlob error: %v", err)
	}

	// Verify blob exists
	blobPath := repo.BlobPath(sha)
	data, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("blob file not found: %v", err)
	}
	if string(data) != string(content) {
		t.Error("blob content mismatch")
	}

	// Verify incomplete is removed
	if _, err := os.Stat(incompletePath); !os.IsNotExist(err) {
		t.Error("incomplete file should be removed")
	}
}

func TestRepoDir_ListSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)
	repo.EnsureDirs()

	t.Run("empty snapshots", func(t *testing.T) {
		snapshots, err := repo.ListSnapshots()
		if err != nil {
			t.Fatalf("ListSnapshots error: %v", err)
		}
		if len(snapshots) != 0 {
			t.Errorf("expected empty snapshots, got %v", snapshots)
		}
	})

	t.Run("with snapshots", func(t *testing.T) {
		// Create some snapshot directories
		os.MkdirAll(repo.SnapshotDir("commit1"), 0755)
		os.MkdirAll(repo.SnapshotDir("commit2"), 0755)

		snapshots, err := repo.ListSnapshots()
		if err != nil {
			t.Fatalf("ListSnapshots error: %v", err)
		}
		if len(snapshots) != 2 {
			t.Errorf("expected 2 snapshots, got %d", len(snapshots))
		}
	})
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("test content for copy")

	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile error: %v", err)
	}

	// Verify content
	dstContent, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst error: %v", err)
	}
	if string(dstContent) != string(content) {
		t.Error("copied content mismatch")
	}
}

func TestIsProcessAlive(t *testing.T) {
	t.Run("current process", func(t *testing.T) {
		if !isProcessAlive(os.Getpid()) {
			t.Error("current process should be alive")
		}
	})

	t.Run("invalid PID", func(t *testing.T) {
		if isProcessAlive(0) {
			t.Error("PID 0 should not be alive")
		}
		if isProcessAlive(-1) {
			t.Error("PID -1 should not be alive")
		}
	})

	// Note: Testing non-existent PID is tricky because the behavior
	// varies by platform. We just verify it doesn't panic.
	t.Run("very large PID", func(t *testing.T) {
		// This should return false without panicking
		_ = isProcessAlive(99999999)
	})
}

func TestIsWindows(t *testing.T) {
	result := isWindows()
	expected := runtime.GOOS == "windows"
	if result != expected {
		t.Errorf("isWindows() = %v, want %v", result, expected)
	}
}

func TestRepoDir_EnsureFriendlyDir(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewHFCache(tmpDir, 0)
	repo, _ := cache.Repo("owner/name", RepoTypeModel)

	if err := repo.EnsureFriendlyDir(); err != nil {
		t.Fatalf("EnsureFriendlyDir error: %v", err)
	}

	friendlyPath := repo.FriendlyPath()
	if _, err := os.Stat(friendlyPath); os.IsNotExist(err) {
		t.Errorf("friendly directory %s was not created", friendlyPath)
	}
}
