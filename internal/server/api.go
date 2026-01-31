// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
	"github.com/bodaay/HuggingFaceModelDownloader/pkg/smartdl"
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
	CacheDir           string `json:"cacheDir"`
	Concurrency        int    `json:"connections"`
	MaxActive          int    `json:"maxActive"`
	MultipartThreshold string `json:"multipartThreshold"`
	Verify             string `json:"verify"`
	Retries            int    `json:"retries"`
	Endpoint           string `json:"endpoint,omitempty"`
	// Proxy settings
	Proxy *ProxySettingsResponse `json:"proxy,omitempty"`
	// Config file paths
	ConfigFile  string `json:"configFile,omitempty"`
	TargetsFile string `json:"targetsFile,omitempty"`
}

// ProxySettingsResponse represents proxy configuration in API responses.
type ProxySettingsResponse struct {
	URL                string `json:"url,omitempty"`
	Username           string `json:"username,omitempty"`
	NoProxy            string `json:"noProxy,omitempty"`
	NoEnvProxy         bool   `json:"noEnvProxy,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
	// Note: Password is intentionally omitted from response for security
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
		"version": "3.0.3",
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

// handlePauseJob pauses a running job.
func (s *Server) handlePauseJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Missing job ID", "")
		return
	}

	if s.jobs.PauseJob(id) {
		writeJSON(w, http.StatusOK, SuccessResponse{
			Success: true,
			Message: "Job paused",
		})
	} else {
		writeError(w, http.StatusNotFound, "Job not found or not running", "")
	}
}

// handleResumeJob resumes a paused job.
func (s *Server) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Missing job ID", "")
		return
	}

	if s.jobs.ResumeJob(id) {
		writeJSON(w, http.StatusOK, SuccessResponse{
			Success: true,
			Message: "Job resumed",
		})
	} else {
		writeError(w, http.StatusNotFound, "Job not found or not paused", "")
	}
}

// handleGetSettings returns current settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	// Don't expose full token, just indicate if set
	tokenStatus := ""
	if s.config.Token != "" {
		tokenStatus = "********" + s.config.Token[max(0, len(s.config.Token)-4):]
	}

	cacheDir := s.config.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	resp := SettingsResponse{
		Token:              tokenStatus,
		CacheDir:           cacheDir,
		Concurrency:        s.config.Concurrency,
		MaxActive:          s.config.MaxActive,
		MultipartThreshold: s.config.MultipartThreshold,
		Verify:             s.config.Verify,
		Retries:            s.config.Retries,
		Endpoint:           s.config.Endpoint,
		ConfigFile:         ConfigPath(),
		TargetsFile:        hfdownloader.DefaultTargetsPath(),
	}

	// Add proxy settings (without password for security)
	if s.config.Proxy != nil && s.config.Proxy.URL != "" {
		resp.Proxy = &ProxySettingsResponse{
			URL:                s.config.Proxy.URL,
			Username:           s.config.Proxy.Username,
			NoProxy:            s.config.Proxy.NoProxy,
			NoEnvProxy:         s.config.Proxy.NoEnvProxy,
			InsecureSkipVerify: s.config.Proxy.InsecureSkipVerify,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateSettings updates settings and persists them to config file.
// Note: Output directories cannot be changed via API for security.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token              *string `json:"token,omitempty"`
		Concurrency        *int    `json:"connections,omitempty"`
		MaxActive          *int    `json:"maxActive,omitempty"`
		MultipartThreshold *string `json:"multipartThreshold,omitempty"`
		Verify             *string `json:"verify,omitempty"`
		Retries            *int    `json:"retries,omitempty"`
		Endpoint           *string `json:"endpoint,omitempty"`
		// Proxy settings
		Proxy *struct {
			URL                *string `json:"url,omitempty"`
			Username           *string `json:"username,omitempty"`
			Password           *string `json:"password,omitempty"`
			NoProxy            *string `json:"noProxy,omitempty"`
			NoEnvProxy         *bool   `json:"noEnvProxy,omitempty"`
			InsecureSkipVerify *bool   `json:"insecureSkipVerify,omitempty"`
		} `json:"proxy,omitempty"`
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
	if req.Endpoint != nil {
		s.config.Endpoint = *req.Endpoint
	}

	// Update proxy settings
	if req.Proxy != nil {
		if s.config.Proxy == nil {
			s.config.Proxy = &hfdownloader.ProxyConfig{}
		}
		if req.Proxy.URL != nil {
			s.config.Proxy.URL = *req.Proxy.URL
		}
		if req.Proxy.Username != nil {
			s.config.Proxy.Username = *req.Proxy.Username
		}
		if req.Proxy.Password != nil {
			s.config.Proxy.Password = *req.Proxy.Password
		}
		if req.Proxy.NoProxy != nil {
			s.config.Proxy.NoProxy = *req.Proxy.NoProxy
		}
		if req.Proxy.NoEnvProxy != nil {
			s.config.Proxy.NoEnvProxy = *req.Proxy.NoEnvProxy
		}
		if req.Proxy.InsecureSkipVerify != nil {
			s.config.Proxy.InsecureSkipVerify = *req.Proxy.InsecureSkipVerify
		}
		// Clear proxy if URL is empty
		if s.config.Proxy.URL == "" {
			s.config.Proxy = nil
		}
	}

	// Also update job manager config
	s.jobs.config = s.config

	// Persist settings to config file
	fileCfg := &ConfigFile{
		Token:              s.config.Token,
		Connections:        s.config.Concurrency,
		MaxActive:          s.config.MaxActive,
		MultipartThreshold: s.config.MultipartThreshold,
		Verify:             s.config.Verify,
		Retries:            s.config.Retries,
		Endpoint:           s.config.Endpoint,
	}
	// Add proxy to config file if set
	if s.config.Proxy != nil {
		fileCfg.Proxy = &ProxyConfig{
			URL:                s.config.Proxy.URL,
			Username:           s.config.Proxy.Username,
			Password:           s.config.Proxy.Password,
			NoProxy:            s.config.Proxy.NoProxy,
			NoEnvProxy:         s.config.Proxy.NoEnvProxy,
			InsecureSkipVerify: s.config.Proxy.InsecureSkipVerify,
		}
	}
	if err := SaveConfigFile(fileCfg); err != nil {
		// Log error but don't fail the request - settings are still applied in-memory
		writeJSON(w, http.StatusOK, SuccessResponse{
			Success: true,
			Message: "Settings updated (warning: could not persist to config file)",
		})
		return
	}

	writeJSON(w, http.StatusOK, SuccessResponse{
		Success: true,
		Message: "Settings saved",
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

// --- Smart Analyzer ---

// handleAnalyze analyzes a HuggingFace repository.
func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	// Get repo from path (supports owner/name format)
	repo := r.PathValue("repo")
	if repo == "" {
		writeError(w, http.StatusBadRequest, "Missing repository", "Format: /api/analyze/owner/name")
		return
	}

	// Check if it's a dataset (explicit selection)
	isDataset := r.URL.Query().Get("dataset") == "true"

	// Get revision (defaults to "main")
	revision := r.URL.Query().Get("revision")
	if revision == "" {
		revision = "main"
	}

	// Create analyzer
	opts := smartdl.AnalyzerOptions{
		Token:    s.config.Token,
		Endpoint: s.config.Endpoint,
	}
	analyzer := smartdl.NewAnalyzer(opts)

	// Analyze with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	info, err := analyzer.AnalyzeWithRevision(ctx, repo, isDataset, revision)
	if err != nil {
		// Check if both model and dataset exist
		if err == smartdl.ErrBothExist {
			writeJSON(w, http.StatusOK, map[string]any{
				"needsSelection": true,
				"repo":           repo,
				"message":        "This repository exists as both a model and a dataset. Please select which one you want to analyze.",
				"options":        []string{"model", "dataset"},
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "Analysis failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// --- Cache Browser ---

// CachedRepoInfo represents a cached repository for the API response.
type CachedRepoInfo struct {
	Repo           string            `json:"repo"`
	Owner          string            `json:"owner"`
	Name           string            `json:"name"`
	Type           string            `json:"type"` // "model" or "dataset"
	Path           string            `json:"path"`
	FriendlyPath   string            `json:"friendlyPath,omitempty"`
	Size           int64             `json:"size"`
	SizeHuman      string            `json:"sizeHuman"`
	FileCount      int               `json:"fileCount"`
	Branch         string            `json:"branch,omitempty"`
	Commit         string            `json:"commit,omitempty"`
	Downloaded     string            `json:"downloaded,omitempty"`
	DownloadStatus string            `json:"downloadStatus,omitempty"` // "complete", "filtered", "unknown"
	Snapshots      []string          `json:"snapshots,omitempty"`
	Files          []CachedFileInfo  `json:"files,omitempty"`
	Manifest       *ManifestInfo     `json:"manifest,omitempty"`
}

// CachedFileInfo represents a file in the cache.
type CachedFileInfo struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SizeHuman string `json:"sizeHuman"`
	IsLFS     bool   `json:"isLfs"`
}

// ManifestInfo contains manifest data if available.
type ManifestInfo struct {
	Branch      string `json:"branch"`
	Commit      string `json:"commit"`
	Downloaded  string `json:"downloaded"`
	Command     string `json:"command,omitempty"`
	TotalSize   int64  `json:"totalSize"`
	TotalFiles  int    `json:"totalFiles"`
	IsFiltered  bool   `json:"isFiltered"`  // True if download used filters
	Filters     string `json:"filters,omitempty"` // The filter string if used
}

// CacheStats contains aggregate statistics about the cache.
type CacheStats struct {
	TotalModels   int    `json:"totalModels"`
	TotalDatasets int    `json:"totalDatasets"`
	TotalSize     int64  `json:"totalSize"`
	TotalSizeHuman string `json:"totalSizeHuman"`
	TotalFiles    int    `json:"totalFiles"`
}

// handleCacheList lists all cached repositories with rich metadata.
func (s *Server) handleCacheList(w http.ResponseWriter, r *http.Request) {
	cacheDir := s.config.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	// Get query params
	repoType := r.URL.Query().Get("type") // "model" or "dataset"
	search := strings.ToLower(r.URL.Query().Get("search"))

	cache := hfdownloader.NewHFCache(cacheDir, 0)
	repoDirs, err := cache.ListRepos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list cache", err.Error())
		return
	}

	var repos []CachedRepoInfo
	var stats CacheStats

	for _, rd := range repoDirs {
		rdType := string(rd.Type())
		repoID := rd.RepoID()

		// Filter by type if specified
		if repoType != "" {
			if repoType == "dataset" && rdType != "dataset" {
				continue
			}
			if repoType == "model" && rdType != "model" {
				continue
			}
		}

		// Filter by search term
		if search != "" && !strings.Contains(strings.ToLower(repoID), search) {
			continue
		}

		// Get size by walking blobs directory
		blobsDir := rd.BlobsDir()
		var totalSize int64
		var fileCount int
		filepath.Walk(blobsDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && !strings.HasSuffix(path, ".incomplete") && !strings.HasSuffix(path, ".meta") {
				totalSize += info.Size()
				fileCount++
			}
			return nil
		})

		// Update stats
		if rdType == "model" {
			stats.TotalModels++
		} else {
			stats.TotalDatasets++
		}
		stats.TotalSize += totalSize
		stats.TotalFiles += fileCount

		// Try to read commit from refs/main
		branch := "main"
		commit, _ := rd.ReadRef("main")
		if commit == "" {
			// Try other common refs
			commit, _ = rd.ReadRef("master")
			if commit != "" {
				branch = "master"
			}
		}

		// Get modification time from blobs dir
		var downloaded string
		if info, err := os.Stat(blobsDir); err == nil {
			downloaded = info.ModTime().Format("2006-01-02")
		}

		// Try to read manifest from friendly path
		var manifest *ManifestInfo
		var downloadStatus string
		friendlyPath := rd.FriendlyPath()
		manifestPath := filepath.Join(friendlyPath, hfdownloader.ManifestFilename)
		if m, err := hfdownloader.ReadManifest(manifestPath); err == nil {
			// Parse command for filter flags
			isFiltered, filters := parseCommandFilters(m.Command)

			manifest = &ManifestInfo{
				Branch:     m.Branch,
				Commit:     m.Commit,
				Downloaded: m.CompletedAt.Format("2006-01-02 15:04"),
				Command:    m.Command,
				TotalSize:  m.TotalSize,
				TotalFiles: m.TotalFiles,
				IsFiltered: isFiltered,
				Filters:    filters,
			}
			// Override with manifest data if available
			if m.Branch != "" {
				branch = m.Branch
			}
			if m.Commit != "" {
				commit = m.Commit
			}
			downloaded = m.CompletedAt.Format("2006-01-02")

			// Set download status based on manifest
			if isFiltered {
				downloadStatus = "filtered"
			} else {
				downloadStatus = "complete"
			}
		} else {
			// No manifest - either downloaded by Python or external tool
			downloadStatus = "unknown"
		}

		// Shorten commit hash
		shortCommit := commit
		if len(shortCommit) > 7 {
			shortCommit = shortCommit[:7]
		}

		repo := CachedRepoInfo{
			Repo:           repoID,
			Owner:          rd.Owner(),
			Name:           rd.Name(),
			Type:           rdType,
			Path:           rd.Path(),
			FriendlyPath:   friendlyPath,
			Size:           totalSize,
			SizeHuman:      humanSizeBytes(totalSize),
			FileCount:      fileCount,
			Branch:         branch,
			Commit:         shortCommit,
			Downloaded:     downloaded,
			DownloadStatus: downloadStatus,
			Manifest:       manifest,
		}
		repos = append(repos, repo)
	}

	stats.TotalSizeHuman = humanSizeBytes(stats.TotalSize)

	writeJSON(w, http.StatusOK, map[string]any{
		"repos":    repos,
		"stats":    stats,
		"cacheDir": cacheDir,
	})
}

// handleCacheInfo returns details about a specific cached repository.
func (s *Server) handleCacheInfo(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if repo == "" {
		writeError(w, http.StatusBadRequest, "Missing repository", "Format: /api/cache/owner/name")
		return
	}

	cacheDir := s.config.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	cache := hfdownloader.NewHFCache(cacheDir, 0)

	// Try as model first
	repoDir, err := cache.Repo(repo, hfdownloader.RepoTypeModel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid repository format", err.Error())
		return
	}

	// Check if the path exists
	if _, err := os.Stat(repoDir.Path()); os.IsNotExist(err) {
		// Try as dataset
		repoDir, _ = cache.Repo(repo, hfdownloader.RepoTypeDataset)
		if _, err := os.Stat(repoDir.Path()); os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "Repository not found in cache", "")
			return
		}
	}

	// Get snapshots
	snapshots, _ := repoDir.ListSnapshots()

	// Get size and file list by walking blobs directory
	blobsDir := repoDir.BlobsDir()
	var totalSize int64
	var files []CachedFileInfo

	// If we have snapshots, walk the latest one to get file names
	if len(snapshots) > 0 {
		// Use the first snapshot (usually the most recent)
		snapshotDir := repoDir.SnapshotDir(snapshots[0])
		filepath.Walk(snapshotDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			// Get actual size by following symlink to blob
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			realInfo, err := os.Stat(realPath)
			if err != nil {
				return nil
			}
			relPath, _ := filepath.Rel(snapshotDir, path)
			files = append(files, CachedFileInfo{
				Name:      relPath,
				Size:      realInfo.Size(),
				SizeHuman: humanSizeBytes(realInfo.Size()),
				IsLFS:     realInfo.Size() > 10*1024*1024, // Assume >10MB is LFS
			})
			totalSize += realInfo.Size()
			return nil
		})
	} else {
		// No snapshots, just count blobs
		filepath.Walk(blobsDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && !strings.HasSuffix(path, ".incomplete") && !strings.HasSuffix(path, ".meta") {
				totalSize += info.Size()
				files = append(files, CachedFileInfo{
					Name:      filepath.Base(path),
					Size:      info.Size(),
					SizeHuman: humanSizeBytes(info.Size()),
					IsLFS:     info.Size() > 10*1024*1024,
				})
			}
			return nil
		})
	}

	// Try to read commit and branch
	branch := "main"
	commit, _ := repoDir.ReadRef("main")
	if commit == "" {
		commit, _ = repoDir.ReadRef("master")
		if commit != "" {
			branch = "master"
		}
	}

	// Try to read manifest
	var manifest *ManifestInfo
	var downloadStatus string
	friendlyPath := repoDir.FriendlyPath()
	manifestPath := filepath.Join(friendlyPath, hfdownloader.ManifestFilename)
	if m, err := hfdownloader.ReadManifest(manifestPath); err == nil {
		// Parse command for filter flags
		isFiltered, filters := parseCommandFilters(m.Command)

		manifest = &ManifestInfo{
			Branch:     m.Branch,
			Commit:     m.Commit,
			Downloaded: m.CompletedAt.Format("2006-01-02 15:04"),
			Command:    m.Command,
			TotalSize:  m.TotalSize,
			TotalFiles: m.TotalFiles,
			IsFiltered: isFiltered,
			Filters:    filters,
		}
		if m.Branch != "" {
			branch = m.Branch
		}
		if m.Commit != "" {
			commit = m.Commit
		}

		// Set download status based on manifest
		if isFiltered {
			downloadStatus = "filtered"
		} else {
			downloadStatus = "complete"
		}
	} else {
		// No manifest - either downloaded by Python or external tool
		downloadStatus = "unknown"
	}

	shortCommit := commit
	if len(shortCommit) > 7 {
		shortCommit = shortCommit[:7]
	}

	info := CachedRepoInfo{
		Repo:           repoDir.RepoID(),
		Owner:          repoDir.Owner(),
		Name:           repoDir.Name(),
		Type:           string(repoDir.Type()),
		Path:           repoDir.Path(),
		FriendlyPath:   friendlyPath,
		Size:           totalSize,
		SizeHuman:      humanSizeBytes(totalSize),
		FileCount:      len(files),
		Branch:         branch,
		Commit:         shortCommit,
		DownloadStatus: downloadStatus,
		Snapshots:      snapshots,
		Files:          files,
		Manifest:       manifest,
	}

	writeJSON(w, http.StatusOK, info)
}

// RebuildResponse represents the result of a cache rebuild operation.
type RebuildResponse struct {
	Success         bool     `json:"success"`
	ReposScanned    int      `json:"reposScanned"`
	SymlinksCreated int      `json:"symlinksCreated"`
	SymlinksUpdated int      `json:"symlinksUpdated"`
	OrphansRemoved  int      `json:"orphansRemoved,omitempty"`
	Errors          []string `json:"errors,omitempty"`
	Message         string   `json:"message,omitempty"`
}

// handleCacheRebuild regenerates the friendly view symlinks from the hub cache.
func (s *Server) handleCacheRebuild(w http.ResponseWriter, r *http.Request) {
	cacheDir := s.config.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	// Parse options from request body
	var req struct {
		Clean bool `json:"clean"` // Remove orphaned symlinks
	}
	json.NewDecoder(r.Body).Decode(&req) // Ignore errors, use defaults

	cache := hfdownloader.NewHFCache(cacheDir, hfdownloader.DefaultStaleTimeout)

	opts := hfdownloader.SyncOptions{
		Clean:   req.Clean,
		Verbose: false,
	}

	result, err := cache.Sync(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Rebuild failed", err.Error())
		return
	}

	resp := RebuildResponse{
		Success:         true,
		ReposScanned:    result.ReposScanned,
		SymlinksCreated: result.SymlinksCreated,
		SymlinksUpdated: result.SymlinksUpdated,
		OrphansRemoved:  result.OrphansRemoved,
	}

	for _, e := range result.Errors {
		resp.Errors = append(resp.Errors, e.Error())
	}

	if resp.SymlinksCreated == 0 && resp.SymlinksUpdated == 0 {
		resp.Message = "Friendly view is up to date"
	} else {
		resp.Message = fmt.Sprintf("Created %d symlinks, updated %d", resp.SymlinksCreated, resp.SymlinksUpdated)
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleCacheDelete deletes a repository from the cache.
// SECURITY: This endpoint requires extensive validation to prevent:
// - Path traversal attacks (../, encoded variants)
// - Symlink attacks (symlinks pointing outside cache)
// - TOCTOU race conditions
// - Directory escape via prefix manipulation
func (s *Server) handleCacheDelete(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if repo == "" {
		writeError(w, http.StatusBadRequest, "Missing repo path", "")
		return
	}

	// Security Layer 1: Validate repo format strictly (owner/name)
	if !hfdownloader.IsValidModelName(repo) {
		writeError(w, http.StatusBadRequest, "Invalid repository ID format", "Expected format: owner/name")
		return
	}

	// Security Layer 2: Check for path traversal attempts (multiple encodings)
	// Check raw string for obvious traversal patterns
	if strings.Contains(repo, "..") || strings.Contains(repo, "//") {
		writeError(w, http.StatusBadRequest, "Invalid repository ID", "Path traversal not allowed")
		return
	}

	// Security Layer 3: Check for backslashes (Windows path traversal)
	if strings.Contains(repo, "\\") {
		writeError(w, http.StatusBadRequest, "Invalid repository ID", "Backslashes not allowed")
		return
	}

	// Security Layer 4: Validate characters in owner/name are safe
	// Only allow alphanumeric, dash, underscore, and period (standard HF naming)
	parts := strings.SplitN(repo, "/", 2)
	if !isValidRepoComponent(parts[0]) || !isValidRepoComponent(parts[1]) {
		writeError(w, http.StatusBadRequest, "Invalid repository ID", "Invalid characters in repository name")
		return
	}

	// Determine type from query param
	repoTypeStr := r.URL.Query().Get("type")
	repoType := hfdownloader.RepoTypeModel
	if repoTypeStr == "dataset" {
		repoType = hfdownloader.RepoTypeDataset
	}

	cacheDir := s.config.CacheDir
	if cacheDir == "" {
		cacheDir = hfdownloader.DefaultCacheDir()
	}

	cache := hfdownloader.NewHFCache(cacheDir, hfdownloader.DefaultStaleTimeout)

	// Find the repo directory
	repoDir, err := cache.Repo(repo, repoType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid repository ID", err.Error())
		return
	}

	hubPath := repoDir.Path()
	friendlyPath := repoDir.FriendlyPath()

	// Security Layer 5: Resolve absolute paths
	absCacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to resolve cache path", err.Error())
		return
	}
	// Ensure cache dir ends with separator to prevent /cache/huggingface-evil matching /cache/huggingface
	absCacheDirWithSep := absCacheDir + string(filepath.Separator)

	absHubPath, err := filepath.Abs(hubPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to resolve path", err.Error())
		return
	}

	// Security Layer 6: Check if path exists and is not a symlink (TOCTOU mitigation)
	hubInfo, err := os.Lstat(absHubPath)
	if os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "Repository not found in cache", repo)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to check path", err.Error())
		return
	}

	// Security Layer 7: Reject if the hub path itself is a symlink (symlink attack prevention)
	if hubInfo.Mode()&os.ModeSymlink != 0 {
		writeError(w, http.StatusBadRequest, "Invalid path", "Cannot delete symlinked directories")
		return
	}

	// Security Layer 8: Verify path is within cache (using cleaned absolute path)
	if !strings.HasPrefix(absHubPath+string(filepath.Separator), absCacheDirWithSep) {
		writeError(w, http.StatusBadRequest, "Invalid path", "Path outside cache directory")
		return
	}

	// Security Layer 9: Verify path follows expected HF cache structure
	// Must be: {cacheDir}/hub/{models|datasets}--{owner}--{name}
	expectedPrefix := "models--"
	if repoType == hfdownloader.RepoTypeDataset {
		expectedPrefix = "datasets--"
	}
	hubSubpath, err := filepath.Rel(absCacheDir, absHubPath)
	if err != nil || !strings.HasPrefix(hubSubpath, filepath.Join("hub", expectedPrefix)) {
		writeError(w, http.StatusBadRequest, "Invalid path", "Path does not match expected cache structure")
		return
	}

	// Security Layer 10: Resolve symlinks to verify final destination is also within cache
	// This catches symlinks inside the directory structure
	realHubPath, err := filepath.EvalSymlinks(absHubPath)
	if err == nil && realHubPath != absHubPath {
		// Path contained symlinks - verify resolved path is still within cache
		if !strings.HasPrefix(realHubPath+string(filepath.Separator), absCacheDirWithSep) {
			writeError(w, http.StatusBadRequest, "Invalid path", "Resolved path outside cache directory")
			return
		}
	}

	// All security checks passed - proceed with deletion
	if err := os.RemoveAll(absHubPath); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete cache", err.Error())
		return
	}

	// Delete the friendly view directory (symlinks) with same security checks
	if friendlyPath != "" {
		if err := safeDeleteFriendlyPath(friendlyPath, absCacheDirWithSep); err != nil {
			// Log but don't fail - hub directory was successfully deleted
			// The friendly path might not exist or might be invalid
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Deleted %s from cache", repo),
	})
}

// isValidRepoComponent checks if a repository owner or name contains only safe characters.
// Allows: alphanumeric, dash (-), underscore (_), and period (.)
// This matches HuggingFace's naming conventions.
func isValidRepoComponent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	// Additional check: component must not be "." or ".."
	if s == "." || s == ".." {
		return false
	}
	return true
}

// safeDeleteFriendlyPath safely deletes the friendly view path with security checks.
func safeDeleteFriendlyPath(friendlyPath, absCacheDirWithSep string) error {
	absFriendlyPath, err := filepath.Abs(friendlyPath)
	if err != nil {
		return err
	}

	// Check it's within cache
	if !strings.HasPrefix(absFriendlyPath+string(filepath.Separator), absCacheDirWithSep) {
		return fmt.Errorf("friendly path outside cache")
	}

	// Check it's not a symlink at the top level
	info, err := os.Lstat(absFriendlyPath)
	if err != nil {
		return err // Doesn't exist, that's fine
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("friendly path is a symlink")
	}

	// Resolve symlinks and verify again
	realPath, err := filepath.EvalSymlinks(absFriendlyPath)
	if err == nil && realPath != absFriendlyPath {
		if !strings.HasPrefix(realPath+string(filepath.Separator), absCacheDirWithSep) {
			return fmt.Errorf("resolved friendly path outside cache")
		}
	}

	return os.RemoveAll(absFriendlyPath)
}

// parseCommandFilters extracts filter information from a manifest command string.
// Returns isFiltered (bool) and the filter string.
func parseCommandFilters(command string) (bool, string) {
	if command == "" {
		return false, ""
	}

	// Look for filter flags: -f, -F, --filters
	// Format examples:
	//   -f "q4_k_m,q5_k_m"
	//   -F q4_k_m
	//   --filters "q4_k_m"
	filters := ""
	isFiltered := false

	// Split command into parts (respecting quotes)
	parts := splitCommand(command)

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if part == "-f" || part == "-F" || part == "--filters" || part == "--include" {
			isFiltered = true
			// Next part is the filter value
			if i+1 < len(parts) {
				filters = parts[i+1]
				i++
			}
		} else if strings.HasPrefix(part, "-f=") || strings.HasPrefix(part, "-F=") || strings.HasPrefix(part, "--filters=") || strings.HasPrefix(part, "--include=") {
			isFiltered = true
			// Filter value is after =
			idx := strings.Index(part, "=")
			if idx != -1 {
				filters = part[idx+1:]
			}
		}
	}

	return isFiltered, filters
}

// splitCommand splits a command string into parts, respecting quoted strings.
func splitCommand(command string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range command {
		switch {
		case (r == '"' || r == '\'') && !inQuote:
			inQuote = true
			quoteChar = r
		case r == quoteChar && inQuote:
			inQuote = false
			quoteChar = 0
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// humanSizeBytes converts bytes to human-readable format.
func humanSizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
