// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

// Package hubtree lists HuggingFace repository trees. It is shared by the
// downloader's planner and the analyzer so both see every file.
//
// The Hub's tree API returns at most 1000 entries per response and links the
// next page with a `Link: <...>; rel="next"` header. Walking one directory at
// a time without following those links silently dropped everything past the
// first 1000 entries of a directory (e.g. 4,515 of 69,221 files of
// allenai/c4) and cost one API call per directory, exhausting the Hub's rate
// limit on repos with many folders. Walk requests a recursive listing and
// follows pagination instead, falling back to per-directory listing for
// mirrors that ignore the recursive parameter.
package hubtree

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Node is a tree entry as decoded from the API.
type Node interface {
	NodePath() string
	IsDir() bool
}

// Options configures Walk.
type Options struct {
	Client *http.Client
	// URL returns the (unpaginated) listing URL for a directory prefix;
	// "" is the repository root.
	URL func(prefix string) string
	// Authorize adds auth headers to each request (may be nil).
	Authorize func(*http.Request)
	// Status converts a non-200 response into an error. It is called after
	// rate-limit/server-error retries are exhausted.
	Status func(*http.Response) error
	// OnResponse sees every successful response, e.g. to read X-Repo-Commit
	// (may be nil).
	OnResponse func(*http.Response)
	// Retries for 429/5xx/network errors per request (default 5).
	Retries int
}

// MaxWait caps a single wait requested by the server's rate-limit headers.
const MaxWait = 5 * time.Minute

// Walk lists every entry under the repository root and calls fn for each
// file (directories are not passed to fn).
func Walk[N Node](ctx context.Context, o Options, fn func(N) error) error {
	if o.Retries <= 0 {
		o.Retries = 5
	}
	visited := map[string]bool{}
	return walkPrefix(ctx, o, "", visited, fn)
}

func walkPrefix[N Node](ctx context.Context, o Options, prefix string, visited map[string]bool, fn func(N) error) error {
	visited[prefix] = true
	var dirs []string
	parents := map[string]bool{} // every directory that has a listed descendant

	next := withRecursive(o.URL(prefix))
	for next != "" {
		resp, err := get(ctx, o, next)
		if err != nil {
			return err
		}
		var nodes []N
		err = json.NewDecoder(resp.Body).Decode(&nodes)
		link := resp.Header.Get("Link")
		reqURL := resp.Request.URL
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("decode tree listing: %w", err)
		}

		for _, n := range nodes {
			p := strings.Trim(n.NodePath(), "/")
			for d := path.Dir(p); d != "." && d != "/" && d != ""; d = path.Dir(d) {
				if parents[d] {
					break
				}
				parents[d] = true
			}
			if n.IsDir() {
				dirs = append(dirs, p)
				continue
			}
			if err := fn(n); err != nil {
				return err
			}
		}
		next = nextLink(link, reqURL)
	}

	// A directory with no listed descendants means the server ignored
	// recursive=true (some mirrors): list it explicitly. Git trees have no
	// empty directories, so this never re-lists a directory the Hub covered.
	// Entries that aren't strictly below prefix (empty paths, the prefix
	// itself, "..") are ignored so a malformed listing can't recurse forever.
	for _, d := range dirs {
		if parents[d] || visited[d] || !strictlyUnder(d, prefix) {
			continue
		}
		if err := walkPrefix(ctx, o, d, visited, fn); err != nil {
			return err
		}
	}
	return nil
}

// strictlyUnder reports whether p is a clean path strictly inside prefix.
func strictlyUnder(p, prefix string) bool {
	if p == "" || p != path.Clean(p) || p == ".." || strings.HasPrefix(p, "../") {
		return false
	}
	return prefix == "" || strings.HasPrefix(p, prefix+"/")
}

// get performs a GET, retrying rate limits (429), server errors (5xx) and
// network errors with the server-requested or exponential delay.
func get(ctx context.Context, o Options, u string) (*http.Response, error) {
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if o.Authorize != nil {
			o.Authorize(req)
		}
		resp, err := o.Client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			if o.OnResponse != nil {
				o.OnResponse(resp)
			}
			return resp, nil
		}
		retryable := err != nil || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable || attempt >= o.Retries {
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			return nil, o.Status(resp)
		}
		wait := backoff
		if err == nil {
			if d := serverWait(resp); d > 0 {
				wait = d
			}
			resp.Body.Close()
		}
		if wait > MaxWait {
			wait = MaxWait
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
		backoff *= 2
	}
}

var rateLimitReset = regexp.MustCompile(`(?:^|[;,\s])t=(\d+)`)

// serverWait returns how long the server asked us to wait: Retry-After
// (seconds or HTTP date), else the Hub's `RateLimit: "api";r=0;t=<secs>`.
func serverWait(resp *http.Response) time.Duration {
	if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if m := rateLimitReset.FindStringSubmatch(resp.Header.Get("RateLimit")); m != nil {
			if secs, err := strconv.Atoi(m[1]); err == nil {
				return time.Duration(secs+1) * time.Second
			}
		}
	}
	return 0
}

// withRecursive adds recursive=true to a listing URL.
func withRecursive(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	q.Set("recursive", "true")
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// nextLink extracts the rel="next" target from a Link header, resolved
// against the request URL.
func nextLink(header string, base *url.URL) string {
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(part, ";")
		target := strings.TrimSpace(segs[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, s := range segs[1:] {
			if strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(s), " ", ""), `rel="next"`) ||
				strings.EqualFold(strings.TrimSpace(s), "rel=next") {
				next, err := url.Parse(strings.Trim(target, "<>"))
				if err != nil {
					return ""
				}
				if base != nil {
					next = base.ResolveReference(next)
				}
				return next.String()
			}
		}
	}
	return ""
}
