// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadSingle_RangeIgnoredWithDropsTerminates: a server that ignores
// Range (always 200, so every resume restarts from zero) and drops the
// connection mid-file must exhaust the retry budget, not loop forever because
// each attempt "wrote bytes".
func TestDownloadSingle_RangeIgnoredWithDropsTerminates(t *testing.T) {
	full := testPayload(10000)
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(full)))
		w.WriteHeader(http.StatusOK)
		w.Write(full[:3000])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/file.bin", Size: int64(len(full))}
	cfg := Settings{Retries: 2, BackoffInitial: "1ms", BackoffMax: "1ms"}

	done := make(chan error, 1)
	go func() {
		done <- downloadSingle(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, func(ProgressEvent) {})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after the retry budget was exhausted")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("download never gave up (%d requests)", gets.Load())
	}
	// First attempt moves 0→3000 (progress, budget resets once); after that
	// every attempt restarts at 0 and ends at 3000, so no further resets.
	if n := gets.Load(); n > 6 {
		t.Errorf("made %d requests with Retries=2; the budget kept resetting", n)
	}
}

func TestDownload_PermanentStatusFailsFast(t *testing.T) {
	for _, tc := range []struct {
		status int
		target error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrUnauthorized},
		{http.StatusNotFound, ErrNotFound},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			var reqs atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reqs.Add(1)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			cfg := Settings{Concurrency: 4, Retries: 4, BackoffInitial: "1ms"}
			for _, multipart := range []bool{false, true} {
				reqs.Store(0)
				dst := filepath.Join(t.TempDir(), "file.bin")
				it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/f", Size: 40000, AcceptRanges: true}
				var err error
				if multipart {
					err = downloadMultipart(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, func(ProgressEvent) {})
				} else {
					err = downloadSingle(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, func(ProgressEvent) {})
				}
				if !errors.Is(err, tc.target) {
					t.Errorf("multipart=%v: err = %v, want errors.Is %v", multipart, err, tc.target)
				}
				if n := reqs.Load(); n != 1 {
					t.Errorf("multipart=%v: %d requests, want 1 (no retries for %d)", multipart, n, tc.status)
				}
				if left, _ := filepath.Glob(dst + ".part*"); len(left) != 0 {
					t.Errorf("multipart=%v: left temp files behind: %v", multipart, left)
				}
			}
		})
	}
}

// TestDownloadMultipart_FallsBackWhenRangeIgnored: a server that answers part
// requests with 200 must not fail the download; it falls back to one stream.
func TestDownloadMultipart_FallsBackWhenRangeIgnored(t *testing.T) {
	full := testPayload(40000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(full)))
		if r.Method == http.MethodHead {
			return
		}
		w.Write(full) // ignores Range
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/f", Size: int64(len(full)), AcceptRanges: true}
	cfg := Settings{Concurrency: 4, Retries: 2, BackoffInitial: "1ms"}
	if err := downloadMultipart(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, func(ProgressEvent) {}); err != nil {
		t.Fatalf("downloadMultipart: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, full) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(full))
	}
	if left, _ := filepath.Glob(dst + ".part-*"); len(left) != 0 {
		t.Errorf("part files left behind: %v", left)
	}
}

// TestDownloadMultipart_ClampsOverlongPart: a 206 body longer than the
// requested range must not spill into the part file.
func TestDownloadMultipart_ClampsOverlongPart(t *testing.T) {
	full := testPayload(40000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(full)))
			return
		}
		var start, end int
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		w.WriteHeader(http.StatusPartialContent)
		w.Write(full[start:]) // everything to EOF, not just the range
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/f", Size: int64(len(full)), AcceptRanges: true}
	if err := downloadMultipart(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, Settings{Concurrency: 4, Retries: 1}, it, dst, func(ProgressEvent) {}); err != nil {
		t.Fatalf("downloadMultipart: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, full) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(full))
	}
}

func TestRetryAfter(t *testing.T) {
	mk := func(status int, v string) *http.Response {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return &http.Response{StatusCode: status, Header: h}
	}
	cases := []struct {
		resp *http.Response
		want time.Duration
	}{
		{mk(429, "3"), 3 * time.Second},
		{mk(503, "1"), time.Second},
		{mk(429, "999999"), maxServerWait},
		{mk(429, ""), 0},
		{mk(429, "soon"), 0},
		{mk(500, "3"), 0},
	}
	for _, tc := range cases {
		if got := retryAfter(tc.resp); got != tc.want {
			t.Errorf("retryAfter(%d, %q) = %s, want %s", tc.resp.StatusCode, tc.resp.Header.Get("Retry-After"), got, tc.want)
		}
	}
}

func TestBlobTempName(t *testing.T) {
	a := blobTempName(PlanItem{RelativePath: "a/b_c.json"})
	b := blobTempName(PlanItem{RelativePath: "a_b/c.json"})
	if a == b {
		t.Errorf("distinct paths share temp name %q", a)
	}
	long := blobTempName(PlanItem{RelativePath: strings.Repeat("d/", 100) + strings.Repeat("x", 250)})
	if len(long) > 64 {
		t.Errorf("temp name too long (%d): %s", len(long), long)
	}
	if got := blobTempName(PlanItem{RelativePath: "m.gguf", SHA256: "abc"}); got != "tmp-abc" {
		t.Errorf("hash-named temp = %q, want tmp-abc (stable for resume)", got)
	}
}

func TestValidate_StallTimeout(t *testing.T) {
	for _, v := range []string{"", "0", "1s", "90s", "2m"} {
		if err := validate(Job{Repo: "o/r"}, Settings{StallTimeout: v}); err != nil {
			t.Errorf("stall-timeout %q rejected: %v", v, err)
		}
	}
	for _, v := range []string{"banana", "-5s", "1ns", "500ms"} {
		if err := validate(Job{Repo: "o/r"}, Settings{StallTimeout: v}); err == nil {
			t.Errorf("stall-timeout %q accepted", v)
		}
	}
}

// fakeHub is a minimal HuggingFace endpoint: revision info, a flat tree and
// resolve/raw file routes. pinnedOK controls whether tree and file routes
// accept the commit SHA (some mirrors only accept branch names).
type fakeHub struct {
	commit        string
	files         map[string][]byte // path → content (all served as LFS)
	treeAcceptSHA bool
	fileAcceptSHA bool
	fileDelay     time.Duration // delay before serving file bodies

	mu    sync.Mutex
	paths []string
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.paths = append(h.paths, r.Method+" "+r.URL.Path)
	h.mu.Unlock()
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/models/o/r/revision/"):
		json.NewEncoder(w).Encode(map[string]string{"sha": h.commit})
	case strings.HasPrefix(p, "/api/models/o/r/tree/"):
		rev := strings.TrimPrefix(p, "/api/models/o/r/tree/")
		if rev == h.commit && !h.treeAcceptSHA {
			http.NotFound(w, r)
			return
		}
		var nodes []hfNode
		for path, data := range h.files {
			sum := sha256.Sum256(data)
			nodes = append(nodes, hfNode{Type: "file", Path: path, Size: 100,
				LFS: &hfLfsInfo{Oid: hex.EncodeToString(sum[:]), Size: int64(len(data))}})
		}
		json.NewEncoder(w).Encode(nodes)
	case strings.HasPrefix(p, "/o/r/resolve/"):
		rest := strings.TrimPrefix(p, "/o/r/resolve/")
		rev, path, _ := strings.Cut(rest, "/")
		if rev == h.commit && !h.fileAcceptSHA {
			http.NotFound(w, r)
			return
		}
		data, ok := h.files[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			time.Sleep(h.fileDelay)
		}
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(data))
	default:
		http.NotFound(w, r)
	}
}

func (h *fakeHub) requested(substr string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.paths {
		if strings.Contains(p, substr) {
			n++
		}
	}
	return n
}

func TestScanRepo_RevisionFallback(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	files := map[string][]byte{"model.bin": testPayload(5000)}
	for _, tc := range []struct {
		name               string
		treeSHA, fileSHA   bool
		wantURLContains    string
		wantURLNotContains string
	}{
		{"full pinning", true, true, "/resolve/" + commit + "/", "/resolve/main/"},
		{"tree rejects sha", false, true, "/resolve/main/", "/resolve/" + commit},
		{"files reject sha", true, false, "/resolve/main/", "/resolve/" + commit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := &fakeHub{commit: commit, files: files, treeAcceptSHA: tc.treeSHA, fileAcceptSHA: tc.fileSHA}
			srv := httptest.NewServer(hub)
			defer srv.Close()

			plan, err := scanRepo(context.Background(), srv.Client(), "", Job{Repo: "o/r", Revision: "main"}, Settings{Endpoint: srv.URL})
			if err != nil {
				t.Fatalf("scanRepo: %v", err)
			}
			if plan.Commit != commit {
				t.Errorf("plan commit = %q, want %q", plan.Commit, commit)
			}
			if len(plan.Items) != 1 {
				t.Fatalf("items = %d, want 1", len(plan.Items))
			}
			u := plan.Items[0].URL
			if !strings.Contains(u, tc.wantURLContains) || strings.Contains(u, tc.wantURLNotContains) {
				t.Errorf("URL = %s, want it to contain %q", u, tc.wantURLContains)
			}
		})
	}
}

// TestDownload_DuplicateContentFiles: two paths with byte-identical content
// share a blob and a temp file. They used to download concurrently into the
// same tmp-<sha> and one failed with a rename error.
func TestDownload_DuplicateContentFiles(t *testing.T) {
	data := testPayload(300000)
	hub := &fakeHub{
		commit:        "0123456789abcdef0123456789abcdef01234567",
		files:         map[string][]byte{"Q2_K/model-Q2_K.gguf": data, "model-Q2_K_L.gguf": data},
		treeAcceptSHA: true,
		fileAcceptSHA: true,
		fileDelay:     50 * time.Millisecond, // make the two downloads overlap
	}
	srv := httptest.NewServer(hub)
	defer srv.Close()

	cache := t.TempDir()
	cfg := Settings{
		Endpoint: srv.URL, CacheDir: cache, Concurrency: 4, MaxActiveDownloads: 4,
		MultipartThreshold: "64KiB", Retries: 1, NoManifest: true,
	}
	if err := Download(context.Background(), Job{Repo: "o/r"}, cfg, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	snap := filepath.Join(cache, "hub", "models--o--r", "snapshots", hub.commit)
	for path := range hub.files {
		got, err := os.ReadFile(filepath.Join(snap, path))
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("%s: err=%v, content ok=%v", path, err, bytes.Equal(got, data))
		}
	}
	if n := hub.requested("HEAD /o/r/resolve/"); n > 2 { // 1 file download + 1 pin probe
		t.Errorf("identical content downloaded more than once (%d HEADs)", n)
	}
}

func TestValidate_VerifyMode(t *testing.T) {
	for _, v := range []string{"", "none", "size", "etag", "sha256"} {
		if err := validate(Job{Repo: "o/r"}, Settings{Verify: v}); err != nil {
			t.Errorf("verify %q rejected: %v", v, err)
		}
	}
	if err := validate(Job{Repo: "o/r"}, Settings{Verify: "bogus"}); err == nil {
		t.Error("verify bogus accepted")
	}
}
