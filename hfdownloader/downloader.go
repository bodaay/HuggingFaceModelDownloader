// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	AgreementModelURL      = "https://huggingface.co/%s"
	AgreementDatasetURL    = "https://huggingface.co/datasets/%s"
	RawModelFileURL        = "https://huggingface.co/%s/raw/%s/%s"
	RawDatasetFileURL      = "https://huggingface.co/datasets/%s/raw/%s/%s"
	LfsModelResolverURL    = "https://huggingface.co/%s/resolve/%s/%s"
	LfsDatasetResolverURL  = "https://huggingface.co/datasets/%s/resolve/%s/%s"
	JsonModelsFileTreeURL  = "https://huggingface.co/api/models/%s/tree/%s/%s"
	JsonDatasetFileTreeURL = "https://huggingface.co/api/datasets/%s/tree/%s/%s"
)

// IsValidModelName checks "owner/name".
func IsValidModelName(modelName string) bool {
	if modelName == "" || !strings.Contains(modelName, "/") {
		return false
	}
	parts := strings.Split(modelName, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// ------------------------
// Public API
// ------------------------

type PlanItem struct {
	RelativePath string `json:"path"`
	URL          string `json:"url"`
	LFS          bool   `json:"lfs"`
	SHA256       string `json:"sha256,omitempty"`
	Size         int64  `json:"size"`
	AcceptRanges bool   `json:"acceptRanges"`
	// Subdir holds the matched filter (if any) used when --append-filter-subdir is set.
	Subdir string `json:"subdir,omitempty"`
}

type Plan struct {
	Items []PlanItem `json:"items"`
}

// Plan builds the file list without downloading.
func PlanRepo(ctx context.Context, job Job, cfg Settings) (*Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validate(job, cfg); err != nil {
		return nil, err
	}
	if job.Revision == "" {
		job.Revision = "main"
	}
	httpc := buildHTTPClient()
	return scanRepo(ctx, httpc, cfg.Token, job, cfg)
}

// Download scans and downloads. Resume is always ON.
// Skip decisions rely ONLY on the filesystem:
//   - LFS files: sha256 comparison when SHA is available.
//   - non-LFS files: size comparison.
//
// Cancellation: all loops/sleeps/requests are tied to ctx for fast abort.
func Download(ctx context.Context, job Job, cfg Settings, progress ProgressFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validate(job, cfg); err != nil {
		return err
	}
	if job.Revision == "" {
		job.Revision = "main"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "Storage"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.MaxActiveDownloads <= 0 {
		cfg.MaxActiveDownloads = runtime.GOMAXPROCS(0)
	}
	// 32MiB default threshold (configurable via cfg.MultipartThreshold)
	thresholdBytes, err := parseSizeString(cfg.MultipartThreshold, 256<<20)
	if err != nil {
		return fmt.Errorf("invalid multipart-threshold: %w", err)
	}

	httpc := buildHTTPClient()
	emit := func(ev ProgressEvent) {
		if progress != nil {
			if ev.Time.IsZero() {
				ev.Time = time.Now()
			}
			if ev.Repo == "" {
				ev.Repo = job.Repo
			}
			if ev.Revision == "" {
				ev.Revision = job.Revision
			}
			progress(ev)
		}
	}

	emit(ProgressEvent{Event: "scan_start", Message: "scanning repo"})

	plan, err := scanRepo(ctx, httpc, cfg.Token, job, cfg)
	if err != nil {
		return err
	}

	// Ensure destination root exists
	if err := os.MkdirAll(destinationBase(job, cfg), 0o755); err != nil {
		return err
	}

	// Overall concurrency limiter (ctx-aware acquisition)
	type token struct{}
	lim := make(chan token, cfg.MaxActiveDownloads)

	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Items))

	// To print "skip" only once per final path (including subdir) per run
	var skipOnce sync.Map // map[finalRelPath]struct{}

	var skippedCount int64
	var downloadedCount int64

LOOP:
	for _, item := range plan.Items {
		// Stop scheduling more work once canceled
		select {
		case <-ctx.Done():
			break LOOP
		default:
		}

		it := item // capture for goroutine

		// Final relative path shown to the user (includes subdir if requested)
		displayRel := it.RelativePath
		if job.AppendFilterSubdir && it.Subdir != "" {
			displayRel = filepath.ToSlash(filepath.Join(it.Subdir, it.RelativePath))
		}

		emit(ProgressEvent{Event: "plan_item", Path: displayRel, Total: it.Size})

		// Acquire a slot or abort if canceled
		select {
		case lim <- token{}:
		case <-ctx.Done():
			break LOOP
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-lim }()

			// Per-file context; ensures all inner loops stop on cancellation
			fileCtx, fileCancel := context.WithCancel(ctx)
			defer fileCancel()

			// final destination path: OutputDir/<repo>[/<filter>]/<repo-relative-path>
			base := destinationBase(job, cfg)
			finalRel := it.RelativePath
			if job.AppendFilterSubdir && it.Subdir != "" {
				finalRel = filepath.ToSlash(filepath.Join(it.Subdir, it.RelativePath))
			}
			dst := filepath.Join(base, finalRel)

			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				select { // don't block on emit if we're canceled
				case errCh <- err:
				default:
				}
				return
			}

			// Filesystem-based skip/resume
			alreadyOK, reason, err := shouldSkipLocal(it, dst)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if alreadyOK {
				// Ensure we print "skip" at most once for this final path in this run.
				if _, loaded := skipOnce.LoadOrStore(finalRel, struct{}{}); !loaded {
					emit(ProgressEvent{Event: "file_done", Path: finalRel, Message: "skip (" + reason + ")"})
					atomic.AddInt64(&skippedCount, 1)
				}
				return
			}

			emit(ProgressEvent{Event: "file_start", Path: finalRel, Total: it.Size})

			// Choose single/multipart path
			var dlErr error
			// For progress to show the finalRel inside the inner helpers, pass a copy with updated RelativePath.
			itForIO := it
			itForIO.RelativePath = finalRel

			if it.Size >= thresholdBytes && it.AcceptRanges {
				dlErr = downloadMultipart(fileCtx, httpc, cfg.Token, job, cfg, itForIO, dst, emit)
			} else {
				dlErr = downloadSingle(fileCtx, httpc, cfg.Token, job, cfg, itForIO, dst, emit)
			}
			if dlErr != nil {
				select {
				case errCh <- fmt.Errorf("download %s: %w", finalRel, dlErr):
				default:
				}
				return
			}

			// Verify after download
			if it.LFS && it.SHA256 != "" {
				if err := verifySHA256(dst, it.SHA256); err != nil {
					select {
					case errCh <- fmt.Errorf("sha256 verify failed: %s: %w", finalRel, err):
					default:
					}
					return
				}
			} else if cfg.Verify == "size" && it.Size > 0 {
				fi, err := os.Stat(dst)
				if err != nil || fi.Size() != it.Size {
					select {
					case errCh <- fmt.Errorf("size mismatch for %s", finalRel):
					default:
					}
					return
				}
			} else if cfg.Verify == "sha256" {
				// For non-LFS, try remote-provided sha via HEAD (if present)
				_, remoteSha, _ := headForETag(fileCtx, httpc, cfg.Token, itForIO)
				if remoteSha != "" {
					if err := verifySHA256(dst, remoteSha); err != nil {
						select {
						case errCh <- fmt.Errorf("sha256 verify failed: %s: %w", finalRel, err):
						default:
						}
						return
					}
				}
			}

			emit(ProgressEvent{Event: "file_done", Path: finalRel})
			atomic.AddInt64(&downloadedCount, 1)
		}()
	}

	// Wait for all started workers
	wg.Wait()
	close(errCh)

	// Drain errors; handle first non-nil (prevents nil deref panic).
	var firstErr error
	for e := range errCh {
		if e != nil {
			firstErr = e
			break
		}
	}
	if firstErr != nil {
		emit(ProgressEvent{Level: "error", Event: "error", Message: firstErr.Error()})
		return firstErr
	}

	// If canceled, surface context error (fast abort) rather than "complete"
	if ctx.Err() != nil {
		return ctx.Err()
	}

	emit(ProgressEvent{
		Event:   "done",
		Message: fmt.Sprintf("download complete (downloaded %d, skipped %d)", downloadedCount, skippedCount),
	})
	return nil
}

func validate(job Job, cfg Settings) error {
	if job.Repo == "" {
		return errors.New("missing repo")
	}
	if !IsValidModelName(job.Repo) {
		return fmt.Errorf("invalid repo id %q (expected owner/name)", job.Repo)
	}
	return nil
}

// ------------------------
// Planning & API
// ------------------------

func destinationBase(job Job, cfg Settings) string {
	// Always OutputDir/<repo>; per-file filter subdirs are applied in Download().
	return filepath.Join(cfg.OutputDir, job.Repo)
}

func scanRepo(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings) (*Plan, error) {
	var items []PlanItem
	seen := make(map[string]struct{}) // ensure each relative path appears once in the plan

	err := walkTree(ctx, httpc, token, job, "", func(n hfNode) error {
		if n.Type != "file" && n.Type != "blob" {
			return nil
		}
		rel := n.Path

		// Deduplicate by relative path
		if _, ok := seen[rel]; ok {
			return nil
		}
		seen[rel] = struct{}{}

		name := filepath.Base(rel)
		isLFS := n.LFS != nil

		// Determine which filter (if any) matches this file name, prefer the longest match
		matchedFilter := ""
		if isLFS && len(job.Filters) > 0 {
			for _, f := range job.Filters {
				if strings.Contains(name, f) {
					if len(f) > len(matchedFilter) {
						matchedFilter = f
					}
				}
			}
			// If filters provided and none matched, skip typical large LFS blobs
			if matchedFilter == "" {
				ln := strings.ToLower(name)
				ext := strings.ToLower(filepath.Ext(name))
				if ext == ".bin" || ext == ".act" || ext == ".safetensors" || ext == ".zip" || strings.HasSuffix(ln, ".gguf") || strings.HasSuffix(ln, ".ggml") {
					return nil
				}
			}
		}

		// Build URL and file size
		var urlStr string
		if isLFS {
			urlStr = lfsURL(job, rel)
		} else {
			urlStr = rawURL(job, rel)
		}
		size := n.Size
		if size == 0 && n.LFS != nil && n.LFS.Size > 0 {
			size = n.LFS.Size
		}

		// Best-effort Accept-Ranges
		acceptRanges := false
		if headOK, accept := quickHeadAcceptRanges(ctx, httpc, token, urlStr); headOK {
			acceptRanges = accept
		}

		sha := n.Sha256
		if sha == "" && n.LFS != nil {
			sha = n.LFS.Sha256
		}

		items = append(items, PlanItem{
			RelativePath: rel,
			URL:          urlStr,
			LFS:          isLFS,
			SHA256:       sha,
			Size:         size,
			AcceptRanges: acceptRanges,
			Subdir:       matchedFilter, // empty when no filter matched
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Plan{Items: items}, nil
}

func rawURL(job Job, path string) string {
	if job.IsDataset {
		return fmt.Sprintf(RawDatasetFileURL, url.PathEscape(job.Repo), url.PathEscape(job.Revision), pathEscapeAll(path))
	}
	return fmt.Sprintf(RawModelFileURL, url.PathEscape(job.Repo), url.PathEscape(job.Revision), pathEscapeAll(path))
}

func lfsURL(job Job, path string) string {
	if job.IsDataset {
		return fmt.Sprintf(LfsDatasetResolverURL, url.PathEscape(job.Repo), url.PathEscape(job.Revision), pathEscapeAll(path))
	}
	return fmt.Sprintf(LfsModelResolverURL, url.PathEscape(job.Repo), url.PathEscape(job.Revision), pathEscapeAll(path))
}

func treeURL(job Job, prefix string) string {
	if job.IsDataset {
		return fmt.Sprintf(JsonDatasetFileTreeURL, url.PathEscape(job.Repo), url.PathEscape(job.Revision), pathEscapeAll(prefix))
	}
	return fmt.Sprintf(JsonModelsFileTreeURL, url.PathEscape(job.Repo), url.PathEscape(job.Revision), pathEscapeAll(prefix))
}

func pathEscapeAll(p string) string {
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return strings.Join(segs, "/")
}

type hfNode struct {
	Type   string     `json:"type"` // "file"|"directory" (sometimes "blob"|"tree")
	Path   string     `json:"path"`
	Size   int64      `json:"size,omitempty"`
	LFS    *hfLfsInfo `json:"lfs,omitempty"`
	Sha256 string     `json:"sha256,omitempty"`
}

type hfLfsInfo struct {
	Oid    string `json:"oid,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

func walkTree(ctx context.Context, httpc *http.Client, token string, job Job, prefix string, fn func(hfNode) error) error {
	reqURL := treeURL(job, prefix)
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		base := AgreementModelURL
		if job.IsDataset {
			base = AgreementDatasetURL
		}
		return fmt.Errorf("401 unauthorized: repo requires token or you do not have access (visit %s)", fmt.Sprintf(base, job.Repo))
	}
	if resp.StatusCode == 403 {
		base := AgreementModelURL
		if job.IsDataset {
			base = AgreementDatasetURL
		}
		return fmt.Errorf("403 forbidden: please accept the repository terms: %s", fmt.Sprintf(base, job.Repo))
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("tree API failed: %s", resp.Status)
	}
	var nodes []hfNode
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&nodes); err != nil {
		return err
	}
	for _, n := range nodes {
		switch n.Type {
		case "directory", "tree":
			if err := walkTree(ctx, httpc, token, job, n.Path, fn); err != nil {
				return err
			}
		default:
			if err := fn(n); err != nil {
				return err
			}
		}
	}
	return nil
}

// ------------------------
// Download helpers
// ------------------------

func buildHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{Transport: tr}
}

func addAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "hfdownloader/2")
}

func quickHeadAcceptRanges(ctx context.Context, httpc *http.Client, token string, urlStr string) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	return true, strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes")
}

func headForETag(ctx context.Context, httpc *http.Client, token string, it PlanItem) (etag string, remoteSha string, _ error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", it.URL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	return resp.Header.Get("ETag"), resp.Header.Get("x-amz-meta-sha256"), nil
}

func downloadSingle(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings, it PlanItem, dst string, emit func(ProgressEvent)) error {
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer out.Close()

	retry := newRetry(cfg)
	var lastErr error
	for attempt := 0; attempt <= cfg.Retries; attempt++ {
		// Abort promptly if canceled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, _ := http.NewRequestWithContext(ctx, "GET", it.URL, nil)
		addAuth(req, token)
		resp, err := httpc.Do(req)
		if err != nil {
			lastErr = err
		} else {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("bad status: %s", resp.Status)
			} else {
				_, cerr := io.Copy(out, resp.Body) // Read returns fast on ctx cancel
				resp.Body.Close()
				if cerr == nil {
					out.Close()
					return os.Rename(tmp, dst)
				}
				lastErr = cerr
			}
		}
		if attempt < cfg.Retries {
			emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
			if d := retry.Next(); !sleepCtx(ctx, d) {
				return ctx.Err()
			}
			continue
		}
		break
	}
	return lastErr
}

func downloadMultipart(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings, it PlanItem, dst string, emit func(ProgressEvent)) error {
	// HEAD to resolve size
	req, _ := http.NewRequestWithContext(ctx, "HEAD", it.URL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if it.Size == 0 {
		if clen := resp.Header.Get("Content-Length"); clen != "" {
			var n int64
			fmt.Sscan(clen, &n)
			it.Size = n
		}
	}
	if it.Size == 0 {
		// Fallback
		return downloadSingle(ctx, httpc, token, job, cfg, it, dst, emit)
	}

	// Plan parts
	n := cfg.Concurrency
	chunk := it.Size / int64(n)
	if chunk <= 0 {
		chunk = it.Size
		n = 1
	}
	tmpParts := make([]string, n)
	for i := 0; i < n; i++ {
		tmpParts[i] = fmt.Sprintf("%s.part-%02d", dst, i)
	}

	// Download parts in parallel with resume
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		start := int64(i) * chunk
		end := start + chunk - 1
		if i == n-1 {
			end = it.Size - 1
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			tmp := tmpParts[i]
			// Resume part: skip if already correct size
			if fi, err := os.Stat(tmp); err == nil && fi.Size() == (end-start+1) {
				return
			}

			retry := newRetry(cfg)
			var lastErr error
			for attempt := 0; attempt <= cfg.Retries; attempt++ {
				// Abort promptly if canceled
				select {
				case <-ctx.Done():
					return
				default:
				}

				rq, _ := http.NewRequestWithContext(ctx, "GET", it.URL, nil)
				addAuth(rq, token)
				rq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

				rs, err := httpc.Do(rq)
				if err != nil {
					lastErr = err
				} else if rs.StatusCode != 206 {
					lastErr = fmt.Errorf("range not supported (status %s)", rs.Status)
				} else {
					out, err := os.Create(tmp)
					if err != nil {
						lastErr = err
					} else {
						_, lastErr = io.Copy(out, rs.Body) // returns fast on ctx cancel
						out.Close()
					}
					rs.Body.Close()
					if lastErr == nil {
						return
					}
				}
				if attempt < cfg.Retries {
					emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
					if d := retry.Next(); !sleepCtx(ctx, d) {
						return
					}
				}
			}
			select {
			case errCh <- lastErr:
			default:
			}
		}()
	}

	// Emit periodic progress (stops on ctx cancel)
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var bytes int64
				for _, p := range tmpParts {
					if fi, err := os.Stat(p); err == nil {
						bytes += fi.Size()
					}
				}
				emit(ProgressEvent{Event: "file_progress", Path: it.RelativePath, Bytes: bytes, Total: it.Size})
			}
		}
	}()

	wg.Wait()
	// Non-blocking error read (if any part failed)
	select {
	case e := <-errCh:
		return e
	default:
	}

	// Assemble parts
	out, err := os.Create(dst + ".part")
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		p := tmpParts[i]
		in, err := os.Open(p)
		if err != nil {
			out.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			return err
		}
		in.Close()
	}
	out.Close()
	if err := os.Rename(dst+".part", dst); err != nil {
		return err
	}
	for _, p := range tmpParts {
		_ = os.Remove(p)
	}
	return nil
}

// ------------------------
// Skip logic (filesystem-based; no persistent metadata)
// ------------------------

func shouldSkipLocal(it PlanItem, dst string) (bool, string, error) {
	fi, err := os.Stat(dst)
	if err != nil {
		// no file
		return false, "", nil
	}
	// Quick size check first: if known and different, don't skip
	if it.Size > 0 && fi.Size() != it.Size {
		return false, "", nil
	}
	// LFS with known sha: compute and compare
	if it.LFS && it.SHA256 != "" {
		if err := verifySHA256(dst, it.SHA256); err == nil {
			return true, "sha256 match", nil
		}
		// size matched but sha mismatched -> re-download
		return false, "", nil
	}
	// Non-LFS (or unknown sha): size match is sufficient
	if it.Size > 0 && fi.Size() == it.Size {
		return true, "size match", nil
	}
	return false, "", nil
}

// ------------------------
// Small utilities
// ------------------------

type backoff struct {
	next   time.Duration
	max    time.Duration
	mult   float64
	jitter time.Duration
}

func newRetry(cfg Settings) *backoff {
	init := 400 * time.Millisecond
	max := 10 * time.Second
	if d, err := time.ParseDuration(defaultString(cfg.BackoffInitial, "400ms")); err == nil {
		init = d
	}
	if d, err := time.ParseDuration(defaultString(cfg.BackoffMax, "10s")); err == nil {
		max = d
	}
	return &backoff{next: init, max: max, mult: 1.6, jitter: 120 * time.Millisecond}
}

func (b *backoff) Next() time.Duration {
	d := b.next + time.Duration(int64(b.jitter)*int64(time.Now().UnixNano()%3)/2)
	b.next = time.Duration(float64(b.next) * b.mult)
	if b.next > b.max {
		b.next = b.max
	}
	return d
}

// sleepCtx waits for d or returns false if ctx is canceled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func verifySHA256(path string, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s got %s", expected, sum)
	}
	return nil
}

func parseSizeString(s string, def int64) (int64, error) {
	if s == "" {
		return def, nil
	}
	var n float64
	var unit string
	_, err := fmt.Sscanf(strings.ToUpper(strings.TrimSpace(s)), "%f%s", &n, &unit)
	if err != nil {
		var nn int64
		if _, e2 := fmt.Sscanf(s, "%d", &nn); e2 == nil {
			return nn, nil
		}
		return 0, err
	}
	switch unit {
	case "B", "":
		return int64(n), nil
	case "KB":
		return int64(n * 1000), nil
	case "MB":
		return int64(n * 1000 * 1000), nil
	case "GB":
		return int64(n * 1000 * 1000 * 1000), nil
	case "KIB":
		return int64(n * 1024), nil
	case "MIB":
		return int64(n * 1024 * 1024), nil
	case "GIB":
		return int64(n * 1024 * 1024 * 1024), nil
	default:
		return 0, fmt.Errorf("unknown unit %q", unit)
	}
}

func defaultString(s string, def string) string {
	if s == "" {
		return def
	}
	return s
}
