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
	JobStatusPaused     JobStatus = "paused"
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
	StorageMode string          `json:"storageMode,omitempty"`
	Status    JobStatus         `json:"status"`
	Progress  JobProgress       `json:"progress"`
	Error     string            `json:"error,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	StartedAt *time.Time        `json:"startedAt,omitempty"`
	EndedAt   *time.Time        `json:"endedAt,omitempty"`
	Files     []JobFileProgress `json:"files,omitempty"`

	cancel     context.CancelFunc `json:"-"`
	generation int                `json:"-"` // Tracks which runJob instance is current
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
	mu          sync.RWMutex
	jobs        map[string]*Job
	config      Config
	listeners   []chan *Job
	listenerMu  sync.RWMutex
	wsHub       *WSHub
	wsCoalescer *jobCoalescer
	// runWG tracks in-flight runJob goroutines so shutdown paths (and
	// tests) can wait for every download to actually unwind — not just
	// for Status to flip to Cancelled. Without this a t.TempDir cleanup
	// can race a still-in-flight mkdir inside the downloader and fail
	// with "directory not empty".
	runWG sync.WaitGroup
}

// wsBroadcastMinGap is the minimum interval between consecutive WebSocket
// broadcasts for the same job. Progress events arriving inside this window
// are coalesced — only the latest job state is flushed when the window
// elapses. Terminal status changes (completed, failed, cancelled, paused)
// bypass this gate and are sent immediately. See github issue #62.
const wsBroadcastMinGap = 250 * time.Millisecond

// NewJobManager creates a new job manager.
func NewJobManager(cfg Config, wsHub *WSHub) *JobManager {
	m := &JobManager{
		jobs:   make(map[string]*Job),
		config: cfg,
		wsHub:  wsHub,
	}
	if wsHub != nil {
		m.wsCoalescer = newJobCoalescer(wsBroadcastMinGap, func(j *Job) {
			wsHub.BroadcastJob(j)
		})
	}
	return m
}

// generateID creates a short random ID.
func generateID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// cloneJobLocked returns a fully-independent copy of a Job. Must be called
// while m.mu is held (any lock, read or write) so the fields being copied
// are stable. The returned *Job can be safely handed to JSON encoders or
// WebSocket broadcasters without racing against runJob's in-place mutations
// of the live Job stored in m.jobs. Slice fields are deep-copied so that
// subsequent mutations of the live job's slices can't leak through a shared
// backing array.
func (m *JobManager) cloneJobLocked(j *Job) *Job {
	if j == nil {
		return nil
	}
	clone := *j
	clone.cancel = nil
	if j.Filters != nil {
		clone.Filters = append([]string(nil), j.Filters...)
	}
	if j.Excludes != nil {
		clone.Excludes = append([]string(nil), j.Excludes...)
	}
	if j.Files != nil {
		clone.Files = append([]JobFileProgress(nil), j.Files...)
	}
	if j.StartedAt != nil {
		t := *j.StartedAt
		clone.StartedAt = &t
	}
	if j.EndedAt != nil {
		t := *j.EndedAt
		clone.EndedAt = &t
	}
	return &clone
}

// CreateJob creates a new download job.
// Returns existing job if same repo+revision+dataset is already in progress.
func (m *JobManager) CreateJob(req DownloadRequest) (*Job, bool, error) {
	revision := req.Revision
	if revision == "" {
		revision = "main"
	}

	// Determine output directory and storage mode
	storageMode := req.StorageMode
	if storageMode == "" {
		storageMode = string(m.config.StorageMode)
	}
	if storageMode == "" {
		storageMode = string(StorageModeCache)
	}

	var outputDir string
	baseDir := m.config.CacheDir
	if baseDir == "" {
		baseDir = hfdownloader.DefaultCacheDir()
	}

	switch storageMode {
	case string(StorageModeFlat):
		// Flat file mode: files go directly to base directory
		outputDir = baseDir
	case string(StorageModeFlatStructured):
		// Flat structured mode: base path is baseDir; downloader appends owner/model once
		outputDir = baseDir
	default:
		// Cache mode (default): use the cache directory (hfdownloader will create hub/models--)
		outputDir = baseDir
	}

	// Check for existing active job with same repo+revision+type.
	// Returning a clone prevents the caller's JSON encoder from racing
	// against runJob's in-place mutations of the live job.
	m.mu.Lock()
	for _, existing := range m.jobs {
		if existing.Repo == req.Repo &&
			existing.Revision == revision &&
			existing.IsDataset == req.Dataset &&
			(existing.Status == JobStatusQueued || existing.Status == JobStatusRunning) {
			snapshot := m.cloneJobLocked(existing)
			m.mu.Unlock()
			return snapshot, true, nil
		}
	}

	job := &Job{
		ID:          generateID(),
		Repo:        req.Repo,
		Revision:    revision,
		IsDataset:   req.Dataset,
		Filters:     req.Filters,
		Excludes:    req.Excludes,
		OutputDir:   outputDir,
		StorageMode: storageMode,
		Status:      JobStatusQueued,
		CreatedAt:   time.Now(),
		Progress:    JobProgress{},
	}

	m.jobs[job.ID] = job
	snapshot := m.cloneJobLocked(job)
	m.mu.Unlock()

	// Start the job
	m.runWG.Add(1)
	go m.runJob(job)

	return snapshot, false, nil
}

// GetJob retrieves a snapshot of a job by ID. The returned pointer is a
// standalone copy; the caller can read its fields without racing against
// the runJob goroutine that owns the live version in m.jobs.
func (m *JobManager) GetJob(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	return m.cloneJobLocked(job), true
}

// ListJobs returns snapshots of all jobs. Each returned *Job is an
// independent copy — safe to JSON-encode or hand to the WebSocket hub
// without holding any lock.
func (m *JobManager) ListJobs() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, m.cloneJobLocked(job))
	}
	return jobs
}

// CancelJob cancels a running or queued job.
func (m *JobManager) CancelJob(id string) bool {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false
	}

	if job.Status != JobStatusQueued && job.Status != JobStatusRunning && job.Status != JobStatusPaused {
		m.mu.Unlock()
		return false
	}

	if job.cancel != nil {
		job.cancel()
	}
	job.Status = JobStatusCancelled
	now := time.Now()
	job.EndedAt = &now
	snapshot := m.cloneJobLocked(job)
	m.mu.Unlock()

	m.notifyListeners(snapshot)
	return true
}

// PauseJob pauses a running job.
func (m *JobManager) PauseJob(id string) bool {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false
	}

	if job.Status != JobStatusRunning {
		m.mu.Unlock()
		return false
	}

	if job.cancel != nil {
		job.cancel()
	}
	job.Status = JobStatusPaused
	snapshot := m.cloneJobLocked(job)
	m.mu.Unlock()

	m.notifyListeners(snapshot)
	return true
}

// ResumeJob resumes a paused job.
func (m *JobManager) ResumeJob(id string) bool {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false
	}

	if job.Status != JobStatusPaused {
		m.mu.Unlock()
		return false
	}

	job.Status = JobStatusQueued
	// Reset progress - the downloader will re-scan and report all files.
	// Already-downloaded files will be skipped during actual download but
	// reported in plan.
	job.Progress = JobProgress{}
	job.Files = nil
	snapshot := m.cloneJobLocked(job)
	m.mu.Unlock()

	// Notify listeners of status change
	m.notifyListeners(snapshot)

	// Restart the job - already downloaded files will be skipped by the downloader
	m.runWG.Add(1)
	go m.runJob(job)

	return true
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

// WaitAll blocks until every in-flight runJob goroutine has returned or
// until timeout elapses. Returns true if all goroutines exited cleanly,
// false on timeout. Primarily for tests and graceful shutdown — lets
// callers observe actual goroutine exit rather than just Status==Cancelled,
// which is set before the downloader's filesystem operations fully unwind.
func (m *JobManager) WaitAll(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		m.runWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// DismissJobResult distinguishes the three possible outcomes of a dismiss
// attempt so the HTTP layer can map them to appropriate status codes.
type DismissJobResult int

const (
	// DismissJobOK means the job was in a terminal state and has been removed.
	DismissJobOK DismissJobResult = iota
	// DismissJobNotFound means no job with that ID exists.
	DismissJobNotFound
	// DismissJobStillActive means the job is queued or running; it must be
	// cancelled first (or completed) before it can be dismissed.
	DismissJobStillActive
)

// DismissJob removes a job from the manager if and only if it is in a
// terminal state (completed, failed, cancelled, paused). Dismissal is the
// user's way of hiding a finished job from the UI permanently, and the
// guarantee that matters for github issue #68 is that the job does not
// come back on the next page refresh — so the underlying storage drops it.
// Dismissing a queued or running job is rejected so a stray click can't
// wipe a live download.
func (m *JobManager) DismissJob(id string) bool {
	res, _ := m.DismissJobResult(id)
	return res == DismissJobOK
}

// DismissJobResult is the richer variant of DismissJob that returns the
// reason a dismissal failed, for use by the HTTP handler.
func (m *JobManager) DismissJobResult(id string) (DismissJobResult, *Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return DismissJobNotFound, nil
	}
	if !isTerminalJobStatus(job.Status) {
		return DismissJobStillActive, job
	}
	delete(m.jobs, id)
	return DismissJobOK, job
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

// notifyListeners forwards an already-snapshotted job update to channel
// listeners and the WebSocket broadcast path. The caller MUST pass in a
// snapshot (produced by cloneJobLocked while holding m.mu) — this function
// does not take m.mu itself, so it is safe to call from sites that already
// hold m.mu.Lock() (like CancelJob / PauseJob with a deferred unlock).
func (m *JobManager) notifyListeners(snapshot *Job) {
	// Notify channel listeners (tests and other internal subscribers see
	// every raw update; only the WebSocket path is throttled).
	m.listenerMu.RLock()
	for _, ch := range m.listeners {
		select {
		case ch <- snapshot:
		default:
			// Listener is slow, skip
		}
	}
	m.listenerMu.RUnlock()

	// Broadcast to WebSocket clients through the per-job coalescer so the
	// browser isn't asked to re-render at 5Hz × file-count.
	if m.wsCoalescer != nil {
		m.wsCoalescer.schedule(snapshot)
	} else if m.wsHub != nil {
		m.wsHub.BroadcastJob(snapshot)
	}
}

// runJob executes the download job.
func (m *JobManager) runJob(job *Job) {
	defer m.runWG.Done()

	ctx, cancel := context.WithCancel(context.Background())

	// Increment generation and store our generation number
	m.mu.Lock()
	job.cancel = cancel
	job.generation++
	myGeneration := job.generation // Track which generation we are
	job.Status = JobStatusRunning
	now := time.Now()
	job.StartedAt = &now
	startSnap := m.cloneJobLocked(job)
	m.mu.Unlock()
	m.notifyListeners(startSnap)

	// Create hfdownloader job and settings
	dlJob := hfdownloader.Job{
		Repo:               job.Repo,
		Revision:           job.Revision,
		IsDataset:          job.IsDataset,
		Filters:            job.Filters,
		Excludes:           job.Excludes,
		AppendFilterSubdir: false,
	}

	// Build settings based on storage mode
	settings := hfdownloader.Settings{
		Concurrency:        m.config.Concurrency,
		MaxActiveDownloads: m.config.MaxActive,
		Token:              m.config.Token,
		MultipartThreshold: m.config.MultipartThreshold,
		Verify:             m.config.Verify,
		Retries:            m.config.Retries,
		BackoffInitial:     "400ms",
		BackoffMax:         "10s",
		Endpoint:           m.config.Endpoint,
		Proxy:              m.config.Proxy,
	}

	// Set output based on storage mode
	storageMode := job.StorageMode
	if storageMode == string(StorageModeFlat) || storageMode == string(StorageModeFlatStructured) {
		// Flat modes: use OutputDir directly, no HF cache structure
		// MUST clear CacheDir so hfdownloader doesn't create hub/models--owner--name/
		settings.OutputDir = job.OutputDir
		settings.CacheDir = ""
		settings.NoRepoSubdir = storageMode == string(StorageModeFlat)
		settings.NoFriendlyView = true
		settings.NoManifest = false // Still create manifest
	} else {
		// Cache mode: use CacheDir (HuggingFace cache structure)
		cacheDir := m.config.CacheDir
		if cacheDir == "" {
			cacheDir = hfdownloader.DefaultCacheDir()
		}
		settings.CacheDir = cacheDir
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

		progressSnap := m.cloneJobLocked(job)
		m.mu.Unlock() // Unlock BEFORE notifying to avoid deadlock
		m.notifyListeners(progressSnap)
	}

	// Run the download
	err := hfdownloader.Run(ctx, dlJob, settings, progressFunc)

	// Update final status
	m.mu.Lock()
	// Don't update status if:
	// 1. Job was paused (user intentionally stopped it)
	// 2. We're a stale goroutine (a newer runJob has started)
	if job.Status == JobStatusPaused || job.generation != myGeneration {
		m.mu.Unlock()
		return
	}
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
	endSnap := m.cloneJobLocked(job)
	m.mu.Unlock()

	m.notifyListeners(endSnap)
}

