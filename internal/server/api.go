// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// DownloadRequest is the request body for starting a download.
// Note: Output path is NOT configurable via API for security reasons.
// The server uses its configured OutputDir (Models/ for models, Datasets/ for datasets).
type DownloadRequest struct {
	Repo               string   `json:"repo"`
	Revision           string   `json:"revision,omitempty"`
	Dataset            bool     `json:"dataset,omitempty"`
	Filters            []string `json:"filters,omitempty"`
	Excludes           []string `json:"excludes,omitempty"`
	AppendFilterSubdir bool     `json:"appendFilterSubdir,omitempty"`
	DryRun             bool     `json:"dryRun,omitempty"`
}

// PlanResponse is the response for a dry-run/plan request.
type PlanResponse struct {
	Repo       string     `json:"repo"`
	Revision   string     `json:"revision"`
	Files      []PlanFile `json:"files"`
	TotalSize  int64      `json:"totalSize"`
	TotalFiles int        `json:"totalFiles"`
}

// PlanFile represents a file in the plan.
type PlanFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	LFS  bool   `json:"lfs"`
}

// SettingsResponse represents current settings.
type SettingsResponse struct {
	Token              string `json:"token,omitempty"`
	ModelsDir          string `json:"modelsDir"`
	DatasetsDir        string `json:"datasetsDir"`
	Concurrency        int    `json:"connections"`
	MaxActive          int    `json:"maxActive"`
	MultipartThreshold string `json:"multipartThreshold"`
	Verify             string `json:"verify"`
	Retries            int    `json:"retries"`
	Endpoint           string `json:"endpoint,omitempty"`
}

// ErrorResponse represents an API error.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// SuccessResponse represents a simple success message.
type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// --- Handlers ---

// handleHealth returns server health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": "2.3.3",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// handleStartDownload starts a new download job.
func (s *Server) handleStartDownload(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	// Validate
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "Missing required field: repo", "")
		return
	}

	// Parse filters from repo:filter syntax
	if strings.Contains(req.Repo, ":") && len(req.Filters) == 0 {
		parts := strings.SplitN(req.Repo, ":", 2)
		req.Repo = parts[0]
		if parts[1] != "" {
			for _, f := range strings.Split(parts[1], ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					req.Filters = append(req.Filters, f)
				}
			}
		}
	}

	if !hfdownloader.IsValidModelName(req.Repo) {
		writeError(w, http.StatusBadRequest, "Invalid repo format", "Expected owner/name")
		return
	}

	// If dry-run, return the plan
	if req.DryRun {
		s.handlePlanInternal(w, req)
		return
	}

	// Create and start the job (or return existing if duplicate)
	job, wasExisting, err := s.jobs.CreateJob(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create job", err.Error())
		return
	}

	// Return appropriate status
	if wasExisting {
		// Job already exists for this repo - return it with 200
		writeJSON(w, http.StatusOK, map[string]any{
			"job":     job,
			"message": "Download already in progress",
		})
	} else {
		// New job created
		writeJSON(w, http.StatusAccepted, job)
	}
}

// handlePlan returns a download plan without starting the download.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	req.DryRun = true
	s.handlePlanInternal(w, req)
}

func (s *Server) handlePlanInternal(w http.ResponseWriter, req DownloadRequest) {
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "Missing required field: repo", "")
		return
	}

	// Parse filters from repo:filter syntax
	if strings.Contains(req.Repo, ":") && len(req.Filters) == 0 {
		parts := strings.SplitN(req.Repo, ":", 2)
		req.Repo = parts[0]
		if parts[1] != "" {
			for _, f := range strings.Split(parts[1], ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					req.Filters = append(req.Filters, f)
				}
			}
		}
	}

	revision := req.Revision
	if revision == "" {
		revision = "main"
	}

	// Create job for scanning
	dlJob := hfdownloader.Job{
		Repo:               req.Repo,
		Revision:           revision,
		IsDataset:          req.Dataset,
		Filters:            req.Filters,
		Excludes:           req.Excludes,
		AppendFilterSubdir: req.AppendFilterSubdir,
	}

	// Use server-configured output directory (not from request for security)
	outputDir := s.config.ModelsDir
	if req.Dataset {
		outputDir = s.config.DatasetsDir
	}

	settings := hfdownloader.Settings{
		OutputDir: outputDir,
		Token:     s.config.Token,
		Endpoint:  s.config.Endpoint,
	}

	// Collect plan items
	var files []PlanFile
	var totalSize int64

	progressFunc := func(evt hfdownloader.ProgressEvent) {
		if evt.Event == "plan_item" {
			files = append(files, PlanFile{
				Path: evt.Path,
				Size: evt.Total,
				LFS:  evt.IsLFS,
			})
			totalSize += evt.Total
		}
	}

	// Run in dry-run mode (plan only)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// We need to get the plan - use a modified Run that returns early
	// For now, we'll scan the repo manually
	err := hfdownloader.ScanPlan(ctx, dlJob, settings, progressFunc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to scan repository", err.Error())
		return
	}

	resp := PlanResponse{
		Repo:       req.Repo,
		Revision:   revision,
		Files:      files,
		TotalSize:  totalSize,
		TotalFiles: len(files),
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleListJobs returns all jobs.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobs.ListJobs()
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// handleGetJob returns a specific job.
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Missing job ID", "")
		return
	}

	job, ok := s.jobs.GetJob(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found", "")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// handleCancelJob cancels a job.
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Missing job ID", "")
		return
	}

	if s.jobs.CancelJob(id) {
		writeJSON(w, http.StatusOK, SuccessResponse{
			Success: true,
			Message: "Job cancelled",
		})
	} else {
		writeError(w, http.StatusNotFound, "Job not found or already completed", "")
	}
}

// handleGetSettings returns current settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	// Don't expose full token, just indicate if set
	tokenStatus := ""
	if s.config.Token != "" {
		tokenStatus = "********" + s.config.Token[max(0, len(s.config.Token)-4):]
	}

	resp := SettingsResponse{
		Token:              tokenStatus,
		ModelsDir:          s.config.ModelsDir,
		DatasetsDir:        s.config.DatasetsDir,
		Concurrency:        s.config.Concurrency,
		MaxActive:          s.config.MaxActive,
		MultipartThreshold: s.config.MultipartThreshold,
		Verify:             s.config.Verify,
		Retries:            s.config.Retries,
		Endpoint:           s.config.Endpoint,
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateSettings updates settings.
// Note: Output directories cannot be changed via API for security.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token              *string `json:"token,omitempty"`
		Concurrency        *int    `json:"connections,omitempty"`
		MaxActive          *int    `json:"maxActive,omitempty"`
		MultipartThreshold *string `json:"multipartThreshold,omitempty"`
		Verify             *string `json:"verify,omitempty"`
		Retries            *int    `json:"retries,omitempty"`
		// Note: ModelsDir and DatasetsDir are NOT updatable via API for security
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	// Update config (only safe fields)
	if req.Token != nil {
		s.config.Token = *req.Token
	}
	if req.Concurrency != nil && *req.Concurrency > 0 {
		s.config.Concurrency = *req.Concurrency
	}
	if req.MaxActive != nil && *req.MaxActive > 0 {
		s.config.MaxActive = *req.MaxActive
	}
	if req.MultipartThreshold != nil && *req.MultipartThreshold != "" {
		s.config.MultipartThreshold = *req.MultipartThreshold
	}
	if req.Verify != nil && *req.Verify != "" {
		s.config.Verify = *req.Verify
	}
	if req.Retries != nil && *req.Retries > 0 {
		s.config.Retries = *req.Retries
	}

	// Also update job manager config
	s.jobs.config = s.config

	writeJSON(w, http.StatusOK, SuccessResponse{
		Success: true,
		Message: "Settings updated",
	})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message, details string) {
	writeJSON(w, status, ErrorResponse{
		Error:   message,
		Details: details,
	})
}
