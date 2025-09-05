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
// Downloader entrypoint (v2)
// ------------------------

func Download(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Repo == "" {
		return errors.New("missing repo")
	}
	if opts.OutputDir == "" {
		opts.OutputDir = "Storage"
	}
	if opts.Revision == "" {
		opts.Revision = "main"
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.MaxActiveDownloads <= 0 {
		opts.MaxActiveDownloads = runtime.GOMAXPROCS(0)
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}

	thresholdBytes, err := parseSizeString(opts.MultipartThreshold, 32<<20) // 32MiB default
	if err != nil {
		return fmt.Errorf("invalid --multipart-threshold: %w", err)
	}
	// backoffInitial, err := time.ParseDuration(defaultString(opts.BackoffInitial, "400ms"))
	// if err != nil {
	// 	return fmt.Errorf("invalid --backoff-initial: %w", err)
	// }
	// backoffMax, err := time.ParseDuration(defaultString(opts.BackoffMax, "10s"))
	// if err != nil {
	// 	return fmt.Errorf("invalid --backoff-max: %w", err)
	// }

	httpc := buildHTTPClient()
	authToken := strings.TrimSpace(opts.Token)

	emit := func(ev ProgressEvent) {
		if opts.Progress != nil {
			if ev.Time.IsZero() {
				ev.Time = time.Now()
			}
			if ev.Repo == "" {
				ev.Repo = opts.Repo
			}
			if ev.Revision == "" {
				ev.Revision = opts.Revision
			}
			opts.Progress(ev)
		}
	}

	emit(ProgressEvent{Event: "scan_start", Message: "scanning repo"})

	// Build the plan
	plan, planErr := scanRepo(ctx, httpc, authToken, opts)
	if planErr != nil {
		return planErr
	}

	if opts.DryRun {
		// Print plan and exit
		if opts.PlanFormat == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(plan); err != nil {
				return err
			}
		} else {
			fmt.Printf("Plan for %s@%s (%d files):\n", opts.Repo, opts.Revision, len(plan.Items))
			for _, it := range plan.Items {
				fmt.Printf("  %s  %8d  lfs=%t\n", it.RelativePath, it.Size, it.LFS)
			}
		}
		return nil
	}

	// Overall concurrency limiter
	type token struct{}
	lim := make(chan token, opts.MaxActiveDownloads)

	// Load metadata
	metaPath := filepath.Join(destinationBase(opts), ".hfdownloader.meta.json")
	meta, _ := loadMeta(metaPath)
	if meta == nil {
		meta = &metaFile{Files: map[string]fileMeta{}}
	}

	// Create destination root
	if err := os.MkdirAll(destinationBase(opts), 0o755); err != nil {
		return err
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Items))
	for _, item := range plan.Items {
		it := item // capture
		emit(ProgressEvent{Event: "plan_item", Path: it.RelativePath, Total: it.Size})
		lim <- token{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-lim }() // release

			// Determine destination path
			dst := filepath.Join(destinationBase(opts), it.RelativePath)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				errCh <- err
				return
			}

			// Skip logic based on verify mode & metadata
			alreadyOK, reason, err := shouldSkip(ctx, httpc, authToken, opts, meta, it, dst)
			if err != nil {
				errCh <- err
				return
			}
			if alreadyOK && !opts.Overwrite {
				emit(ProgressEvent{Event: "file_done", Path: it.RelativePath, Message: "skip (" + reason + ")"})
				return
			}

			emit(ProgressEvent{Event: "file_start", Path: it.RelativePath, Total: it.Size})

			// Choose single/multipart
			var dlErr error
			if it.Size >= thresholdBytes && it.AcceptRanges {
				dlErr = downloadMultipart(ctx, httpc, authToken, opts, it, dst, emit)
			} else {
				dlErr = downloadSingle(ctx, httpc, authToken, opts, it, dst, emit)
			}

			if dlErr != nil {
				errCh <- fmt.Errorf("download %s: %w", it.RelativePath, dlErr)
				return
			}

			// Verify
			if opts.Verify != "none" {
				if it.LFS && it.SHA256 != "" {
					if err := verifySHA256(dst, it.SHA256); err != nil {
						errCh <- fmt.Errorf("sha256 verify failed: %s: %w", it.RelativePath, err)
						return
					}
				} else {
					switch opts.Verify {
					case "size":
						fi, err := os.Stat(dst)
						if err != nil || fi.Size() != it.Size {
							errCh <- fmt.Errorf("size mismatch for %s", it.RelativePath)
							return
						}
					case "etag", "sha256":
						etag, remoteSha, _ := headForETag(ctx, httpc, authToken, it)
						if opts.Verify == "etag" && etag != "" {
							meta.Files[it.RelativePath] = fileMeta{ETag: etag, Size: it.Size, Sha256: remoteSha}
						} else if opts.Verify == "sha256" && remoteSha != "" {
							if err := verifySHA256(dst, remoteSha); err != nil {
								errCh <- fmt.Errorf("sha256 verify failed: %s: %w", it.RelativePath, err)
								return
							}
							meta.Files[it.RelativePath] = fileMeta{Sha256: remoteSha, Size: it.Size, ETag: etag}
						}
					}
				}
			}

			emit(ProgressEvent{Event: "file_done", Path: it.RelativePath})
		}()
	}

	wg.Wait()
	close(errCh)

	// Persist metadata only if no errors
	select {
	case err := <-errCh:
		emit(ProgressEvent{Level: "error", Event: "error", Message: err.Error()})
		return err
	default:
		// no error collected
	}

	if err := saveMeta(metaPath, meta); err != nil {
		emit(ProgressEvent{Level: "warn", Event: "done", Message: "download complete (metadata save failed: " + err.Error() + ")"})
		return nil
	}

	emit(ProgressEvent{Event: "done", Message: "download complete"})
	return nil
}

// ------------------------
// Planning & API
// ------------------------

type planItem struct {
	RelativePath string `json:"path"`
	URL          string `json:"url"`
	LFS          bool   `json:"lfs"`
	SHA256       string `json:"sha256,omitempty"`
	Size         int64  `json:"size"`
	AcceptRanges bool   `json:"acceptRanges"`
}

type plan struct {
	Items []planItem `json:"items"`
}

func destinationBase(opts Options) string {
	base := filepath.Join(opts.OutputDir, opts.Repo)
	if opts.AppendFilterSubdir && len(opts.Filters) == 1 {
		base = filepath.Join(base, opts.Filters[0])
	}
	return base
}

func scanRepo(ctx context.Context, httpc *http.Client, token string, opts Options) (*plan, error) {
	var items []planItem
	// recursively walk tree
	err := walkTree(ctx, httpc, token, opts, "", func(n hfNode) error {
		if n.Type != "file" && n.Type != "blob" {
			return nil
		}
		rel := n.Path
		name := filepath.Base(rel)

		isLFS := n.LFS != nil
		if isLFS && len(opts.Filters) > 0 {
			matched := false
			for _, f := range opts.Filters {
				if strings.Contains(name, f) {
					matched = true
					break
				}
			}
			if !matched {
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
			urlStr = lfsURL(opts, rel)
		} else {
			urlStr = rawURL(opts, rel)
		}
		size := n.Size
		if size == 0 && n.LFS != nil && n.LFS.Size > 0 {
			size = n.LFS.Size
		}

		// HEAD for Accept-Ranges (best-effort)
		acceptRanges := false
		if headOK, accept := quickHeadAcceptRanges(ctx, httpc, token, urlStr); headOK {
			acceptRanges = accept
		}

		// Safely select sha256 from node or its LFS metadata (if present)
		sha := n.Sha256
		if sha == "" && n.LFS != nil {
			sha = n.LFS.Sha256
		}

		items = append(items, planItem{
			RelativePath: rel,
			URL:          urlStr,
			LFS:          isLFS,
			SHA256:       sha,
			Size:         size,
			AcceptRanges: acceptRanges,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &plan{Items: items}, nil
}

func rawURL(opts Options, path string) string {
	if opts.IsDataset {
		return fmt.Sprintf(RawDatasetFileURL, url.PathEscape(opts.Repo), url.PathEscape(opts.Revision), pathEscapeAll(path))
	}
	return fmt.Sprintf(RawModelFileURL, url.PathEscape(opts.Repo), url.PathEscape(opts.Revision), pathEscapeAll(path))
}

func lfsURL(opts Options, path string) string {
	if opts.IsDataset {
		return fmt.Sprintf(LfsDatasetResolverURL, url.PathEscape(opts.Repo), url.PathEscape(opts.Revision), pathEscapeAll(path))
	}
	return fmt.Sprintf(LfsModelResolverURL, url.PathEscape(opts.Repo), url.PathEscape(opts.Revision), pathEscapeAll(path))
}

func treeURL(opts Options, prefix string) string {
	if opts.IsDataset {
		return fmt.Sprintf(JsonDatasetFileTreeURL, url.PathEscape(opts.Repo), url.PathEscape(opts.Revision), pathEscapeAll(prefix))
	}
	return fmt.Sprintf(JsonModelsFileTreeURL, url.PathEscape(opts.Repo), url.PathEscape(opts.Revision), pathEscapeAll(prefix))
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

func walkTree(ctx context.Context, httpc *http.Client, token string, opts Options, prefix string, fn func(hfNode) error) error {
	reqURL := treeURL(opts, prefix)
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		base := AgreementModelURL
		if opts.IsDataset {
			base = AgreementDatasetURL
		}
		return fmt.Errorf("401 unauthorized: repo requires token or you do not have access (visit %s)", fmt.Sprintf(base, opts.Repo))
	}
	if resp.StatusCode == 403 {
		base := AgreementModelURL
		if opts.IsDataset {
			base = AgreementDatasetURL
		}
		return fmt.Errorf("403 forbidden: please accept the repository terms: %s", fmt.Sprintf(base, opts.Repo))
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
			if err := walkTree(ctx, httpc, token, opts, n.Path, fn); err != nil {
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

func headForETag(ctx context.Context, httpc *http.Client, token string, it planItem) (etag string, remoteSha string, _ error) {
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

func downloadSingle(ctx context.Context, httpc *http.Client, token string, opts Options, it planItem, dst string, emit func(ProgressEvent)) error {
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer out.Close()

	retry := newRetry(opts)
	var lastErr error
	for attempt := 0; attempt <= opts.Retries; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", it.URL, nil)
		addAuth(req, token)
		resp, err := httpc.Do(req)
		if err != nil {
			lastErr = err
		} else {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("bad status: %s", resp.Status)
			} else {
				_, cerr := io.Copy(out, resp.Body)
				resp.Body.Close()
				if cerr == nil {
					out.Close()
					return os.Rename(tmp, dst)
				}
				lastErr = cerr
			}
		}
		if attempt < opts.Retries {
			emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
			time.Sleep(retry.Next())
			continue
		}
		break
	}
	return lastErr
}

func downloadMultipart(ctx context.Context, httpc *http.Client, token string, opts Options, it planItem, dst string, emit func(ProgressEvent)) error {
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
		// Fallback to single
		return downloadSingle(ctx, httpc, token, opts, it, dst, emit)
	}

	// Plan parts
	n := opts.Concurrency
	chunk := it.Size / int64(n)
	if chunk <= 0 {
		chunk = it.Size
		n = 1
	}
	tmpParts := make([]string, n)

	// Prepare part names
	for i := 0; i < n; i++ {
		tmpParts[i] = fmt.Sprintf("%s.part-%02d", dst, i)
	}

	// Download parts in parallel
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
			// Resume: if resume enabled and file exists with expected size, skip re-download
			if fi, err := os.Stat(tmp); err == nil && fi.Size() == (end-start+1) && opts.Resume {
				return
			}

			retry := newRetry(opts)
			var lastErr error
			for attempt := 0; attempt <= opts.Retries; attempt++ {
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
						_, lastErr = io.Copy(out, rs.Body)
						out.Close()
					}
					rs.Body.Close()
					if lastErr == nil {
						return
					}
				}
				if attempt < opts.Retries {
					emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
					time.Sleep(retry.Next())
				}
			}
			errCh <- lastErr
		}()
	}

	// Emit periodic progress
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				var bytes int64
				for _, p := range tmpParts {
					if fi, err := os.Stat(p); err == nil {
						bytes += fi.Size()
					}
				}
				emit(ProgressEvent{Event: "file_progress", Path: it.RelativePath, Bytes: bytes, Total: it.Size})
			case <-done:
				return
			}
		}
	}()

	wg.Wait()
	close(done)

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

	// Atomic rename
	if err := os.Rename(dst+".part", dst); err != nil {
		return err
	}
	// Cleanup parts
	for _, p := range tmpParts {
		_ = os.Remove(p)
	}
	return nil
}

// ------------------------
// Skip logic & metadata
// ------------------------

type fileMeta struct {
	ETag   string `json:"etag,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}
type metaFile struct {
	Files map[string]fileMeta `json:"files"`
}

func loadMeta(path string) (*metaFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mf metaFile
	if err := json.Unmarshal(b, &mf); err != nil {
		return nil, err
	}
	if mf.Files == nil {
		mf.Files = map[string]fileMeta{}
	}
	return &mf, nil
}
func saveMeta(path string, mf *metaFile) error {
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(mf, "", "  ")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func shouldSkip(ctx context.Context, httpc *http.Client, token string, opts Options, meta *metaFile, it planItem, dst string) (bool, string, error) {
	if opts.Overwrite {
		return false, "", nil
	}
	fi, err := os.Stat(dst)
	if err != nil {
		return false, "", nil // does not exist
	}

	// Prefer metadata when available
	if m, ok := meta.Files[it.RelativePath]; ok {
		if opts.Verify == "etag" && m.ETag != "" {
			etag, _, _ := headForETag(ctx, httpc, token, it)
			if etag != "" && etag == m.ETag {
				return true, "etag match", nil
			}
		}
		if it.LFS && it.SHA256 != "" && m.Sha256 == it.SHA256 {
			return true, "sha256 match", nil
		}
		if m.Size > 0 && fi.Size() == m.Size {
			return true, "size match", nil
		}
	}

	// Fallback to size check
	if fi.Size() == it.Size && it.Size > 0 {
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

func newRetry(opts Options) *backoff {
	init := 400 * time.Millisecond
	max := 10 * time.Second
	if d, err := time.ParseDuration(defaultString(opts.BackoffInitial, "400ms")); err == nil {
		init = d
	}
	if d, err := time.ParseDuration(defaultString(opts.BackoffMax, "10s")); err == nil {
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

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func defaultString(s string, def string) string {
	if s == "" {
		return def
	}
	return s
}
