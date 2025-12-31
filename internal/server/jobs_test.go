// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"
)

func TestJobManager_CreateJob(t *testing.T) {
	cfg := Config{
		ModelsDir:   "./test_models",
		DatasetsDir: "./test_datasets",
		Concurrency: 2,
		MaxActive:   1,
	}
	hub := NewWSHub()
	go hub.Run()
	
	mgr := NewJobManager(cfg, hub)

	t.Run("creates model job with server-controlled output", func(t *testing.T) {
		req := DownloadRequest{
			Repo:     "test/model",
			Revision: "main",
			Dataset:  false,
		}

		job, wasExisting, err := mgr.CreateJob(req)
		if err != nil {
			t.Fatalf("CreateJob failed: %v", err)
		}
		if wasExisting {
			t.Error("Expected new job, got existing")
		}
		if job.OutputDir != "./test_models" {
			t.Errorf("Expected output ./test_models, got %s", job.OutputDir)
		}
		if job.IsDataset {
			t.Error("Expected model, got dataset")
		}
	})

	t.Run("creates dataset job with server-controlled output", func(t *testing.T) {
		req := DownloadRequest{
			Repo:    "test/dataset",
			Dataset: true,
		}

		job, _, err := mgr.CreateJob(req)
		if err != nil {
			t.Fatalf("CreateJob failed: %v", err)
		}
		if job.OutputDir != "./test_datasets" {
			t.Errorf("Expected output ./test_datasets, got %s", job.OutputDir)
		}
		if !job.IsDataset {
			t.Error("Expected dataset, got model")
		}
	})

	t.Run("defaults revision to main", func(t *testing.T) {
		req := DownloadRequest{
			Repo: "test/no-revision",
		}

		job, _, _ := mgr.CreateJob(req)
		if job.Revision != "main" {
			t.Errorf("Expected revision main, got %s", job.Revision)
		}
	})
}

func TestJobManager_Deduplication(t *testing.T) {
	cfg := Config{
		ModelsDir: "./test_models",
	}
	hub := NewWSHub()
	go hub.Run()
	
	mgr := NewJobManager(cfg, hub)

	// Create first job
	req := DownloadRequest{
		Repo:     "dedup/test",
		Revision: "main",
	}

	job1, wasExisting1, _ := mgr.CreateJob(req)
	if wasExisting1 {
		t.Error("First job should not be existing")
	}

	// Try to create same job again
	job2, wasExisting2, _ := mgr.CreateJob(req)
	if !wasExisting2 {
		t.Error("Second job should be detected as existing")
	}
	if job1.ID != job2.ID {
		t.Errorf("Expected same job ID, got %s vs %s", job1.ID, job2.ID)
	}
}

func TestJobManager_DifferentRevisionsNotDeduplicated(t *testing.T) {
	cfg := Config{
		ModelsDir: "./test_models",
	}
	hub := NewWSHub()
	go hub.Run()
	
	mgr := NewJobManager(cfg, hub)

	job1, _, _ := mgr.CreateJob(DownloadRequest{
		Repo:     "revision/test",
		Revision: "v1",
	})

	job2, wasExisting, _ := mgr.CreateJob(DownloadRequest{
		Repo:     "revision/test",
		Revision: "v2",
	})

	if wasExisting {
		t.Error("Different revisions should create different jobs")
	}
	if job1.ID == job2.ID {
		t.Error("Different revisions should have different IDs")
	}
}

func TestJobManager_ModelVsDatasetNotDeduplicated(t *testing.T) {
	cfg := Config{
		ModelsDir:   "./test_models",
		DatasetsDir: "./test_datasets",
	}
	hub := NewWSHub()
	go hub.Run()
	
	mgr := NewJobManager(cfg, hub)

	job1, _, _ := mgr.CreateJob(DownloadRequest{
		Repo:    "type/test",
		Dataset: false,
	})

	job2, wasExisting, _ := mgr.CreateJob(DownloadRequest{
		Repo:    "type/test",
		Dataset: true,
	})

	if wasExisting {
		t.Error("Model and dataset with same repo should be different jobs")
	}
	if job1.ID == job2.ID {
		t.Error("Model and dataset should have different IDs")
	}
}

func TestJobManager_GetJob(t *testing.T) {
	cfg := Config{ModelsDir: "./test"}
	hub := NewWSHub()
	go hub.Run()
	mgr := NewJobManager(cfg, hub)

	job, _, _ := mgr.CreateJob(DownloadRequest{Repo: "get/test"})

	t.Run("returns existing job", func(t *testing.T) {
		found, ok := mgr.GetJob(job.ID)
		if !ok {
			t.Error("Expected to find job")
		}
		if found.ID != job.ID {
			t.Error("Wrong job returned")
		}
	})

	t.Run("returns false for missing job", func(t *testing.T) {
		_, ok := mgr.GetJob("nonexistent")
		if ok {
			t.Error("Should not find nonexistent job")
		}
	})
}

func TestJobManager_ListJobs(t *testing.T) {
	cfg := Config{ModelsDir: "./test"}
	hub := NewWSHub()
	go hub.Run()
	mgr := NewJobManager(cfg, hub)

	// Create multiple jobs with unique repos
	mgr.CreateJob(DownloadRequest{Repo: "list/test1"})
	mgr.CreateJob(DownloadRequest{Repo: "list/test2"})
	mgr.CreateJob(DownloadRequest{Repo: "list/test3"})

	jobs := mgr.ListJobs()
	if len(jobs) < 3 {
		t.Errorf("Expected at least 3 jobs, got %d", len(jobs))
	}
}

func TestJobManager_CancelJob(t *testing.T) {
	cfg := Config{ModelsDir: "./test"}
	hub := NewWSHub()
	go hub.Run()
	mgr := NewJobManager(cfg, hub)

	job, _, _ := mgr.CreateJob(DownloadRequest{Repo: "cancel/test"})

	// Wait a bit for job to start
	time.Sleep(50 * time.Millisecond)

	t.Run("cancels running job", func(t *testing.T) {
		ok := mgr.CancelJob(job.ID)
		if !ok {
			t.Error("Cancel should succeed")
		}

		found, _ := mgr.GetJob(job.ID)
		if found.Status != JobStatusCancelled {
			t.Errorf("Expected cancelled status, got %s", found.Status)
		}
	})

	t.Run("returns false for nonexistent job", func(t *testing.T) {
		ok := mgr.CancelJob("nonexistent")
		if ok {
			t.Error("Cancel should fail for nonexistent job")
		}
	})
}

func TestJobStatus_Values(t *testing.T) {
	statuses := []JobStatus{
		JobStatusQueued,
		JobStatusRunning,
		JobStatusCompleted,
		JobStatusFailed,
		JobStatusCancelled,
	}

	for _, s := range statuses {
		if s == "" {
			t.Error("Status should not be empty")
		}
	}
}

