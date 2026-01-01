// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// JobStatus represents the state of a download job.
type JobStatus string

const (
	JobStatusQueued     JobStatus = "queued"
	JobStatusRunning    JobStatus = "running"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
)

// Job represents a download job.
type Job struct {
	ID        string            `json:"id"`
	Repo      string            `json:"repo"`
	Revision  string            `json:"revision"`
	IsDataset bool              `json:"isDataset,omitempty"`
	Filters   []string          `json:"filters,omitempty"`
	Excludes  []string          `json:"excludes,omitempty"`
	OutputDir string            `json:"outputDir"`
	Status    JobStatus         `json:"status"`
	Progress  JobProgress       `json:"progress"`
	Error     string            `json:"error,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	StartedAt *time.Time        `json:"startedAt,omitempty"`
	EndedAt   *time.Time        `json:"endedAt,omitempty"`
	Files     []JobFileProgress `json:"files,omitempty"`

	cancel context.CancelFunc `json:"-"`
}

// JobProgress holds aggregate progress info.
type JobProgress struct {
	TotalFiles      int   `json:"totalFiles"`
	CompletedFiles  int   `json:"completedFiles"`
	TotalBytes      int64 `json:"totalBytes"`
	DownloadedBytes int64 `json:"downloadedBytes"`
	BytesPerSecond  int64 `json:"bytesPerSecond"`
}

// JobFileProgress holds per-file progress.
type JobFileProgress struct {
	Path       string `json:"path"`
	TotalBytes int64  `json:"totalBytes"`
	Downloaded int64  `json:"downloaded"`
	Status     string `json:"status"` // pending, active, complete, skipped, error
}

// JobManager manages download jobs.
type JobManager struct {
	mu         sync.RWMutex
	jobs       map[string]*Job
	config     Config
	listeners  []chan *Job
	listenerMu sync.RWMutex
	wsHub      *WSHub
}

// NewJobManager creates a new job manager.
func NewJobManager(cfg Config, wsHub *WSHub) *JobManager {
	return &JobManager{
		jobs:   make(map[string]*Job),
		config: cfg,
		wsHub:  wsHub,
	}
}

// generateID creates a short random ID.
func generateID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateJob creates a new download job.
// Returns existing job if same repo+revision+dataset is already in progress.
func (m *JobManager) CreateJob(req DownloadRequest) (*Job, bool, error) {
	revision := req.Revision
	if revision == "" {
		revision = "main"
	}

	// Determine output directory based on type (NOT from request for security)
	outputDir := m.config.ModelsDir
	if req.Dataset {
		outputDir = m.config.DatasetsDir
	}

	// Check for existing active job with same repo+revision+type
	m.mu.Lock()
	for _, existing := range m.jobs {
		if existing.Repo == req.Repo &&
			existing.Revision == revision &&
			existing.IsDataset == req.Dataset &&
			(existing.Status == JobStatusQueued || existing.Status == JobStatusRunning) {
			m.mu.Unlock()
			return existing, true, nil // Return existing, wasExisting=true
		}
	}

	job := &Job{
		ID:        generateID(),
		Repo:      req.Repo,
		Revision:  revision,
		IsDataset: req.Dataset,
		Filters:   req.Filters,
		Excludes:  req.Excludes,
		OutputDir: outputDir, // Server-controlled, not from request
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
		Progress:  JobProgress{},
	}

	m.jobs[job.ID] = job
	m.mu.Unlock()

	// Start the job
	go m.runJob(job)

	return job, false, nil // New job, wasExisting=false
}

// GetJob retrieves a job by ID.
func (m *JobManager) GetJob(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

// ListJobs returns all jobs.
func (m *JobManager) ListJobs() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// CancelJob cancels a running or queued job.
func (m *JobManager) CancelJob(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return false
	}

	if job.Status == JobStatusQueued || job.Status == JobStatusRunning {
		if job.cancel != nil {
			job.cancel()
		}
		job.Status = JobStatusCancelled
		now := time.Now()
		job.EndedAt = &now
		m.notifyListeners(job)
		return true
	}

	return false
}

// DeleteJob removes a job from the list.
func (m *JobManager) DeleteJob(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return false
	}

	// Cancel if running
	if job.cancel != nil && (job.Status == JobStatusQueued || job.Status == JobStatusRunning) {
		job.cancel()
	}

	delete(m.jobs, id)
	return true
}

// Subscribe adds a listener for job updates.
func (m *JobManager) Subscribe() chan *Job {
	ch := make(chan *Job, 100)
	m.listenerMu.Lock()
	m.listeners = append(m.listeners, ch)
	m.listenerMu.Unlock()
	return ch
}

// Unsubscribe removes a listener.
func (m *JobManager) Unsubscribe(ch chan *Job) {
	m.listenerMu.Lock()
	defer m.listenerMu.Unlock()

	for i, listener := range m.listeners {
		if listener == ch {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			close(ch)
			return
		}
	}
}

func (m *JobManager) notifyListeners(job *Job) {
	// Notify channel listeners
	m.listenerMu.RLock()
	for _, ch := range m.listeners {
		select {
		case ch <- job:
		default:
			// Listener is slow, skip
		}
	}
	m.listenerMu.RUnlock()

	// Broadcast to WebSocket clients
	if m.wsHub != nil {
		m.wsHub.BroadcastJob(job)
	}
}

// runJob executes the download job.
func (m *JobManager) runJob(job *Job) {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel

	// Update status
	m.mu.Lock()
	job.Status = JobStatusRunning
	now := time.Now()
	job.StartedAt = &now
	m.mu.Unlock()
	m.notifyListeners(job)

	// Create hfdownloader job and settings
	dlJob := hfdownloader.Job{
		Repo:               job.Repo,
		Revision:           job.Revision,
		IsDataset:          job.IsDataset,
		Filters:            job.Filters,
		Excludes:           job.Excludes,
		AppendFilterSubdir: false,
	}

	settings := hfdownloader.Settings{
		OutputDir:          job.OutputDir,
		Concurrency:        m.config.Concurrency,
		MaxActiveDownloads: m.config.MaxActive,
		Token:              m.config.Token,
		MultipartThreshold: m.config.MultipartThreshold,
		Verify:             m.config.Verify,
		Retries:            m.config.Retries,
		BackoffInitial:     "400ms",
		BackoffMax:         "10s",
		Endpoint:           m.config.Endpoint,
	}

	// Progress callback - NOTE: must not hold lock when calling notifyListeners
	progressFunc := func(evt hfdownloader.ProgressEvent) {
		m.mu.Lock()

		switch evt.Event {
		case "plan_item":
			job.Progress.TotalFiles++
			job.Progress.TotalBytes += evt.Total
			job.Files = append(job.Files, JobFileProgress{
				Path:       evt.Path,
				TotalBytes: evt.Total,
				Status:     "pending",
			})

		case "file_start":
			for i := range job.Files {
				if job.Files[i].Path == evt.Path {
					job.Files[i].Status = "active"
					break
				}
			}

		case "file_progress":
			for i := range job.Files {
				if job.Files[i].Path == evt.Path {
					job.Files[i].Downloaded = evt.Downloaded
					break
				}
			}
			// Update aggregate
			var total int64
			for _, f := range job.Files {
				total += f.Downloaded
			}
			job.Progress.DownloadedBytes = total

		case "file_done":
			for i := range job.Files {
				if job.Files[i].Path == evt.Path {
					job.Files[i].Status = "complete"
					job.Files[i].Downloaded = job.Files[i].TotalBytes
					break
				}
			}
			job.Progress.CompletedFiles++
			// Recalculate total downloaded
			var total int64
			for _, f := range job.Files {
				total += f.Downloaded
			}
			job.Progress.DownloadedBytes = total
		}

		m.mu.Unlock() // Unlock BEFORE notifying to avoid deadlock
		m.notifyListeners(job)
	}

	// Run the download
	err := hfdownloader.Run(ctx, dlJob, settings, progressFunc)

	// Update final status
	m.mu.Lock()
	endTime := time.Now()
	job.EndedAt = &endTime
	if ctx.Err() != nil {
		job.Status = JobStatusCancelled
	} else if err != nil {
		job.Status = JobStatusFailed
		job.Error = err.Error()
	} else {
		job.Status = JobStatusCompleted
	}
	m.mu.Unlock()

	m.notifyListeners(job)
}

