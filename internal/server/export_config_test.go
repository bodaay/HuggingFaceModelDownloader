// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Saving settings from the web UI used to rewrite the whole config file,
// dropping cache-dir, backoff, link-mode and every other CLI key.
func TestUpdateSettings_KeepsOtherConfigKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := ConfigPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"cache-dir": "/data/hf", "backoff-initial": "1s", "link-mode": "hardlink", "connections": 4}`), 0o644)

	srv := newTestServer()
	req := httptest.NewRequest("POST", "/api/settings", bytes.NewBufferString(`{"connections": 12}`))
	w := httptest.NewRecorder()
	srv.handleUpdateSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	var got map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{"cache-dir": "/data/hf", "backoff-initial": "1s", "link-mode": "hardlink", "connections": 12.0} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v (file: %s)", k, got[k], want, data)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, want 0600 (may contain the HF token)", fi.Mode().Perm())
	}
}

func TestCacheExport_DisabledWithoutExportDir(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/cache/export", strings.NewReader(`{"repo":"o/r"}`)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "--export-dir") {
		t.Errorf("status %d body %s; want 400 explaining --export-dir", w.Code, w.Body)
	}
}

func TestCacheExport_WritesUnderExportDir(t *testing.T) {
	cacheDir := t.TempDir()
	exportDir := t.TempDir()
	// Minimal cache: blob + snapshot symlink + ref.
	repo := filepath.Join(cacheDir, "hub", "models--o--r")
	os.MkdirAll(filepath.Join(repo, "blobs"), 0o755)
	os.MkdirAll(filepath.Join(repo, "snapshots", "c0ffee"), 0o755)
	os.MkdirAll(filepath.Join(repo, "refs"), 0o755)
	os.WriteFile(filepath.Join(repo, "blobs", "abc"), []byte("weights"), 0o644)
	os.Symlink("../../blobs/abc", filepath.Join(repo, "snapshots", "c0ffee", "model.gguf"))
	os.WriteFile(filepath.Join(repo, "refs", "main"), []byte("c0ffee"), 0o644)

	cfg := DefaultConfig()
	cfg.CacheDir = cacheDir
	cfg.ExportDir = exportDir
	srv := New(cfg)
	mux := http.NewServeMux()
	srv.registerAPIRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/cache/export", strings.NewReader(`{"repo":"o/r"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	out := filepath.Join(exportDir, "o", "r", "model.gguf")
	if b, err := os.ReadFile(out); err != nil || string(b) != "weights" {
		t.Errorf("exported file: %q, %v", b, err)
	}
	if fi, _ := os.Lstat(out); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("exported a symlink, want a real file")
	}

	// Traversal in the repo id must not escape the export dir.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/cache/export", strings.NewReader(`{"repo":"o/../../x"}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("traversal repo id: status %d", w.Code)
	}
}
