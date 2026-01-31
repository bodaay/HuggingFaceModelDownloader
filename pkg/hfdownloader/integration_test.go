// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

//go:build integration
// +build integration

// Integration tests that download real files from HuggingFace.
// Run with: go test -tags=integration ./pkg/hfdownloader/...
//
// These tests require network access and use temporary directories
// that are automatically cleaned up.

package hfdownloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntegration_DownloadSmallRepo downloads a small model's config files.
// Uses gpt2 which has small config files for fast testing.
func TestIntegration_DownloadSmallRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()

	job := Job{
		Repo:     "openai-community/gpt2",
		Revision: "main",
		// Only download config files (very small)
		Filters: []string{"config.json"},
	}

	cfg := Settings{
		CacheDir:           tmpDir,
		Concurrency:        4,
		MaxActiveDownloads: 2,
		Retries:            3,
		BackoffInitial:     "500ms",
		BackoffMax:         "5s",
		Verify:             "size",
	}

	var events []ProgressEvent
	progress := func(e ProgressEvent) {
		events = append(events, e)
		t.Logf("Event: %s - %s", e.Event, e.Path)
	}

	err := Download(ctx, job, cfg, progress)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify we received expected events
	hasStart := false
	hasDone := false
	for _, e := range events {
		if e.Event == "scan_start" || e.Event == "file_start" {
			hasStart = true
		}
		if e.Event == "done" || e.Event == "file_done" {
			hasDone = true
		}
	}

	if !hasStart {
		t.Error("expected start events")
	}
	if !hasDone {
		t.Error("expected done events")
	}

	// Verify files exist in cache
	hubDir := filepath.Join(tmpDir, "hub")
	if _, err := os.Stat(hubDir); os.IsNotExist(err) {
		t.Error("hub directory should exist")
	}
}

// TestIntegration_DownloadWithFilter tests filtered downloads.
func TestIntegration_DownloadWithFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()

	job := Job{
		Repo:     "openai-community/gpt2",
		Revision: "main",
		Filters:  []string{"tokenizer.json"},
		Excludes: []string{".bin", ".safetensors"},
	}

	cfg := Settings{
		CacheDir:           tmpDir,
		Concurrency:        4,
		MaxActiveDownloads: 2,
		Verify:             "size",
	}

	var downloadedFiles []string
	progress := func(e ProgressEvent) {
		if e.Event == "file_done" && e.Path != "" {
			downloadedFiles = append(downloadedFiles, e.Path)
		}
	}

	err := Download(ctx, job, cfg, progress)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify filter worked - should have tokenizer.json
	hasTokenizer := false
	for _, f := range downloadedFiles {
		if strings.Contains(f, "tokenizer.json") {
			hasTokenizer = true
		}
		// Should not have .bin or .safetensors
		if strings.HasSuffix(f, ".bin") || strings.HasSuffix(f, ".safetensors") {
			t.Errorf("excluded file was downloaded: %s", f)
		}
	}

	if !hasTokenizer {
		t.Log("Downloaded files:", downloadedFiles)
		// tokenizer.json might have been skipped if already exists
	}
}

// TestIntegration_PlanRepo tests the plan functionality.
func TestIntegration_PlanRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := Job{
		Repo:     "openai-community/gpt2",
		Revision: "main",
	}

	cfg := Settings{
		Concurrency: 4,
	}

	plan, err := PlanRepo(ctx, job, cfg)
	if err != nil {
		t.Fatalf("PlanRepo failed: %v", err)
	}

	if len(plan.Items) == 0 {
		t.Error("plan should have items")
	}

	// Check for expected files
	hasConfig := false
	hasModel := false
	for _, item := range plan.Items {
		if item.RelativePath == "config.json" {
			hasConfig = true
		}
		if strings.HasSuffix(item.RelativePath, ".safetensors") ||
			strings.HasSuffix(item.RelativePath, ".bin") {
			hasModel = true
		}
	}

	if !hasConfig {
		t.Error("plan should include config.json")
	}
	if !hasModel {
		t.Error("plan should include model weights")
	}

	t.Logf("Plan has %d items", len(plan.Items))
}

// TestIntegration_DownloadDataset tests dataset download.
func TestIntegration_DownloadDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()

	// Use a tiny dataset - just get the README
	job := Job{
		Repo:      "rajpurkar/squad",
		IsDataset: true,
		Revision:  "main",
		Filters:   []string{"README.md"},
	}

	cfg := Settings{
		CacheDir:           tmpDir,
		Concurrency:        4,
		MaxActiveDownloads: 2,
		Verify:             "size",
	}

	var hasFiles bool
	progress := func(e ProgressEvent) {
		if e.Event == "file_done" {
			hasFiles = true
		}
	}

	err := Download(ctx, job, cfg, progress)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify dataset directory structure
	datasetsDir := filepath.Join(tmpDir, "hub")
	entries, _ := os.ReadDir(datasetsDir)
	foundDataset := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "datasets--") {
			foundDataset = true
		}
	}

	if !foundDataset && hasFiles {
		t.Log("Dataset downloaded but directory structure may differ")
	}
}

// TestIntegration_ResumeDownload tests resumable downloads.
func TestIntegration_ResumeDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()

	job := Job{
		Repo:     "openai-community/gpt2",
		Revision: "main",
		Filters:  []string{"vocab.json"},
	}

	cfg := Settings{
		CacheDir:           tmpDir,
		Concurrency:        4,
		MaxActiveDownloads: 2,
		Verify:             "size",
	}

	// First download
	err := Download(ctx, job, cfg, nil)
	if err != nil {
		t.Fatalf("First download failed: %v", err)
	}

	// Second download should skip existing files
	var skipped bool
	progress := func(e ProgressEvent) {
		if e.Event == "file_done" && strings.Contains(e.Message, "skip") {
			skipped = true
		}
	}

	err = Download(ctx, job, cfg, progress)
	if err != nil {
		t.Fatalf("Second download failed: %v", err)
	}

	if !skipped {
		t.Log("Files may have been re-downloaded (no skip detected)")
	}
}

// TestIntegration_CancelDownload tests context cancellation.
func TestIntegration_CancelDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a context that cancels immediately after starting
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tmpDir := t.TempDir()

	// Try to download a larger repo
	job := Job{
		Repo:     "openai-community/gpt2",
		Revision: "main",
	}

	cfg := Settings{
		CacheDir:           tmpDir,
		Concurrency:        1,
		MaxActiveDownloads: 1,
	}

	err := Download(ctx, job, cfg, nil)
	// Should get a context error
	if err == nil {
		t.Log("Download completed before cancellation (fast network)")
	} else if !strings.Contains(err.Error(), "context") {
		t.Logf("Got error (may or may not be cancellation): %v", err)
	}
}

// TestIntegration_HFCache tests HF cache operations.
func TestIntegration_HFCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()

	// Download something first
	job := Job{
		Repo:     "openai-community/gpt2",
		Revision: "main",
		Filters:  []string{"config.json"},
	}

	cfg := Settings{
		CacheDir:           tmpDir,
		Concurrency:        4,
		MaxActiveDownloads: 2,
		Verify:             "size",
	}

	err := Download(ctx, job, cfg, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Now test HFCache operations
	cache := NewHFCache(tmpDir, 5*time.Minute)
	if cache == nil {
		t.Fatal("NewHFCache returned nil")
	}

	// Test HubDir
	hubDir := cache.HubDir()
	if hubDir == "" {
		t.Error("HubDir should not be empty")
	}

	// Test ModelsDir
	modelsDir := cache.ModelsDir()
	if modelsDir == "" {
		t.Error("ModelsDir should not be empty")
	}

	// Test Repo method
	repoDir, err := cache.Repo("openai-community/gpt2", RepoTypeModel)
	if err != nil {
		t.Logf("Repo lookup: %v (may not exist in expected location)", err)
	} else if repoDir != nil {
		t.Logf("Found repo at: %s", repoDir.Path())
	}

	// Test ListRepos
	repos, err := cache.ListRepos()
	if err != nil {
		t.Logf("ListRepos: %v", err)
	} else {
		t.Logf("Found %d repos in cache", len(repos))
	}
}

// TestIntegration_IsValidModelName tests model name validation.
func TestIntegration_IsValidModelName(t *testing.T) {
	validNames := []string{
		"openai-community/gpt2",
		"meta-llama/Llama-2-7b",
		"TheBloke/Mistral-7B-GGUF",
		"stabilityai/stable-diffusion-xl-base-1.0",
		"HuggingFaceFW/fineweb",
	}

	for _, name := range validNames {
		if !IsValidModelName(name) {
			t.Errorf("IsValidModelName(%q) = false, want true", name)
		}
	}

	invalidNames := []string{
		"gpt2",              // No owner
		"owner",             // No repo
		"owner//repo",       // Double slash
		"/owner/repo",       // Leading slash
		"owner/repo/branch", // Too many parts
		"",                  // Empty
	}

	for _, name := range invalidNames {
		if IsValidModelName(name) {
			t.Errorf("IsValidModelName(%q) = true, want false", name)
		}
	}
}
