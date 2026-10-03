// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hubtree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type node struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

func (n node) NodePath() string { return n.Path }
func (n node) IsDir() bool      { return n.Type == "directory" }

// fakeTree serves /tree/<dir> listings for a fixed set of file paths.
// recursive: honor recursive=true (else list one level, like some mirrors).
// pageSize: entries per page, linked with Link rel="next" like the Hub.
type fakeTree struct {
	files     []string
	recursive bool
	pageSize  int
	extra     []node // injected (possibly malformed) entries at the root
	requests  atomic.Int32
}

func (f *fakeTree) entries(dir string, recursive bool) []node {
	seenDir := map[string]bool{}
	var out []node
	for _, p := range f.files {
		if dir != "" && !strings.HasPrefix(p, dir+"/") {
			continue
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(p, dir), "/")
		parts := strings.Split(rest, "/")
		base := dir
		for i, part := range parts {
			if base == "" {
				base = part
			} else {
				base += "/" + part
			}
			if i == len(parts)-1 {
				out = append(out, node{"file", base})
			} else if !seenDir[base] {
				seenDir[base] = true
				out = append(out, node{"directory", base})
			}
			if !recursive {
				break
			}
		}
	}
	if dir == "" {
		out = append(out, f.extra...)
	}
	return out
}

func (f *fakeTree) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	dir := strings.Trim(strings.TrimPrefix(r.URL.Path, "/tree"), "/")
	all := f.entries(dir, f.recursive && r.URL.Query().Get("recursive") == "true")
	start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	end := len(all)
	if f.pageSize > 0 && start+f.pageSize < end {
		end = start + f.pageSize
		q := r.URL.Query()
		q.Set("cursor", strconv.Itoa(end))
		w.Header().Set("Link", fmt.Sprintf(`<%s?%s>; rel="next"`, r.URL.Path, q.Encode()))
	}
	json.NewEncoder(w).Encode(all[start:end])
}

func walkAll(t *testing.T, srv *httptest.Server, o Options) ([]string, error) {
	t.Helper()
	o.Client = srv.Client()
	if o.URL == nil {
		o.URL = func(dir string) string {
			if dir == "" {
				return srv.URL + "/tree"
			}
			return srv.URL + "/tree/" + dir
		}
	}
	if o.Status == nil {
		o.Status = func(resp *http.Response) error { return fmt.Errorf("status %d", resp.StatusCode) }
	}
	var got []string
	err := Walk(context.Background(), o, func(n node) error {
		got = append(got, n.Path)
		return nil
	})
	sort.Strings(got)
	return got, err
}

func manyFiles() []string {
	var files []string
	for i := 0; i < 2500; i++ { // > 2 pages in one directory, like allenai/c4
		files = append(files, fmt.Sprintf("en/c4-train.%05d.json.gz", i))
	}
	for i := 0; i < 8; i++ {
		files = append(files, fmt.Sprintf("en/c4-validation.%05d.json.gz", i))
	}
	return append(files, "README.md", "nested/a/b/c.txt")
}

func TestWalk_FollowsPagination(t *testing.T) {
	files := manyFiles()
	ft := &fakeTree{files: files, recursive: true, pageSize: 1000}
	srv := httptest.NewServer(ft)
	defer srv.Close()

	got, err := walkAll(t, srv, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(files) {
		t.Fatalf("listed %d files, want %d (pagination not followed?)", len(got), len(files))
	}
	if n := ft.requests.Load(); n > 4 {
		t.Errorf("made %d requests; a recursive paginated listing needs about 3", n)
	}
}

// TestWalk_MirrorIgnoresRecursive: servers that ignore recursive=true still
// get every file, via per-directory listing.
func TestWalk_MirrorIgnoresRecursive(t *testing.T) {
	files := manyFiles()
	ft := &fakeTree{files: files, recursive: false, pageSize: 1000}
	srv := httptest.NewServer(ft)
	defer srv.Close()

	got, err := walkAll(t, srv, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(files) {
		t.Fatalf("listed %d files, want %d", len(got), len(files))
	}
}

// TestWalk_MalformedDirectoriesTerminate: directory entries with empty, "."
// or parent paths used to make the walker re-list the same URL forever.
func TestWalk_MalformedDirectoriesTerminate(t *testing.T) {
	ft := &fakeTree{
		files:     []string{"a.txt"},
		recursive: false,
		extra:     []node{{"directory", ""}, {"directory", "."}, {"directory", ".."}, {"directory", "../x"}, {"directory", "/"}},
	}
	srv := httptest.NewServer(ft)
	defer srv.Close()

	done := make(chan struct{})
	var got []string
	var err error
	go func() { got, err = walkAll(t, srv, Options{}); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("walk did not terminate (%d requests)", ft.requests.Load())
	}
	if err != nil || len(got) != 1 {
		t.Errorf("got %v, %v; want [a.txt]", got, err)
	}
}

func TestWalk_RetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("RateLimit", `"api";r=0;t=0`)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode([]node{{"file", "a.txt"}})
	}))
	defer srv.Close()

	got, err := walkAll(t, srv, Options{})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v; want one file after a 429 retry", got, err)
	}
}

func TestWalk_PermanentErrorNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	sentinel := errors.New("needs token")
	_, err := walkAll(t, srv, Options{Status: func(*http.Response) error { return sentinel }})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the Status error", err)
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests for a 401, want 1", calls.Load())
	}
}

func TestServerWait(t *testing.T) {
	mk := func(status int, h map[string]string) *http.Response {
		hdr := http.Header{}
		for k, v := range h {
			hdr.Set(k, v)
		}
		return &http.Response{StatusCode: status, Header: hdr}
	}
	cases := []struct {
		resp *http.Response
		want time.Duration
	}{
		{mk(429, map[string]string{"Retry-After": "7"}), 7 * time.Second},
		{mk(429, map[string]string{"RateLimit": `"api";r=0;t=150`}), 151 * time.Second},
		{mk(503, map[string]string{"RateLimit": `"api";r=0;t=150`}), 0},
		{mk(429, nil), 0},
	}
	for _, tc := range cases {
		if got := serverWait(tc.resp); got != tc.want {
			t.Errorf("serverWait(%d, %v) = %s, want %s", tc.resp.StatusCode, tc.resp.Header, got, tc.want)
		}
	}
}

func TestNextLink(t *testing.T) {
	base, _ := http.NewRequest("GET", "https://huggingface.co/api/datasets/a/b/tree/main?recursive=true", nil)
	hub := `<https://huggingface.co/api/datasets/a/b/tree/main?expand=false&recursive=true&limit=1000&cursor=abc>; rel="next"`
	if got := nextLink(hub, base.URL); !strings.Contains(got, "cursor=abc") {
		t.Errorf("nextLink(hub) = %q", got)
	}
	if got := nextLink(`</x?cursor=2>; rel="next"`, base.URL); got != "https://huggingface.co/x?cursor=2" {
		t.Errorf("relative link = %q", got)
	}
	if got := nextLink(`<https://x/prev>; rel="prev"`, base.URL); got != "" {
		t.Errorf("non-next link = %q", got)
	}
	if got := nextLink("", base.URL); got != "" {
		t.Errorf("empty header = %q", got)
	}
}
