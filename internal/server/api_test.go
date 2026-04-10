// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testCacheDir string

func newTestServer() *Server {
	if testCacheDir == "" {
		testCacheDir = "/tmp/hfdownloader_test_cache"
	}
	cfg := Config{
		Addr:        "127.0.0.1",
		Port:        0, // Random port
		CacheDir:    testCacheDir,
		Concurrency: 2,
		MaxActive:   1,
	}
	return New(cfg)
}

func TestAPI_Health(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "ok" {
		t.Errorf("Expected status ok, got %v", resp["status"])
	}
	if resp["version"] != "2.3.3" {
		t.Errorf("Expected version 2.3.3, got %v", resp["version"])
	}
}

func TestAPI_GetSettings(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()

	srv.handleGetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp SettingsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.CacheDir != testCacheDir {
		t.Errorf("Expected cacheDir %s, got %s", testCacheDir, resp.CacheDir)
	}
}

func TestAPI_GetSettings_TokenMasked(t *testing.T) {
	cfg := Config{
		CacheDir: "/tmp/test_cache",
		Token:    "hf_abcdefghijklmnop",
	}
	srv := New(cfg)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()

	srv.handleGetSettings(w, req)

	var resp SettingsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Token should be masked, not exposed
	if resp.Token == "hf_abcdefghijklmnop" {
		t.Error("Token should be masked, not exposed in full")
	}
	if resp.Token != "********mnop" {
		t.Errorf("Expected masked token ********mnop, got %s", resp.Token)
	}
}

func TestAPI_UpdateSettings(t *testing.T) {
	srv := newTestServer()

	// Update concurrency
	body := `{"connections": 16, "maxActive": 8}`
	req := httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleUpdateSettings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Verify changes applied
	if srv.config.Concurrency != 16 {
		t.Errorf("Expected concurrency 16, got %d", srv.config.Concurrency)
	}
	if srv.config.MaxActive != 8 {
		t.Errorf("Expected maxActive 8, got %d", srv.config.MaxActive)
	}
}

func TestAPI_UpdateSettings_CantChangeCacheDir(t *testing.T) {
	srv := newTestServer()
	originalCache := srv.config.CacheDir

	// Try to inject a different cache path (should be ignored)
	body := `{"cacheDir": "/etc/passwd"}`
	req := httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleUpdateSettings(w, req)

	// Path should NOT have changed
	if srv.config.CacheDir != originalCache {
		t.Errorf("CacheDir should not be changeable via API! Got %s", srv.config.CacheDir)
	}
}

func TestAPI_StartDownload_ValidatesRepo(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "missing repo",
			body:     `{}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid repo format",
			body:     `{"repo": "invalid"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "valid repo",
			body:     `{"repo": "owner/name"}`,
			wantCode: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.handleStartDownload(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Expected %d, got %d. Body: %s", tt.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestAPI_StartDownload_OutputIgnored(t *testing.T) {
	srv := newTestServer()

	// Try to specify custom output path
	body := `{"repo": "test/model", "output": "/etc/evil"}`
	req := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleStartDownload(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d", w.Code)
	}

	var resp Job
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Output should be server-controlled (HF cache), not from request
	if resp.OutputDir == "/etc/evil" {
		t.Error("Output path from request should be ignored!")
	}
	if resp.OutputDir != testCacheDir {
		t.Errorf("Expected server-controlled HF cache output, got %s", resp.OutputDir)
	}
}

func TestAPI_StartDownload_DatasetUsesSameCacheDir(t *testing.T) {
	srv := newTestServer()

	body := `{"repo": "test/dataset", "dataset": true}`
	req := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleStartDownload(w, req)

	var resp Job
	json.Unmarshal(w.Body.Bytes(), &resp)

	// In v3, both models and datasets use the same HF cache directory
	if resp.OutputDir != testCacheDir {
		t.Errorf("Dataset should use HF cache dir, got %s", resp.OutputDir)
	}
}

func TestAPI_StartDownload_DuplicateReturnsExisting(t *testing.T) {
	srv := newTestServer()

	body := `{"repo": "dup/test"}`

	// First request
	req1 := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	srv.handleStartDownload(w1, req1)

	if w1.Code != http.StatusAccepted {
		t.Fatalf("First request should return 202, got %d", w1.Code)
	}

	var job1 Job
	json.Unmarshal(w1.Body.Bytes(), &job1)

	// Second request (duplicate)
	req2 := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handleStartDownload(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Duplicate request should return 200, got %d", w2.Code)
	}

	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)

	if resp["message"] != "Download already in progress" {
		t.Errorf("Expected duplicate message, got %v", resp["message"])
	}

	jobMap := resp["job"].(map[string]any)
	if jobMap["id"] != job1.ID {
		t.Error("Duplicate should return same job ID")
	}
}

func TestAPI_ListJobs(t *testing.T) {
	srv := newTestServer()

	// Create a job first
	body := `{"repo": "list/test"}`
	req := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleStartDownload(w, req)

	// List jobs
	listReq := httptest.NewRequest("GET", "/api/jobs", nil)
	listW := httptest.NewRecorder()
	srv.handleListJobs(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", listW.Code)
	}

	var resp map[string]any
	json.Unmarshal(listW.Body.Bytes(), &resp)

	count := int(resp["count"].(float64))
	if count < 1 {
		t.Error("Expected at least 1 job")
	}
}

func TestAPI_ParseFiltersFromRepo(t *testing.T) {
	srv := newTestServer()

	body := `{"repo": "owner/model:q4_0,q5_0"}`
	req := httptest.NewRequest("POST", "/api/download", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleStartDownload(w, req)

	var resp Job
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Repo != "owner/model" {
		t.Errorf("Repo should be parsed without filters, got %s", resp.Repo)
	}
	if len(resp.Filters) != 2 {
		t.Errorf("Expected 2 filters, got %d", len(resp.Filters))
	}
}

// --- Delete Cache Security Tests ---

func TestAPI_CacheDelete_PathTraversal(t *testing.T) {
	srv := newTestServer()

	// Test various path traversal attempts
	tests := []struct {
		name     string
		repo     string
		wantCode int
	}{
		{
			name:     "direct path traversal",
			repo:     "../../../etc/passwd",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "double dot in owner",
			repo:     "../passwd/file",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "double slash",
			repo:     "owner//name",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "backslash traversal",
			repo:     "owner\\..\\etc",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "just dots owner",
			repo:     "../name",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "just dots name",
			repo:     "owner/..",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "single dot owner",
			repo:     "./name",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "single dot name",
			repo:     "owner/.",
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/api/cache/"+tt.repo, nil)
			parts := strings.SplitN(tt.repo, "/", 2)
			req.SetPathValue("owner", parts[0])
			if len(parts) == 2 {
				req.SetPathValue("name", parts[1])
			}
			w := httptest.NewRecorder()

			srv.handleCacheDelete(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Expected %d for %q, got %d. Body: %s",
					tt.wantCode, tt.repo, w.Code, w.Body.String())
			}
		})
	}
}

func TestAPI_CacheDelete_InvalidCharacters(t *testing.T) {
	srv := newTestServer()

	// Test invalid characters that could be used in attacks
	// Note: Some characters (null byte, control chars) are rejected by the HTTP layer itself
	// and cannot reach our handler, so we only test what can actually arrive.
	tests := []struct {
		name     string
		repo     string
		wantCode int
	}{
		{
			name:     "shell metacharacter semicolon",
			repo:     "owner/name;rm",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "shell metacharacter pipe",
			repo:     "owner/name|cat",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "shell metacharacter backtick",
			repo:     "owner/`whoami`",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "dollar sign",
			repo:     "owner/$HOME",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "colon",
			repo:     "owner/name:evil",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "asterisk",
			repo:     "owner/name*",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "ampersand",
			repo:     "owner/name&cmd",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "space",
			repo:     "owner/name evil",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "at sign",
			repo:     "owner/@evil",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "hash",
			repo:     "owner/#evil",
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/api/cache/test/repo", nil)
			parts := strings.SplitN(tt.repo, "/", 2)
			req.SetPathValue("owner", parts[0])
			if len(parts) == 2 {
				req.SetPathValue("name", parts[1])
			}
			w := httptest.NewRecorder()

			srv.handleCacheDelete(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Expected %d for %q, got %d. Body: %s",
					tt.wantCode, tt.repo, w.Code, w.Body.String())
			}
		})
	}
}

func TestAPI_CacheDelete_ValidRepoFormat(t *testing.T) {
	// Use a real temp directory (avoids /tmp -> /private/tmp symlink issues on macOS)
	tempDir := t.TempDir()
	cfg := Config{
		Addr:        "127.0.0.1",
		Port:        0,
		CacheDir:    tempDir,
		Concurrency: 2,
		MaxActive:   1,
	}
	srv := New(cfg)

	// Valid format repos should pass validation (may return 404 if not found)
	tests := []struct {
		name     string
		repo     string
		wantCode int // 404 is OK - it means validation passed
	}{
		{
			name:     "simple valid repo",
			repo:     "owner/name",
			wantCode: http.StatusNotFound, // Passes validation, not found in cache
		},
		{
			name:     "repo with dash",
			repo:     "the-owner/model-name",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "repo with underscore",
			repo:     "my_owner/my_model",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "repo with numbers",
			repo:     "owner123/model456",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "repo with period",
			repo:     "owner.org/model.v1",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "mixed case",
			repo:     "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/api/cache/"+tt.repo, nil)
			parts := strings.SplitN(tt.repo, "/", 2)
			req.SetPathValue("owner", parts[0])
			if len(parts) == 2 {
				req.SetPathValue("name", parts[1])
			}
			w := httptest.NewRecorder()

			srv.handleCacheDelete(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Expected %d for %q, got %d. Body: %s",
					tt.wantCode, tt.repo, w.Code, w.Body.String())
			}
		})
	}
}

// --- Update Check Tests ---

func TestAPI_CacheUpdates_PathTraversal(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		name     string
		owner    string
		repoName string
		wantCode int
	}{
		{"dot dot owner", "..", "model", http.StatusBadRequest},
		{"dot dot name", "owner", "..", http.StatusBadRequest},
		{"valid but missing", "notexist", "notexist", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/cache/"+tt.owner+"/"+tt.repoName+"/updates", nil)
			req.SetPathValue("owner", tt.owner)
			req.SetPathValue("name", tt.repoName)
			w := httptest.NewRecorder()

			srv.handleCacheUpdates(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Expected %d, got %d. Body: %s", tt.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestAPI_CacheUpdates_InvalidRepoID(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		name     string
		owner    string
		repoName string
	}{
		{"shell metachar in owner", "own;er", "model"},
		{"dollar sign in name", "owner", "model$evil"},
		{"asterisk in name", "owner", "name*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/cache/test/test/updates", nil)
			req.SetPathValue("owner", tt.owner)
			req.SetPathValue("name", tt.repoName)
			w := httptest.NewRecorder()

			srv.handleCacheUpdates(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAPI_DownloadRequest_PathsField(t *testing.T) {
	srv := newTestServer()

	body := `{"repo": "owner/model", "paths": ["config.json", "model.Q4_K_M.gguf"]}`
	req := httptest.NewRequest("POST", "/api/download", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleStartDownload(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Expected 202, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp Job
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Paths) != 2 {
		t.Errorf("Expected 2 paths, got %d: %v", len(resp.Paths), resp.Paths)
	}
	if resp.Paths[0] != "config.json" {
		t.Errorf("Expected paths[0]='config.json', got %q", resp.Paths[0])
	}
	if resp.Paths[1] != "model.Q4_K_M.gguf" {
		t.Errorf("Expected paths[1]='model.Q4_K_M.gguf', got %q", resp.Paths[1])
	}
}

func TestJobManager_PathsDeduplication(t *testing.T) {
	tempDir := t.TempDir()
	cfg := Config{CacheDir: tempDir, Concurrency: 1, MaxActive: 1}
	mgr := NewJobManager(cfg, nil)

	// Full-repo job should deduplicate against another full-repo job
	req1 := DownloadRequest{Repo: "owner/model", Revision: "main"}
	job1, wasExisting1, err := mgr.CreateJob(req1)
	if err != nil || wasExisting1 {
		t.Fatalf("unexpected: err=%v wasExisting=%v", err, wasExisting1)
	}

	job1b, wasExisting1b, _ := mgr.CreateJob(req1)
	if !wasExisting1b || job1b.ID != job1.ID {
		t.Error("identical full-repo jobs should be deduplicated")
	}

	// Paths-based job should NOT deduplicate against the full-repo job
	req2 := DownloadRequest{Repo: "owner/model", Revision: "main", Paths: []string{"config.json"}}
	job2, wasExisting2, err := mgr.CreateJob(req2)
	if err != nil {
		t.Fatalf("CreateJob with paths: %v", err)
	}
	if wasExisting2 {
		t.Error("paths-based job should not be deduplicated against full-repo job")
	}
	if job2.ID == job1.ID {
		t.Error("paths-based job should have a distinct ID from full-repo job")
	}

	// Same paths-based job submitted twice should deduplicate
	job2b, wasExisting2b, _ := mgr.CreateJob(req2)
	if !wasExisting2b || job2b.ID != job2.ID {
		t.Error("identical paths-based jobs should be deduplicated")
	}

	// Different paths should produce a distinct job
	req3 := DownloadRequest{Repo: "owner/model", Revision: "main", Paths: []string{"model.gguf"}}
	job3, wasExisting3, _ := mgr.CreateJob(req3)
	if wasExisting3 || job3.ID == job2.ID {
		t.Error("different paths should produce a distinct job")
	}
}

func TestIsValidRepoComponent(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid
		{"owner", true},
		{"my-org", true},
		{"my_org", true},
		{"MyOrg123", true},
		{"model.v1", true},
		{"a", true},
		{"1", true},
		{"a-b_c.d", true},

		// Invalid - special components
		{"", false},
		{".", false},
		{"..", false},

		// Invalid - dangerous characters
		{"/", false},
		{"\\", false},
		{";", false},
		{"|", false},
		{"$", false},
		{"`", false},
		{"'", false},
		{"\"", false},
		{" ", false},
		{"\n", false},
		{"\t", false},
		{"\x00", false},
		{"*", false},
		{"?", false},
		{"<", false},
		{">", false},
		{":", false},
		{"&", false},
		{"!", false},
		{"(", false},
		{")", false},
		{"[", false},
		{"]", false},
		{"{", false},
		{"}", false},
		{"@", false},
		{"#", false},
		{"%", false},
		{"^", false},
		{"=", false},
		{"+", false},
		{"~", false},

		// Invalid - mixed valid/invalid
		{"owner;evil", false},
		{"owner|evil", false},
		{"name$var", false},
		{"../passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidRepoComponent(tt.input)
			if got != tt.want {
				t.Errorf("isValidRepoComponent(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

