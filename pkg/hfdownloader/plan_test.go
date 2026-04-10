// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// buildMockTreeServer returns a mock HuggingFace API server that responds to
// the repo-info and tree endpoints with the supplied file list.
// All files are returned as non-LFS blobs with a deterministic SHA256
// derived from their path.
func buildMockTreeServer(t *testing.T, owner, name, commit string, files []string) *httptest.Server {
	t.Helper()

	repoID := owner + "/" + name

	// Build tree response nodes
	nodes := make([]hfNode, 0, len(files))
	for _, path := range files {
		nodes = append(nodes, hfNode{
			Type:   "file",
			Path:   path,
			Size:   100,
			Sha256: "sha256of_" + path,
		})
	}
	treeJSON, _ := json.Marshal(nodes)

	// Repo info response
	repoInfoJSON, _ := json.Marshal(RepoInfo{SHA: commit})

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/models/"+repoID+"/revision/main":
			w.Write(repoInfoJSON)
		case r.URL.Path == "/api/models/"+repoID+"/tree/main":
			w.Write(treeJSON)
		default:
			t.Logf("unexpected mock request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("{}"))
		}
	}))
}

// planPaths returns the sorted list of RelativePaths from a Plan.
func planPaths(p *Plan) []string {
	paths := make([]string, len(p.Items))
	for i, item := range p.Items {
		paths[i] = item.RelativePath
	}
	sort.Strings(paths)
	return paths
}

// TestScanRepo_ExactPaths verifies that when Job.Paths is set, scanRepo
// returns only the named files and ignores Filters/Excludes.
func TestScanRepo_ExactPaths(t *testing.T) {
	const owner = "owner"
	const name = "model"
	const commit = "abc123commit"

	allFiles := []string{
		"config.json",
		"tokenizer.json",
		"model.Q4_K_M.gguf",
		"model.Q5_K_M.gguf",
		"model.fp16.safetensors",
	}

	srv := buildMockTreeServer(t, owner, name, commit, allFiles)
	defer srv.Close()

	job := Job{
		Repo:     owner + "/" + name,
		Revision: "main",
	}
	cfg := Settings{
		Endpoint: srv.URL,
	}
	httpc := srv.Client()

	t.Run("no_paths_returns_all_files", func(t *testing.T) {
		plan, err := scanRepo(context.Background(), httpc, "", job, cfg)
		if err != nil {
			t.Fatalf("scanRepo: %v", err)
		}
		got := planPaths(plan)
		if len(got) != len(allFiles) {
			t.Errorf("expected %d files, got %d: %v", len(allFiles), len(got), got)
		}
		if plan.Commit != commit {
			t.Errorf("commit = %q, want %q", plan.Commit, commit)
		}
	})

	t.Run("exact_paths_subset", func(t *testing.T) {
		want := []string{"config.json", "model.Q4_K_M.gguf"}
		j := job
		j.Paths = want

		plan, err := scanRepo(context.Background(), httpc, "", j, cfg)
		if err != nil {
			t.Fatalf("scanRepo: %v", err)
		}
		got := planPaths(plan)
		sort.Strings(want)
		if len(got) != len(want) {
			t.Errorf("expected %d files, got %d: %v", len(want), len(got), got)
		}
		for i, p := range got {
			if p != want[i] {
				t.Errorf("file[%d] = %q, want %q", i, p, want[i])
			}
		}
	})

	t.Run("exact_paths_ignores_filters", func(t *testing.T) {
		// Even though a filter is set, Paths takes priority
		j := job
		j.Paths = []string{"tokenizer.json"}
		j.Filters = []string{"gguf"} // would normally match GGUF files

		plan, err := scanRepo(context.Background(), httpc, "", j, cfg)
		if err != nil {
			t.Fatalf("scanRepo: %v", err)
		}
		got := planPaths(plan)
		if len(got) != 1 || got[0] != "tokenizer.json" {
			t.Errorf("expected [tokenizer.json], got %v", got)
		}
	})

	t.Run("exact_paths_ignores_excludes", func(t *testing.T) {
		j := job
		j.Paths = []string{"config.json", "model.Q4_K_M.gguf"}
		j.Excludes = []string{"config"} // would normally exclude config.json

		plan, err := scanRepo(context.Background(), httpc, "", j, cfg)
		if err != nil {
			t.Fatalf("scanRepo: %v", err)
		}
		got := planPaths(plan)
		want := []string{"config.json", "model.Q4_K_M.gguf"}
		sort.Strings(want)
		if len(got) != len(want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("exact_paths_empty_when_no_match", func(t *testing.T) {
		j := job
		j.Paths = []string{"does_not_exist.bin"}

		plan, err := scanRepo(context.Background(), httpc, "", j, cfg)
		if err != nil {
			t.Fatalf("scanRepo: %v", err)
		}
		if len(plan.Items) != 0 {
			t.Errorf("expected 0 items, got %d", len(plan.Items))
		}
	})

	t.Run("exact_paths_single_file", func(t *testing.T) {
		j := job
		j.Paths = []string{"model.fp16.safetensors"}

		plan, err := scanRepo(context.Background(), httpc, "", j, cfg)
		if err != nil {
			t.Fatalf("scanRepo: %v", err)
		}
		if len(plan.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(plan.Items))
		}
		if plan.Items[0].RelativePath != "model.fp16.safetensors" {
			t.Errorf("path = %q, want model.fp16.safetensors", plan.Items[0].RelativePath)
		}
		// SHA256 should be populated from the mock response
		if plan.Items[0].SHA256 == "" {
			t.Error("expected SHA256 to be set")
		}
	})
}

// TestFetchFileTree_ReturnsAllFiles checks that FetchFileTree returns an entry
// for every file in the repo and correctly propagates the commit SHA.
func TestFetchFileTree_ReturnsAllFiles(t *testing.T) {
	allFiles := []string{"config.json", "model.gguf", "tokenizer.json"}
	const commit = "deadbeef01"

	srv := buildMockTreeServer(t, "owner", "model", commit, allFiles)
	defer srv.Close()

	job := Job{Repo: "owner/model", Revision: "main"}
	cfg := Settings{Endpoint: srv.URL}

	files, gotCommit, err := FetchFileTree(context.Background(), job, cfg)
	if err != nil {
		t.Fatalf("FetchFileTree: %v", err)
	}
	if gotCommit != commit {
		t.Errorf("commit = %q, want %q", gotCommit, commit)
	}
	if len(files) != len(allFiles) {
		t.Errorf("expected %d files, got %d", len(allFiles), len(files))
	}
	// Verify SHA256 field is populated for every entry
	for _, f := range files {
		if f.SHA256 == "" {
			t.Errorf("file %q has empty SHA256", f.Path)
		}
	}
}
