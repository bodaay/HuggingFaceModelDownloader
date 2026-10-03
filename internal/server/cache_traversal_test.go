// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCacheRoutes_RejectTraversal: GET /api/cache/{repo...} used to accept
// "x/..%2F..%2F<dir>", stat paths outside the cache and return file names,
// sizes and refs from them. Every cache route must reject such repo IDs.
func TestCacheRoutes_RejectTraversal(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	// A directory outside the cache that looks like a repo, as the exploit
	// needed: <root>/victim/{refs/main,snapshots/<rev>/secret.txt}.
	victim := filepath.Join(root, "victim")
	os.MkdirAll(filepath.Join(victim, "refs"), 0o755)
	os.MkdirAll(filepath.Join(victim, "snapshots", "rev"), 0o755)
	os.WriteFile(filepath.Join(victim, "refs", "main"), []byte("SECRETREF"), 0o644)
	os.WriteFile(filepath.Join(victim, "snapshots", "rev", "secret.txt"), []byte("x"), 0o644)

	cfg := DefaultConfig()
	cfg.CacheDir = cacheDir
	s := New(cfg)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	// <cache>/hub/models--x--../../../../victim cleans to <root>/victim: the
	// first ".." cancels the bogus "models--x--.." element, the next three
	// climb out of hub/ and cache/. Cover a range of depths and encodings.
	var paths []string
	for depth := 2; depth <= 6; depth++ {
		up := strings.Repeat("..%2F", depth)
		paths = append(paths,
			"/api/cache/x/"+up+"victim",
			"/api/cache/x/"+strings.ReplaceAll(up, "..", "%2E%2E")+"victim",
		)
	}
	paths = append(paths, "/api/cache/..%2F..%2F..%2Fvictim/x")
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		for _, p := range paths {
			req := httptest.NewRequest(method, p, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			body := w.Body.String()
			if w.Code == http.StatusOK {
				t.Errorf("%s %s = 200, want rejection; body: %s", method, p, body)
			}
			if strings.Contains(body, "secret.txt") || strings.Contains(body, "SECRET") {
				t.Errorf("%s %s leaked data from outside the cache: %s", method, p, body)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(victim, "snapshots", "rev", "secret.txt")); err != nil {
		t.Errorf("file outside the cache was removed: %v", err)
	}
}
