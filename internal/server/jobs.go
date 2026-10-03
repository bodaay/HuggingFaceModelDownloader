// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// JobStatus represents the state of a download job.
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusPaused    JobStatus = "paused"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// Job represents a download job.
type Job struct {
	ID         string            `json:"id"`
	Repo       string            `json:"repo"`
	Revision   string            `json:"revision"`
	IsDataset  bool              `json:"isDataset,omitempty"`
	Filters    []string          `json:"filters,omitempty"`
	Excludes   []string          `json:"excludes,omitempty"`
	OutputDir  string            `json:"outputDir"`
	Flat       bool              `json:"flat,omitempty"`       // Save real files (flat mode) instead of HF cache layout
	ExactMatch bool              `json:"exactMatch,omitempty"` // Match filters by whole name segment, not substring
	Status     JobStatus         `json:"status"`
	Progress   JobProgress       `json:"progress"`
	Error      string            `json:"error,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	EndedAt    *time.Time        `json:"endedAt,omitempty"`
	Files      []JobFileProgress `json:"files,omitempty"`

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
	// Activity describes a non-download phase or problem worth surfacing,
	// e.g. "Verifying model.gguf (45%)" or a retry reason. Empty while
	// bytes are simply flowing.
	Activity string `json:"activity,omitempty"`
}

// JobFileProgress holds per-file progress.
type JobFileProgress struct {
	Path       string `json:"path"`
	TotalBytes int64  `json:"totalBytes"`
	Downloaded int64  `json:"downloaded"`
	Status     string `json:"status"` // pending, active, assembling, verifying, complete, skipped
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

	// Serial execution (github issue #85): downloads run one model at a time,
	// mirroring the CLI. queue holds the IDs of jobs waiting to run in FIFO
	// order; busy is true while a job is executing. Per-model parallelism
	// (connections-per-file, files-per-model) is unaffected — only the number
	// of simultaneous *models* is capped at one. Both fields are guarded by mu.
	queue []string
	busy  bool

	// runFn performs a job's download. It defaults to runJob and exists so
	// tests can substitute a fake runner without touching the network.
	runFn func(*Job)
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
	m.runFn = m.runJob
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
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on supported platforms; if it somehow
		// does, panic rather than return a predictable or empty ID that two
		// jobs could collide on.
		panic("hfdownloader: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// snapshotConfig returns a copy of the manager's config under the read lock so
// callers never read a field while UpdateConfig is replacing it. The copy
// shares the *ProxyConfig pointer, which is safe because UpdateConfig publishes
// a fresh ProxyConfig on change rather than mutating the existing one in place.
func (m *JobManager) snapshotConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// UpdateConfig atomically replaces the manager's config. Called from
// POST /api/settings; in-flight runJob goroutines that already snapshotted the
// previous config keep using it until they finish.
func (m *JobManager) UpdateConfig(cfg Config) {
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()
}

// Stop halts background machinery owned by the manager (currently the
// WebSocket broadcast coalescer). Safe to call multiple times.
func (m *JobManager) Stop() {
	if m.wsCoalescer != nil {
		m.wsCoalescer.stop()
	}
}

// enqueueLocked appends a job ID to the run queue. Must hold m.mu (write).
func (m *JobManager) enqueueLocked(id string) {
	m.queue = append(m.queue, id)
}

// scheduleNextLocked starts the next queued job if no job is currently running.
// Downloads execute one model at a time (github issue #85) — additional jobs
// stay in the queue with status "queued" until the active one finishes. Jobs
// that were cancelled or deleted while waiting are skipped. Must hold m.mu
// (write).
func (m *JobManager) scheduleNextLocked() {
	if m.busy {
		return
	}
	for len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		job, ok := m.jobs[id]
		if !ok || job.Status != JobStatusQueued {
			continue // cancelled / deleted / no longer queued
		}
		m.busy = true
		m.runWG.Add(1)
		go func() {
			// finishRun (registered last → runs first) releases the slot and
			// starts the next job *before* runWG.Done, so the wait group never
			// momentarily drops to zero mid-chain.
			defer m.runWG.Done()
			defer m.finishRun()
			m.runFn(job)
		}()
		return
	}
}

// finishRun releases the single run slot and starts the next queued job.
func (m *JobManager) finishRun() {
	m.mu.Lock()
	m.busy = false
	m.scheduleNextLocked()
	m.mu.Unlock()
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

	cfg := m.snapshotConfig()

	// Use HuggingFace cache directory (v3 mode)
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	// Output directory shown to the client. In local mode the whole server
	// writes real files into LocalDir/<owner>/<repo>; otherwise the HF cache.
	flat := cfg.LocalDir != ""
	outputDir := cacheDir
	if flat {
		outputDir = cfg.LocalDir
	}

	// Check for existing active job with same repo+revision+type.
	// Returning a clone prevents the caller's JSON encoder from racing
	// against runJob's in-place mutations of the live job.
	m.mu.Lock()
	for _, existing := range m.jobs {
		// Same selection only: a request for the same repo with different
		// filters is a different download, not a duplicate.
		if existing.Repo == req.Repo &&
			existing.Revision == revision &&
			existing.IsDataset == req.Dataset &&
			sameStrings(existing.Filters, req.Filters) &&
			sameStrings(existing.Excludes, req.Excludes) &&
			existing.ExactMatch == req.ExactMatch &&
			(existing.Status == JobStatusQueued || existing.Status == JobStatusRunning) {
			snapshot := m.cloneJobLocked(existing)
			m.mu.Unlock()
			return snapshot, true, nil
		}
	}

	job := &Job{
		ID:         generateID(),
		Repo:       req.Repo,
		Revision:   revision,
		IsDataset:  req.Dataset,
		Filters:    req.Filters,
		Excludes:   req.Excludes,
		OutputDir:  outputDir,
		Flat:       flat,
		ExactMatch: req.ExactMatch,
		Status:     JobStatusQueued,
		CreatedAt:  time.Now(),
		Progress:   JobProgress{},
	}

	m.jobs[job.ID] = job
	// Enqueue and start it only if no other model is downloading. Extra jobs
	// wait their turn (status stays "queued") instead of all running at once.
	m.enqueueLocked(job.ID)
	m.scheduleNextLocked()
	snapshot := m.cloneJobLocked(job)
	m.mu.Unlock()

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
	// Re-queue; already-downloaded files are skipped when it actually runs.
	m.enqueueLocked(job.ID)
	m.scheduleNextLocked()
	snapshot := m.cloneJobLocked(job)
	m.mu.Unlock()

	// Notify listeners of status change
	m.notifyListeners(snapshot)

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

// runJob executes the download job. It is invoked through scheduleNextLocked's
// wrapper, which owns runWG bookkeeping and releases the run slot when runJob
// returns (so the next queued job can start).
func (m *JobManager) runJob(job *Job) {
	cfg := m.snapshotConfig()

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
		ExactMatch:         job.ExactMatch,
		AppendFilterSubdir: false,
	}

	// Use HuggingFace cache structure (v3 mode) instead of legacy OutputDir
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	settings := hfdownloader.Settings{
		CacheDir:           cacheDir, // Use HF cache structure
		Concurrency:        cfg.Concurrency,
		MaxActiveDownloads: cfg.MaxActive,
		Token:              cfg.Token,
		MultipartThreshold: cfg.MultipartThreshold,
		Verify:             cfg.Verify,
		Retries:            cfg.Retries,
		BackoffInitial:     "400ms",
		BackoffMax:         "10s",
		Endpoint:           cfg.Endpoint,
		Proxy:              cfg.Proxy,
		LinkMode:           cfg.LinkMode,
		// Recorded in the hfd.yaml manifest; the cache browser reads the
		// filters back from it to show filtered downloads as "filtered".
		Command: jobCommand(job),
	}

	// Local mode: write real files into LocalDir instead of the HF cache
	// layout. Clearing CacheDir forces flat-file output.
	if cfg.LocalDir != "" {
		settings.OutputDir = cfg.LocalDir
		settings.CacheDir = ""
	}

	// Progress callback - NOTE: must not hold lock when calling notifyListeners
	// activityPath is the file the current Activity message refers to, and
	// activityMark its byte count when the message was set: the message is
	// cleared once that file's download actually moves past the mark (the
	// multipart ticker keeps reporting unchanged bytes during a retry).
	var activityPath string
	var activityMark int64
	// Speed: an exponential moving average over ~1s samples.
	var speedLastBytes int64
	var speedLastTime time.Time
	var speedEMA float64
	findFile := func(path string) *JobFileProgress {
		for i := range job.Files {
			if job.Files[i].Path == path {
				return &job.Files[i]
			}
		}
		return nil
	}

	progressFunc := func(evt hfdownloader.ProgressEvent) {
		m.mu.Lock()

		// Ignore events from a run that has been paused, cancelled or
		// superseded by a resume: they would corrupt the new run's state.
		if job.generation != myGeneration || job.Status != JobStatusRunning {
			m.mu.Unlock()
			return
		}

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

		case "file_assemble", "file_verify":
			stage, verb := "assembling", "Assembling"
			if evt.Event == "file_verify" {
				stage, verb = "verifying", "Verifying"
			}
			if f := findFile(evt.Path); f != nil {
				f.Status = stage
			}
			pct := 0.0
			if evt.Total > 0 {
				pct = float64(evt.Downloaded) / float64(evt.Total) * 100
			}
			job.Progress.Activity = fmt.Sprintf("%s %s (%.0f%%)", verb, evt.Path, pct)
			activityPath, activityMark = evt.Path, evt.Total

		case "retry":
			job.Progress.Activity = fmt.Sprintf("Retrying %s (attempt %d): %s", evt.Path, evt.Attempt, evt.Message)
			activityPath, activityMark = evt.Path, 0
			if f := findFile(evt.Path); f != nil {
				activityMark = f.Downloaded
			}

		case "file_progress":
			if activityPath == evt.Path && evt.Downloaded > activityMark {
				job.Progress.Activity = ""
				activityPath = ""
			}
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
			now := time.Now()
			if speedLastTime.IsZero() {
				speedLastTime, speedLastBytes = now, total
			} else if dt := now.Sub(speedLastTime).Seconds(); dt >= 1 {
				rate := float64(total-speedLastBytes) / dt
				if rate < 0 {
					rate = 0
				}
				if speedEMA == 0 {
					speedEMA = rate
				} else {
					speedEMA = 0.3*rate + 0.7*speedEMA
				}
				job.Progress.BytesPerSecond = int64(speedEMA)
				speedLastTime, speedLastBytes = now, total
			}

		case "file_done":
			if activityPath == evt.Path {
				job.Progress.Activity = ""
				activityPath = ""
			}
			for i := range job.Files {
				if job.Files[i].Path == evt.Path {
					job.Files[i].Status = "complete"
					if strings.HasPrefix(evt.Message, "skip") {
						job.Files[i].Status = "skipped" // already in the cache
					}
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
	if !finishRunStatusLocked(job, myGeneration, ctx.Err(), err) {
		m.mu.Unlock()
		return
	}
	endSnap := m.cloneJobLocked(job)
	m.mu.Unlock()

	m.notifyListeners(endSnap)
}

// finishRunStatusLocked records a run's outcome on job and reports whether it
// did. It leaves the job alone when this run no longer owns it:
//  1. the job was paused (user intentionally stopped it);
//  2. the job was resumed (re-queued) while this paused run was still winding
//     down — the queued run owns it now, and marking it cancelled here would
//     make the scheduler skip it, silently killing the resume;
//  3. this is a stale run (a newer runJob has started).
//
// Must hold m.mu (write).
func finishRunStatusLocked(job *Job, myGeneration int, ctxErr, runErr error) bool {
	if job.Status == JobStatusPaused || job.Status == JobStatusQueued || job.generation != myGeneration {
		return false
	}
	endTime := time.Now()
	job.EndedAt = &endTime
	job.Progress.Activity = ""
	job.Progress.BytesPerSecond = 0
	if ctxErr != nil {
		job.Status = JobStatusCancelled
	} else if runErr != nil {
		job.Status = JobStatusFailed
		job.Error = runErr.Error()
	} else {
		job.Status = JobStatusCompleted
	}
	return true
}

// sameStrings reports whether two string lists hold the same values, in any
// order.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		if count[v]--; count[v] < 0 {
			return false
		}
	}
	return true
}

// HasActiveJob reports whether repo has a queued, running or paused job.
func (m *JobManager) HasActiveJob(repo string, isDataset bool) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, j := range m.jobs {
		if j.Repo == repo && j.IsDataset == isDataset &&
			(j.Status == JobStatusQueued || j.Status == JobStatusRunning || j.Status == JobStatusPaused) {
			return true
		}
	}
	return false
}

// jobCommand is the CLI command equivalent to a web download job.
func jobCommand(j *Job) string {
	parts := []string{"hfdownloader", "download", j.Repo}
	if j.IsDataset {
		parts = append(parts, "--dataset")
	}
	if j.Revision != "" && j.Revision != "main" {
		parts = append(parts, "-b", j.Revision)
	}
	if len(j.Filters) > 0 {
		parts = append(parts, "-F", strings.Join(j.Filters, ","))
	}
	if j.ExactMatch {
		parts = append(parts, "--exact")
	}
	if len(j.Excludes) > 0 {
		parts = append(parts, "-E", strings.Join(j.Excludes, ","))
	}
	return strings.Join(parts, " ")
}
