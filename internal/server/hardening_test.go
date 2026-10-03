// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWSHubConcurrentAccessRace stresses the hub's three map-touching paths
// (register, broadcast-with-eviction, unregister) against concurrent
// ClientCount() readers. Before the fix the broadcast loop deleted from
// h.clients while holding only RLock and read len(h.clients) outside the lock,
// which `go test -race` flags as a concurrent map read/write (and can panic in
// production). With the broadcast loop under the write lock and counts read
// under the lock, this must run clean.
func TestWSHubConcurrentAccessRace(t *testing.T) {
	hub := NewWSHub()
	go hub.Run()

	// Register clients with tiny, undrained send buffers so the very next
	// broadcast overflows them and forces the eviction path.
	for i := 0; i < 40; i++ {
		hub.register <- &WSClient{send: make(chan []byte, 1), hub: hub}
	}

	var wg sync.WaitGroup

	// Broadcasters + counters running together.
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				hub.Broadcast("progress", map[string]int{"n": i})
				_ = hub.ClientCount()
			}
		}()
	}

	// Continuous register/unregister churn to keep eviction-eligible clients
	// flowing through the map while counts are read.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 150; i++ {
				c := &WSClient{send: make(chan []byte, 1), hub: hub}
				hub.register <- c
				hub.unregister <- c
			}
		}()
	}

	wg.Wait()
}

// TestWSClientCloseSendOnce verifies the send channel is closed at most once
// even when both the eviction path and the unregister path target the same
// client — a double close would panic.
func TestWSClientCloseSendOnce(t *testing.T) {
	c := &WSClient{send: make(chan []byte, 1)}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.closeSend()
		}()
	}
	wg.Wait()

	// Channel must be closed exactly once (reading yields the zero value with
	// ok=false); a second close inside closeSend would have panicked above.
	if _, ok := <-c.send; ok {
		t.Fatalf("send channel should be closed")
	}
}

// TestIsAllowedWSOrigin pins the cross-site WebSocket hijacking defense: only
// same-origin requests, explicit allowlist entries, and originless (native)
// clients may connect.
func TestIsAllowedWSOrigin(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		host    string
		origin  string
		want    bool
	}{
		{"no origin (native client)", nil, "localhost:8080", "", true},
		{"same origin", nil, "localhost:8080", "http://localhost:8080", true},
		{"cross origin denied by default", nil, "localhost:8080", "http://evil.example", false},
		{"cross origin allowlisted", []string{"http://evil.example"}, "localhost:8080", "http://evil.example", true},
		{"wildcard allows all", []string{"*"}, "localhost:8080", "http://evil.example", true},
		{"different port is cross origin", nil, "localhost:8080", "http://localhost:9999", false},
		{"allowlist miss still denied", []string{"http://ok.example"}, "localhost:8080", "http://evil.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.AllowedOrigins = tc.allowed
			s := New(cfg)

			r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/api/ws", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := s.isAllowedOrigin(r); got != tc.want {
				t.Errorf("isAllowedOrigin(host=%q, origin=%q) = %v, want %v",
					tc.host, tc.origin, got, tc.want)
			}
		})
	}
}

// TestSettingsUpdateNoRace runs concurrent settings writers and readers through
// the real handlers. Before the fix, handleUpdateSettings mutated s.config (and
// the shared *ProxyConfig) and did `s.jobs.config = s.config` with no lock,
// racing handleGetSettings and the job manager's config reads. HOME is
// redirected so the persisted config file lands in a temp dir, not the user's.
func TestSettingsUpdateNoRace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := New(DefaultConfig())

	var wg sync.WaitGroup

	// Writers
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				body := fmt.Sprintf(
					`{"connections":%d,"verify":"sha256","proxy":{"url":"http://p%d-%d:8080","username":"u"}}`,
					(i%8)+1, g, i)
				req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
				s.handleUpdateSettings(httptest.NewRecorder(), req)
			}
		}(g)
	}

	// Readers (handler + the snapshot helpers the job manager uses)
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				s.handleGetSettings(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/settings", nil))
				_ = s.settingsConfig()
				_ = s.jobs.snapshotConfig()
			}
		}()
	}

	wg.Wait()
}

// TestBasicAuthMiddleware locks the auth behavior unchanged by the move to a
// constant-time comparison: right creds pass, anything else is 401.
func TestBasicAuthMiddleware(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthUser = "admin"
	cfg.AuthPass = "s3cret"
	s := New(cfg)

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := s.basicAuthMiddleware(ok)

	cases := []struct {
		name       string
		setCreds   bool
		user, pass string
		want       int
	}{
		{"no creds", false, "", "", http.StatusUnauthorized},
		{"wrong user", true, "root", "s3cret", http.StatusUnauthorized},
		{"wrong pass", true, "admin", "nope", http.StatusUnauthorized},
		{"correct", true, "admin", "s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
			if tc.setCreds {
				req.SetBasicAuth(tc.user, tc.pass)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestJobsRunSerially verifies the fix for issue #85: regardless of how many
// models are submitted at once, only one downloads at a time and the rest run
// in FIFO order. It substitutes a fake runner (m.runFn) so the scheduler is
// exercised without touching the network.
func TestJobsRunSerially(t *testing.T) {
	m := NewJobManager(DefaultConfig(), nil)

	var mu sync.Mutex
	cur, maxCur := 0, 0
	var order []string

	started := make(chan string, 16)
	proceed := make(chan struct{})

	m.runFn = func(j *Job) {
		mu.Lock()
		cur++
		if cur > maxCur {
			maxCur = cur
		}
		order = append(order, j.Repo)
		mu.Unlock()

		started <- j.Repo
		<-proceed // hold the slot until the test lets this job finish

		mu.Lock()
		cur--
		mu.Unlock()
	}

	const n = 5
	for i := 0; i < n; i++ {
		if _, _, err := m.CreateJob(DownloadRequest{Repo: "owner/r" + string(rune('0'+i))}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
	}

	for i := 0; i < n; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("job %d never started — serial scheduler stalled", i)
		}
		// While this job holds the slot, no other job may have started.
		select {
		case extra := <-started:
			t.Fatalf("a second job (%s) started concurrently — not serial", extra)
		case <-time.After(40 * time.Millisecond):
		}
		proceed <- struct{}{} // finish the running job → next should start
	}

	if !m.WaitAll(2 * time.Second) {
		t.Fatal("not all queued jobs completed")
	}

	mu.Lock()
	defer mu.Unlock()
	if maxCur != 1 {
		t.Errorf("max concurrent jobs = %d, want 1 (serial execution)", maxCur)
	}
	if len(order) != n {
		t.Fatalf("ran %d jobs, want %d", len(order), n)
	}
	for i := 0; i < n; i++ {
		want := "owner/r" + string(rune('0'+i))
		if order[i] != want {
			t.Errorf("run order[%d] = %s, want %s (FIFO)", i, order[i], want)
		}
	}
}

// TestHealthReportsConfiguredVersion confirms the health endpoint reports the
// injected build version (and falls back to "dev" when unset) rather than a
// stamp hardcoded in source.
func TestHealthReportsConfiguredVersion(t *testing.T) {
	t.Run("configured version", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Version = "9.9.9"
		s := New(cfg)
		rec := httptest.NewRecorder()
		s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if !strings.Contains(rec.Body.String(), `"version":"9.9.9"`) {
			t.Errorf("health body = %s, want version 9.9.9", rec.Body.String())
		}
	})
	t.Run("unset falls back to dev", func(t *testing.T) {
		s := New(DefaultConfig())
		rec := httptest.NewRecorder()
		s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if !strings.Contains(rec.Body.String(), `"version":"dev"`) {
			t.Errorf("health body = %s, want version dev", rec.Body.String())
		}
	})
}

// TestJobCoalescerStopCancelsPending verifies stop() halts a queued flush timer
// and rejects subsequent schedules, so no broadcast fires after shutdown.
func TestJobCoalescerStopCancelsPending(t *testing.T) {
	sink := &recordingSink{}
	c := newJobCoalescer(100*time.Millisecond, sink.send)

	// First event sends immediately; second is queued behind the min-gap.
	c.schedule(&Job{ID: "j1", Status: JobStatusRunning})
	c.schedule(&Job{ID: "j1", Status: JobStatusRunning, Progress: JobProgress{DownloadedBytes: 7}})

	c.stop()
	time.Sleep(200 * time.Millisecond) // the queued flush would have fired by now

	if n := len(sink.snapshot()); n != 1 {
		t.Fatalf("after stop got %d sends, want 1 (queued flush must be cancelled)", n)
	}

	// Schedules after stop are no-ops.
	c.schedule(&Job{ID: "j2", Status: JobStatusRunning})
	if n := len(sink.snapshot()); n != 1 {
		t.Errorf("schedule after stop should be a no-op, got %d sends", n)
	}
}
