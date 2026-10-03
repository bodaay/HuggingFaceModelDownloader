// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stallingRangeServer serves Range requests for full, but the FIRST request
// for each distinct range end writes half its bytes and then goes silent
// without closing the connection — the github #87/#88 failure mode that TCP
// keepalive cannot detect. Later requests are served normally.
func stallingRangeServer(t *testing.T, full []byte) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	stalled := map[int64]bool{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(full)))
			w.Header().Set("Accept-Ranges", "bytes")
			return
		}
		start, end := int64(0), int64(len(full)-1)
		status := http.StatusOK
		if rng := r.Header.Get("Range"); rng != "" {
			if _, err := fmt.Sscanf(rng, "bytes=%d-%d", &start, &end); err != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(full)))
		}
		mu.Lock()
		stall := !stalled[end]
		stalled[end] = true
		mu.Unlock()

		content := full[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(status)
		if stall {
			w.Write(content[:len(content)/2])
			w.(http.Flusher).Flush()
			select { // go silent until the client gives up
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		}
		w.Write(content)
	}))
}

func testPayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// retryRecorder collects retry events emitted by a download.
type retryRecorder struct {
	mu       sync.Mutex
	messages []string
}

func (r *retryRecorder) emit(ev ProgressEvent) {
	if ev.Event == "retry" {
		r.mu.Lock()
		r.messages = append(r.messages, ev.Message)
		r.mu.Unlock()
	}
}

func (r *retryRecorder) sawStall() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.messages {
		if strings.Contains(m, ErrStalled.Error()) {
			return true
		}
	}
	return false
}

func TestDownloadSingle_RetriesStalledTransfer(t *testing.T) {
	full := testPayload(10000)
	srv := stallingRangeServer(t, full)
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/file.bin", Size: int64(len(full))}
	cfg := Settings{Retries: 2, BackoffInitial: "1ms", StallTimeout: "150ms"}
	rec := &retryRecorder{}

	start := time.Now()
	if err := downloadSingle(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, rec.emit); err != nil {
		t.Fatalf("downloadSingle: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("stall was not detected promptly: took %s", elapsed)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, full) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(full))
	}
	if !rec.sawStall() {
		t.Errorf("expected a retry event reporting the stall, got %v", rec.messages)
	}
}

func TestDownloadMultipart_RetriesStalledPart(t *testing.T) {
	full := testPayload(40000)
	srv := stallingRangeServer(t, full)
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/file.bin", Size: int64(len(full)), AcceptRanges: true}
	cfg := Settings{Concurrency: 4, Retries: 2, BackoffInitial: "1ms", StallTimeout: "150ms"}
	rec := &retryRecorder{}

	if err := downloadMultipart(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, rec.emit); err != nil {
		t.Fatalf("downloadMultipart: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, full) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(full))
	}
	if !rec.sawStall() {
		t.Errorf("expected retry events reporting stalls, got %v", rec.messages)
	}
	if _, err := os.Stat(multipartLayoutFile(dst)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("layout record should be removed after assembly, stat err = %v", err)
	}
}

// TestDownloadSingle_ProgressResetsRetryBudget: a connection that drops every
// few KB must not exhaust the retry budget as long as each attempt moves data
// forward. Before, 5 interruptions in total killed even a progressing download.
func TestDownloadSingle_ProgressResetsRetryBudget(t *testing.T) {
	full := testPayload(10000)
	const chunk = 1000 // bytes served per connection before it drops
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := int64(0)
		if rng := r.Header.Get("Range"); rng != "" {
			fmt.Sscanf(rng, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(full)-1, len(full)))
			w.Header().Set("Content-Length", strconv.Itoa(len(full)-int(start)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(full)))
			w.WriteHeader(http.StatusOK)
		}
		end := start + chunk
		if end >= int64(len(full)) {
			w.Write(full[start:])
			return
		}
		w.Write(full[start:end])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "file.bin")
	it := PlanItem{RelativePath: "file.bin", URL: srv.URL + "/file.bin", Size: int64(len(full))}
	cfg := Settings{Retries: 1, BackoffInitial: "1ms", BackoffMax: "1ms"}

	if err := downloadSingle(context.Background(), srv.Client(), "", Job{Repo: "o/r"}, cfg, it, dst, func(ProgressEvent) {}); err != nil {
		t.Fatalf("downloadSingle should finish despite 9 interruptions with Retries=1: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, full) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(full))
	}
}

func TestPrepareMultipartLayout(t *testing.T) {
	writeParts := func(dst string, n int) {
		for i := 0; i < n; i++ {
			os.WriteFile(fmt.Sprintf("%s.part-%02d", dst, i), []byte("x"), 0o644)
		}
	}
	partCount := func(dst string) int {
		m, _ := filepath.Glob(dst + ".part-*")
		return len(m)
	}

	t.Run("same layout keeps parts", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "f")
		writeParts(dst, 4)
		os.WriteFile(multipartLayoutFile(dst), []byte("4 100"), 0o644)
		if err := prepareMultipartLayout(dst, 4, 100); err != nil {
			t.Fatal(err)
		}
		if got := partCount(dst); got != 4 {
			t.Errorf("parts = %d, want 4 kept", got)
		}
	})

	t.Run("changed connection count discards parts", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "f")
		writeParts(dst, 4)
		os.WriteFile(multipartLayoutFile(dst), []byte("4 100"), 0o644)
		if err := prepareMultipartLayout(dst, 2, 100); err != nil {
			t.Fatal(err)
		}
		if got := partCount(dst); got != 0 {
			t.Errorf("parts = %d, want all discarded", got)
		}
		if b, _ := os.ReadFile(multipartLayoutFile(dst)); string(b) != "2 100" {
			t.Errorf("layout record = %q, want %q", b, "2 100")
		}
	})

	t.Run("unrecorded parts beyond current count are discarded", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "f")
		writeParts(dst, 8)
		if err := prepareMultipartLayout(dst, 4, 100); err != nil {
			t.Fatal(err)
		}
		if got := partCount(dst); got != 0 {
			t.Errorf("parts = %d, want all discarded", got)
		}
	})

	t.Run("unrecorded parts that fit are kept", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "f")
		writeParts(dst, 4)
		if err := prepareMultipartLayout(dst, 4, 100); err != nil {
			t.Fatal(err)
		}
		if got := partCount(dst); got != 4 {
			t.Errorf("parts = %d, want 4 kept", got)
		}
	})
}

func TestAssembleParts_StopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p0")
	os.WriteFile(p, testPayload(1000), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := assembleParts(ctx, filepath.Join(dir, "out"), []string{p}, func(int64) {})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestComputeSHA256Ctx_ReportsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	data := testPayload(3 << 20)
	os.WriteFile(path, data, 0o644)

	var last int64
	sum, err := computeSHA256Ctx(context.Background(), path, func(done int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	want, _ := computeSHA256(path)
	if sum != want {
		t.Errorf("sum mismatch")
	}
	if last != int64(len(data)) {
		t.Errorf("final progress = %d, want %d", last, len(data))
	}
}

func TestStallTimeoutSetting(t *testing.T) {
	cases := map[string]time.Duration{
		"":      DefaultStallTimeout,
		"0":     0,
		"2m":    2 * time.Minute,
		"bogus": DefaultStallTimeout,
		"-5s":   DefaultStallTimeout,
	}
	for in, want := range cases {
		if got := stallTimeout(Settings{StallTimeout: in}); got != want {
			t.Errorf("stallTimeout(%q) = %s, want %s", in, got, want)
		}
	}
}
