// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// getFreePort finds an available port
func getFreePort() int {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// These tests require network access and actually download from HuggingFace.
// Run with: go test -tags=integration -v ./internal/server/

func TestIntegration_FullDownloadFlow(t *testing.T) {
	port := getFreePort()
	cfg := Config{
		Addr:        "127.0.0.1",
		Port:        port,
		ModelsDir:   t.TempDir(),
		DatasetsDir: t.TempDir(),
		Concurrency: 4,
		MaxActive:   2,
	}

	srv := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	go srv.ListenAndServe(ctx)
	time.Sleep(200 * time.Millisecond)

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)

	t.Run("health check", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/health")
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("start download and track progress", func(t *testing.T) {
		// Start a tiny model download
		body := `{"repo": "hf-internal-testing/tiny-random-gpt2"}`
		resp, err := http.Post(
			baseURL+"/api/download",
			"application/json",
			bytes.NewBufferString(body),
		)
		if err != nil {
			t.Fatalf("Start download failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 202 {
			t.Fatalf("Expected 202, got %d", resp.StatusCode)
		}

		var job Job
		json.NewDecoder(resp.Body).Decode(&job)

		if job.ID == "" {
			t.Error("Job ID should not be empty")
		}

		// Poll for completion
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				t.Fatal("Download timed out")
			case <-ticker.C:
				jobResp, _ := http.Get(baseURL + "/api/jobs/" + job.ID)
				var current Job
				json.NewDecoder(jobResp.Body).Decode(&current)
				jobResp.Body.Close()

				t.Logf("Job status: %s, progress: %d/%d files",
					current.Status, current.Progress.CompletedFiles, current.Progress.TotalFiles)

				if current.Status == JobStatusCompleted {
					t.Log("Download completed successfully!")
					return
				}
				if current.Status == JobStatusFailed {
					t.Fatalf("Download failed: %s", current.Error)
				}
			}
		}
	})
}

func TestIntegration_DryRun(t *testing.T) {
	port := getFreePort()
	cfg := Config{
		Addr:        "127.0.0.1",
		Port:        port,
		ModelsDir:   t.TempDir(),
		DatasetsDir: t.TempDir(),
	}

	srv := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.ListenAndServe(ctx)
	time.Sleep(200 * time.Millisecond)

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)

	body := `{"repo": "hf-internal-testing/tiny-random-gpt2"}`
	resp, err := http.Post(
		baseURL+"/api/plan",
		"application/json",
		bytes.NewBufferString(body),
	)
	if err != nil {
		t.Fatalf("Plan request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var plan PlanResponse
	json.NewDecoder(resp.Body).Decode(&plan)

	if plan.TotalFiles == 0 {
		t.Error("Expected files in plan")
	}
	t.Logf("Plan: %d files, %d bytes", plan.TotalFiles, plan.TotalSize)

	for _, f := range plan.Files {
		t.Logf("  %s (%d bytes, LFS=%v)", f.Path, f.Size, f.LFS)
	}
}

