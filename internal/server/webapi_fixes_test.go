// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateSettings_RejectsInvalidValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, body := range []string{
		`{"verify":"bogus"}`,
		`{"multipartThreshold":"lots"}`,
		`{"connections":100000}`,
		`{"endpoint":"file:///etc"}`,
		`{"retries":-3}`,
	} {
		srv := newTestServer()
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, w.Code)
		}
	}
	srv := newTestServer()
	w := httptest.NewRecorder()
	srv.handleUpdateSettings(w, httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(`{"multipartThreshold":"64MiB","verify":"sha256","endpoint":"https://hf-mirror.com"}`)))
	if w.Code != http.StatusOK {
		t.Errorf("valid settings rejected: %d %s", w.Code, w.Body)
	}
}

// A second request for the same repo with different filters is a different
// download; it used to be merged into the first and its filters dropped.
func TestCreateJob_DedupConsidersFilters(t *testing.T) {
	m := NewJobManager(DefaultConfig(), nil)
	m.runFn = func(*Job) { select {} } // keep jobs active
	a, dupA, _ := m.CreateJob(DownloadRequest{Repo: "o/r", Filters: []string{"q4_k_m"}})
	b, dupB, _ := m.CreateJob(DownloadRequest{Repo: "o/r", Filters: []string{"q8_0"}})
	c, dupC, _ := m.CreateJob(DownloadRequest{Repo: "o/r", Filters: []string{"q4_k_m"}})
	if dupA || dupB || a.ID == b.ID {
		t.Errorf("different filters deduplicated: %v %v", dupB, a.ID == b.ID)
	}
	if !dupC || c.ID != a.ID {
		t.Errorf("identical request not deduplicated")
	}
	if !m.HasActiveJob("o/r", false) || m.HasActiveJob("o/other", false) {
		t.Error("HasActiveJob wrong")
	}
}

func TestCacheDelete_RefusedWhileDownloading(t *testing.T) {
	srv := newTestServer()
	srv.jobs.runFn = func(*Job) { select {} }
	srv.jobs.CreateJob(DownloadRequest{Repo: "o/r"})
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/cache/o/r", nil))
	if w.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", w.Code)
	}
}

func TestRequestBodyLimit(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	h := limitBodyMiddleware(mux)
	big := `{"repo":"o/r","filters":["` + strings.Repeat("x", 2<<20) + `"]}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/plan", strings.NewReader(big)))
	if w.Code < 400 || w.Code >= 500 {
		t.Errorf("2 MiB body: status %d, want a 4xx", w.Code)
	}
}

func TestJobCommand(t *testing.T) {
	j := &Job{Repo: "o/r", Revision: "v2", IsDataset: true, Filters: []string{"q4_k_m", "mmproj-f16"}, ExactMatch: true, Excludes: []string{".md"}}
	want := "hfdownloader download o/r --dataset -b v2 -F q4_k_m,mmproj-f16 --exact -E .md"
	if got := jobCommand(j); got != want {
		t.Errorf("jobCommand = %q, want %q", got, want)
	}
	if got := jobCommand(&Job{Repo: "o/r", Revision: "main"}); got != "hfdownloader download o/r" {
		t.Errorf("plain job command = %q", got)
	}
}

// On case-insensitive filesystems "BARTOWSKI/x" is the same cache dir.
func TestCacheDelete_GuardIgnoresCase(t *testing.T) {
	srv := newTestServer()
	srv.jobs.runFn = func(*Job) { select {} }
	srv.jobs.CreateJob(DownloadRequest{Repo: "bartowski/SmolLM2"})
	if !srv.jobs.HasActiveJob("BARTOWSKI/smollm2", false) {
		t.Error("case-changed repo id bypassed the active-download guard")
	}
}

func TestUpdateSettings_RejectsBadProxy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, body := range []string{`{"proxy":{"url":"bogus::"}}`, `{"proxy":{"url":"ftp://x:1"}}`} {
		srv := newTestServer()
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, w.Code)
		}
	}
}
