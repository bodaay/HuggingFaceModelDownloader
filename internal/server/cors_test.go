// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCORSMiddleware: requests from other origins must be rejected before the
// handler runs. Previously, with no AllowedOrigins configured, any origin was
// echoed back as allowed, so any website could read and change settings, start
// downloads or delete the cache through a locally running server.
func TestCORSMiddleware(t *testing.T) {
	const host = "localhost:8080"
	cases := []struct {
		name        string
		allowed     []string
		method      string
		origin      string
		wantStatus  int
		wantHandler bool
		wantACAO    string
	}{
		{"no origin (curl)", nil, http.MethodPost, "", http.StatusOK, true, ""},
		{"same origin GET", nil, http.MethodGet, "http://" + host, http.StatusOK, true, "http://" + host},
		{"same origin POST", nil, http.MethodPost, "http://" + host, http.StatusOK, true, "http://" + host},
		{"foreign GET", nil, http.MethodGet, "http://evil.example", http.StatusForbidden, false, ""},
		{"foreign POST", nil, http.MethodPost, "http://evil.example", http.StatusForbidden, false, ""},
		{"foreign DELETE", nil, http.MethodDelete, "http://evil.example", http.StatusForbidden, false, ""},
		{"foreign preflight", nil, http.MethodOptions, "http://evil.example", http.StatusForbidden, false, ""},
		{"null origin", nil, http.MethodPost, "null", http.StatusForbidden, false, ""},
		{"other local port", nil, http.MethodPost, "http://localhost:3000", http.StatusForbidden, false, ""},
		{"allowlisted POST", []string{"https://hfd.example.com"}, http.MethodPost, "https://hfd.example.com", http.StatusOK, true, "https://hfd.example.com"},
		{"allowlisted preflight", []string{"https://hfd.example.com"}, http.MethodOptions, "https://hfd.example.com", http.StatusNoContent, false, "https://hfd.example.com"},
		{"allowlist miss", []string{"https://hfd.example.com"}, http.MethodPost, "http://evil.example", http.StatusForbidden, false, ""},
		{"wildcard", []string{"*"}, http.MethodPost, "http://anything.example", http.StatusOK, true, "http://anything.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.AllowedOrigins = tc.allowed
			s := New(cfg)

			called := false
			h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			}))
			r := httptest.NewRequest(tc.method, "http://"+host+"/api/settings", nil)
			r.Host = host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if called != tc.wantHandler {
				t.Errorf("handler called = %v, want %v", called, tc.wantHandler)
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tc.wantACAO {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tc.wantACAO)
			}
		})
	}
}
