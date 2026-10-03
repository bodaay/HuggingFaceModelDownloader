// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// progressReader wraps an io.Reader and emits progress events during reads.
type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	path       string
	emit       func(ProgressEvent)
	lastEmit   time.Time
	interval   time.Duration
}

func newProgressReader(r io.Reader, total int64, path string, emit func(ProgressEvent)) *progressReader {
	return &progressReader{
		reader:   r,
		total:    total,
		path:     path,
		emit:     emit,
		lastEmit: time.Now(),
		interval: 200 * time.Millisecond, // Emit at most 5 times per second
	}
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.downloaded += int64(n)
		// Throttle emissions to avoid flooding
		if time.Since(pr.lastEmit) >= pr.interval || err == io.EOF {
			pr.emit(ProgressEvent{
				Event:      "file_progress",
				Path:       pr.path,
				Downloaded: pr.downloaded,
				Total:      pr.total,
			})
			pr.lastEmit = time.Now()
		}
	}
	return n, err
}

// Download scans and downloads files from a HuggingFace repo.
//
// v3.0+: Files are stored in HuggingFace Hub cache structure by default:
//   - Blobs: hub/models--{owner}--{repo}/blobs/{sha256}
//   - Snapshots: hub/models--{owner}--{repo}/snapshots/{commit}/{path} (symlinks)
//   - Friendly: models/{owner}/{repo}/{path} (symlinks)
//
// Legacy mode (OutputDir set): Falls back to flat directory structure.
//
// Cancellation: all loops/sleeps/requests are tied to ctx for fast abort.
func Download(ctx context.Context, job Job, cfg Settings, progress ProgressFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validate(job, cfg); err != nil {
		return err
	}

	// Apply defaults
	if job.Revision == "" {
		job.Revision = "main"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.MaxActiveDownloads <= 0 {
		cfg.MaxActiveDownloads = runtime.GOMAXPROCS(0)
	}

	// Determine storage mode: HF cache (new) vs flat directory (legacy)
	// Use HF cache mode when:
	// 1. --cache-dir is explicitly set, OR
	// 2. --output is NOT set (default to HF cache)
	useHFCache := cfg.CacheDir != "" || cfg.OutputDir == ""
	var hfCache *HFCache
	var repoDir *RepoDir

	if useHFCache {
		var err error
		hfCache, err = cfg.BuildHFCache()
		if err != nil {
			return fmt.Errorf("build hf cache: %w", err)
		}
		repoType := RepoTypeModel
		if job.IsDataset {
			repoType = RepoTypeDataset
		}
		repoDir, err = hfCache.Repo(job.Repo, repoType)
		if err != nil {
			return fmt.Errorf("create repo dir: %w", err)
		}
		if err := repoDir.EnsureDirs(); err != nil {
			return fmt.Errorf("ensure repo dirs: %w", err)
		}
	} else {
		// Legacy mode: use OutputDir
		if cfg.OutputDir == "" {
			cfg.OutputDir = "Storage"
		}
	}

	thresholdBytes, err := parseSizeString(cfg.MultipartThreshold, 256<<20)
	if err != nil {
		return fmt.Errorf("invalid multipart-threshold: %w", err)
	}

	httpc := buildHTTPClientWithProxy(cfg.Proxy)

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

	// Emit ALL plan_item events upfront so TUI knows total size immediately
	for _, item := range plan.Items {
		displayRel := item.RelativePath
		if job.AppendFilterSubdir && item.Subdir != "" {
			displayRel = filepath.ToSlash(filepath.Join(item.Subdir, item.RelativePath))
		}
		emit(ProgressEvent{Event: "plan_item", Path: displayRel, Total: item.Size})
	}

	// Ensure destination root exists (only for legacy mode)
	// HF cache mode already created directories via repoDir.EnsureDirs()
	if !useHFCache {
		if err := os.MkdirAll(destinationBase(job, cfg), 0o755); err != nil {
			return err
		}
	}

	// Overall concurrency limiter (ctx-aware acquisition)
	type token struct{}
	lim := make(chan token, cfg.MaxActiveDownloads)

	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Items))

	// To print "skip" only once per final path per run
	var skipOnce sync.Map

	var skippedCount int64
	var downloadedCount int64

	// Build manifest during download (thread-safe)
	// Manifest is always written unless explicitly disabled with NoManifest
	var manifestBuilder *ManifestBuilder
	var manifestMu sync.Mutex
	if useHFCache && !cfg.NoManifest {
		manifestBuilder = NewManifestBuilder(job, cfg.Command)
		manifestBuilder.SetCommit(plan.Commit)
	}

LOOP:
	for _, item := range plan.Items {
		// Stop scheduling more work once canceled
		select {
		case <-ctx.Done():
			break LOOP
		default:
		}

		it := item // capture for goroutine

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

			finalRel := it.RelativePath
			filterSubdir := ""
			if job.AppendFilterSubdir && it.Subdir != "" {
				filterSubdir = it.Subdir
				finalRel = filepath.ToSlash(filepath.Join(it.Subdir, it.RelativePath))
			}

			var dst string
			var skipCheck func() (bool, string, error)

			if useHFCache {
				// HF Cache mode: check blob existence
				skipCheck = func() (bool, string, error) {
					if it.SHA256 != "" {
						status, _, err := repoDir.CheckBlob(it.SHA256)
						if err != nil {
							return false, "", err
						}
						if status == BlobComplete {
							// Blob exists, but ensure symlinks are in place
							if err := repoDir.createSnapshotSymlink(plan.Commit, it.RelativePath, it.SHA256); err == nil {
								if !cfg.NoFriendlyView {
									repoDir.CreateFriendlySymlink(plan.Commit, it.RelativePath, filterSubdir)
								}
							}
							return true, "blob exists", nil
						}
						if status == BlobDownloading {
							return true, "downloading by another process", nil
						}
					}
					return false, "", nil
				}
				// Download to temp location, will be moved to blob later
				// Use SHA256 as temp name to avoid collisions (e.g., multiple config.json files)
				tmpName := "tmp-" + it.SHA256
				if it.SHA256 == "" {
					// Fallback: sanitize path to avoid collisions
					tmpName = "tmp-" + strings.ReplaceAll(it.RelativePath, "/", "_")
				}
				dst = filepath.Join(repoDir.BlobsDir(), tmpName)
			} else {
				// Legacy mode: flat directory structure
				base := destinationBase(job, cfg)
				dst = filepath.Join(base, finalRel)
				skipCheck = func() (bool, string, error) {
					return shouldSkipLocal(it, dst)
				}
			}

			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}

			// Check if we can skip
			alreadyOK, reason, err := skipCheck()
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if alreadyOK {
				if _, loaded := skipOnce.LoadOrStore(finalRel, struct{}{}); !loaded {
					emit(ProgressEvent{Event: "file_done", Path: finalRel, Message: "skip (" + reason + ")"})
					atomic.AddInt64(&skippedCount, 1)
					// Add to manifest (skipped files are still part of the download job)
					if manifestBuilder != nil {
						manifestMu.Lock()
						manifestBuilder.AddFile(it.RelativePath, it.SHA256, it.Size, it.LFS)
						manifestMu.Unlock()
					}
				}
				return
			}

			emit(ProgressEvent{Event: "file_start", Path: finalRel, Total: it.Size})

			// Create a copy with updated RelativePath for progress display
			itForIO := it
			itForIO.RelativePath = finalRel

			// Choose single/multipart path
			var dlErr error
			if it.Size >= thresholdBytes && it.AcceptRanges {
				dlErr = downloadMultipart(fileCtx, httpc, cfg.Token, job, cfg, itForIO, dst, emit)
			} else {
				dlErr = downloadSingle(fileCtx, httpc, cfg.Token, job, cfg, itForIO, dst, emit)
			}
			if dlErr != nil {
				select {
				case errCh <- &DownloadError{Path: finalRel, Err: dlErr}:
				default:
				}
				return
			}

			// Verify after download.
			//
			// When the plan carries an expected content SHA256 (always for LFS,
			// which also covers every multipart download since range requests
			// are only used for LFS; and for any other file a mirror annotates),
			// always verify it regardless of the configured mode — a known hash
			// is the strongest available check and catches same-size byte flips
			// that a size check cannot. Files without a known hash fall back to
			// the configured mode.
			verifyProgress := func(done int64) {
				emit(ProgressEvent{Event: "file_verify", Path: finalRel, Downloaded: done, Total: it.Size})
			}
			if verr := verifyDownloadedProgress(fileCtx, httpc, cfg, itForIO, it, dst, finalRel, verifyProgress); verr != nil {
				// A file that failed verification is corrupt: remove it so it
				// isn't left consuming disk space (the next run re-downloads).
				var ve *VerificationError
				if errors.As(verr, &ve) {
					_ = os.Remove(dst)
				}
				select {
				case errCh <- verr:
				default:
				}
				return
			}

			// For HF Cache mode: move to blob and create symlinks
			var finalSHA256 string
			if useHFCache {
				sha := it.SHA256
				result, err := repoDir.StoreDownloadedFile(dst, it.RelativePath, plan.Commit, sha, filterSubdir, cfg.NoFriendlyView)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("store file %s: %w", finalRel, err):
					default:
					}
					return
				}
				finalSHA256 = result.SHA256 // Use computed SHA256 from store result
			} else {
				finalSHA256 = it.SHA256
			}

			// Add to manifest with actual LFS info from API and final SHA256
			if manifestBuilder != nil {
				manifestMu.Lock()
				manifestBuilder.AddFile(it.RelativePath, finalSHA256, it.Size, it.LFS)
				manifestMu.Unlock()
			}

			emit(ProgressEvent{Event: "file_done", Path: finalRel})
			atomic.AddInt64(&downloadedCount, 1)
		}()
	}

	wg.Wait()
	close(errCh)

	// Drain errors. errCh is buffered to len(plan.Items) and every worker
	// sends at most one error, so collecting them all here can't block and
	// can't drop failures. We aggregate with errors.Join rather than reporting
	// only the first failure, so a multi-file run surfaces every problem (and
	// errors.Is/As still traverses each joined error).
	var errs []error
	for e := range errCh {
		if e != nil {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		joined := errors.Join(errs...)
		emit(ProgressEvent{Level: "error", Event: "error", Message: joined.Error()})
		return joined
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// For HF Cache mode: write ref file and ensure friendly directory exists
	if useHFCache && repoDir != nil {
		// Write refs/main (or the revision used)
		ref := job.Revision
		if ref == "" {
			ref = "main"
		}
		if err := repoDir.WriteRef(ref, plan.Commit); err != nil {
			emit(ProgressEvent{Level: "warn", Event: "warning", Message: fmt.Sprintf("failed to write ref: %v", err)})
		}
		// Ensure friendly directory structure exists (unless disabled)
		if !cfg.NoFriendlyView {
			if err := repoDir.EnsureFriendlyDir(); err != nil {
				emit(ProgressEvent{Level: "warn", Event: "warning", Message: fmt.Sprintf("failed to create friendly dir: %v", err)})
			}
		}
	}

	// Write/update the rebuild shell script if using HF cache (unless friendly view disabled)
	if hfCache != nil && !cfg.NoFriendlyView {
		if _, err := hfCache.WriteRebuildScript(); err != nil {
			emit(ProgressEvent{Level: "warn", Event: "warning", Message: fmt.Sprintf("failed to write rebuild script: %v", err)})
		}
	}

	// Write manifest file (hfd.yaml) if using HF cache (unless friendly view disabled)
	if manifestBuilder != nil && repoDir != nil && !cfg.NoFriendlyView {
		manifest := manifestBuilder.Build()
		if _, err := manifest.Write(repoDir.FriendlyPath()); err != nil {
			emit(ProgressEvent{Level: "warn", Event: "warning", Message: fmt.Sprintf("failed to write manifest: %v", err)})
		}
	}

	emit(ProgressEvent{
		Event:   "done",
		Message: fmt.Sprintf("download complete (downloaded %d, skipped %d)", downloadedCount, skippedCount),
	})
	return nil
}

// downloadSingle downloads a file in a single request.
//
// Resume behavior: if a .part file already exists from a previous interrupted
// run, its bytes are preserved and the HTTP request uses a Range header to
// fetch only the remaining bytes. If the server ignores the Range header and
// responds with 200 (full body), the .part file is truncated and the download
// restarts from zero.
func downloadSingle(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings, it PlanItem, dst string, emit func(ProgressEvent)) error {
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	fi, err := out.Stat()
	if err != nil {
		return err
	}
	pos := fi.Size()

	// If the partial is already exactly the right size, finalize without network.
	if it.Size > 0 && pos == it.Size {
		out.Close()
		return os.Rename(tmp, dst)
	}
	// If the partial is larger than expected (stale/corrupt), start over.
	if it.Size > 0 && pos > it.Size {
		if err := out.Truncate(0); err != nil {
			return err
		}
		pos = 0
	}
	if _, err := out.Seek(pos, io.SeekStart); err != nil {
		return err
	}
	if pos > 0 {
		emit(ProgressEvent{Event: "file_progress", Path: it.RelativePath, Downloaded: pos, Total: it.Size})
	}

	retry := newRetry(cfg)
	stall := stallTimeout(cfg)
	var lastErr error

	for attempt := 0; attempt <= cfg.Retries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Per-attempt context: the stall watchdog cancels only this attempt,
		// which then retries from the current offset.
		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		var written int64

		req, _ := http.NewRequestWithContext(attemptCtx, "GET", it.URL, nil)
		addAuth(req, token)
		if pos > 0 {
			if it.Size > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", pos, it.Size-1))
			} else {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", pos))
			}
		}

		resp, err := httpc.Do(req)
		if err != nil {
			lastErr = err
		} else {
			// If we asked for a range but the server returned the whole body,
			// throw away any existing partial bytes and start fresh.
			if pos > 0 && resp.StatusCode == http.StatusOK {
				if err := out.Truncate(0); err != nil {
					resp.Body.Close()
					cancelAttempt()
					return err
				}
				if _, err := out.Seek(0, io.SeekStart); err != nil {
					resp.Body.Close()
					cancelAttempt()
					return err
				}
				pos = 0
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("bad status: %s", resp.Status)
				resp.Body.Close()
			} else {
				guard := newStallGuard(resp.Body, stall, cancelAttempt)
				pr := newProgressReader(guard, it.Size, it.RelativePath, emit)
				pr.downloaded = pos // emitted progress reflects cumulative bytes
				n, cerr := io.Copy(out, pr)
				written = n
				guard.stop()
				resp.Body.Close()
				if cerr == nil {
					cancelAttempt()
					out.Close()
					return os.Rename(tmp, dst)
				}
				lastErr = guard.wrapErr(cerr, stall)
				// Update pos to current file position so the next retry issues
				// a Range request for the remaining bytes instead of duplicating.
				if cur, serr := out.Seek(0, io.SeekCurrent); serr == nil {
					pos = cur
				}
			}
		}
		cancelAttempt()
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt < cfg.Retries {
			emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
			if d := retry.Next(); !sleepCtx(ctx, d) {
				return ctx.Err()
			}
			// An attempt that moved data forward isn't a failure streak:
			// reset the budget so long downloads over flaky links don't die
			// after a handful of interruptions spread over hours.
			if written > 0 {
				attempt = -1
				retry = newRetry(cfg)
			}
		}
	}
	return lastErr
}

// multipartLayoutFile records how a file was split into parts so a resume
// with a different connection count doesn't stitch together parts with
// mismatched boundaries.
func multipartLayoutFile(dst string) string { return dst + ".parts" }

// prepareMultipartLayout discards existing part files that were written with a
// different split (e.g. the connections setting changed between runs). Parts
// from before layout records existed are kept when they fit the current split.
func prepareMultipartLayout(dst string, n int, size int64) error {
	layout := fmt.Sprintf("%d %d", n, size)
	prev, err := os.ReadFile(multipartLayoutFile(dst))
	stale := false
	switch {
	case err == nil:
		stale = string(prev) != layout
	case errors.Is(err, os.ErrNotExist):
		// No record: only a part index beyond the current count proves the
		// old split differed.
		_, statErr := os.Stat(fmt.Sprintf("%s.part-%02d", dst, n))
		stale = statErr == nil
	default:
		return err
	}
	if stale {
		removeMultipartParts(dst)
	}
	return os.WriteFile(multipartLayoutFile(dst), []byte(layout), 0o644)
}

// removeMultipartParts deletes all part files and the layout record for dst.
func removeMultipartParts(dst string) {
	if matches, err := filepath.Glob(globEscape(dst) + ".part-*"); err == nil {
		for _, p := range matches {
			_ = os.Remove(p)
		}
	}
	_ = os.Remove(multipartLayoutFile(dst))
}

// globEscape escapes glob metacharacters so a literal path can prefix a pattern.
func globEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)
	if runtime.GOOS == "windows" {
		// Backslash is the path separator on Windows and can't be escaped.
		r = strings.NewReplacer(`*`, `[*]`, `?`, `[?]`, `[`, `[[]`)
	}
	return r.Replace(s)
}

// downloadMultipart downloads a file using multiple parallel range requests.
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
	if err := prepareMultipartLayout(dst, n, it.Size); err != nil {
		return err
	}
	stall := stallTimeout(cfg)

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
			expected := end - start + 1

			// Open or create the part file without truncating; we may be
			// resuming from a previous interrupted run.
			out, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			defer out.Close()

			fi, err := out.Stat()
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			pos := fi.Size()
			// Already fully downloaded.
			if pos == expected {
				return
			}
			// Oversize (stale/corrupt) — reset.
			if pos > expected {
				if err := out.Truncate(0); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				pos = 0
			}
			if _, err := out.Seek(pos, io.SeekStart); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}

			retry := newRetry(cfg)
			var lastErr error

			for attempt := 0; attempt <= cfg.Retries; attempt++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Per-attempt context so the stall watchdog can abort just this
				// part's request and retry it from the current offset.
				attemptCtx, cancelAttempt := context.WithCancel(ctx)
				var written int64

				rq, _ := http.NewRequestWithContext(attemptCtx, "GET", it.URL, nil)
				addAuth(rq, token)
				rq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start+pos, end))

				rs, err := httpc.Do(rq)
				if err != nil {
					lastErr = err
				} else if rs.StatusCode != 206 {
					lastErr = fmt.Errorf("range not supported (status %s)", rs.Status)
					rs.Body.Close()
				} else {
					guard := newStallGuard(rs.Body, stall, cancelAttempt)
					n, cerr := io.Copy(out, guard)
					written = n
					guard.stop()
					rs.Body.Close()
					if cerr == nil {
						cancelAttempt()
						return
					}
					lastErr = guard.wrapErr(cerr, stall)
					// Advance pos by what we actually wrote so the next retry
					// Range request picks up from the correct offset.
					if cur, serr := out.Seek(0, io.SeekCurrent); serr == nil {
						pos = cur
					}
				}
				cancelAttempt()
				if ctx.Err() != nil {
					return
				}

				if attempt < cfg.Retries {
					emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
					if d := retry.Next(); !sleepCtx(ctx, d) {
						return
					}
					// Progress resets the retry budget (see downloadSingle).
					if written > 0 {
						attempt = -1
						retry = newRetry(cfg)
					}
				}
			}

			select {
			case errCh <- lastErr:
			default:
			}
		}()
	}

	// Emit periodic progress while parts download. The ticker is stopped
	// cleanly after wg.Wait() so it cannot observe mid-assembly state (parts
	// being deleted) and emit a bogus 0-byte progress event — the bug behind
	// the "progress jumps 2.4% ↔ 2.5% for hours" symptom in github #75.
	tickerDone := make(chan struct{})
	var tickerWG sync.WaitGroup
	tickerWG.Add(1)
	go func() {
		defer tickerWG.Done()
		t := time.NewTicker(200 * time.Millisecond) // More frequent updates for responsive UI
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tickerDone:
				return
			case <-t.C:
				var downloaded int64
				for _, p := range tmpParts {
					if fi, err := os.Stat(p); err == nil {
						downloaded += fi.Size()
					}
				}
				emit(ProgressEvent{Event: "file_progress", Path: it.RelativePath, Downloaded: downloaded, Total: it.Size})
			}
		}
	}()

	wg.Wait()

	// Stop the progress ticker and wait for it to exit before touching the
	// part files. Any in-flight tick will finish and emit one final event
	// while parts are still on disk at their real sizes.
	close(tickerDone)
	tickerWG.Wait()

	// If the context was cancelled while parts were running (pause / abort /
	// timeout), return the cancellation error immediately. Part goroutines
	// that exit via their ctx-aware retry/sleep path do NOT push to errCh,
	// so we cannot rely on the errCh drain to catch this — we must check
	// ctx.Err() explicitly. Returning here is critical: it prevents the
	// bogus "downloaded == total" progress emit below AND stops the
	// assembly loop from stitching an incomplete part set into a corrupt
	// final file and deleting the partial bytes the next resume needs.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case e := <-errCh:
		return e
	default:
	}

	// Emit one explicit full-progress reading so the caller's last observed
	// file_progress value is the full byte count, regardless of when the
	// ticker happened to last fire.
	emit(ProgressEvent{Event: "file_progress", Path: it.RelativePath, Downloaded: it.Size, Total: it.Size})

	// Assemble parts. For a multi-GB file this is a long disk-bound phase, so
	// report progress (otherwise the file looks stuck at 100%, github #88)
	// and stop promptly on cancellation, leaving the parts for a resume.
	assembled := dst + ".part"
	if err := assembleParts(ctx, assembled, tmpParts, func(done int64) {
		emit(ProgressEvent{Event: "file_assemble", Path: it.RelativePath, Downloaded: done, Total: it.Size})
	}); err != nil {
		_ = os.Remove(assembled)
		return err
	}

	if err := os.Rename(assembled, dst); err != nil {
		return err
	}

	removeMultipartParts(dst)

	return nil
}

// assembleParts concatenates parts in order into out, reporting cumulative
// bytes written through onProgress.
func assembleParts(ctx context.Context, out string, parts []string, onProgress func(done int64)) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	var base int64
	for _, p := range parts {
		in, err := os.Open(p)
		if err != nil {
			f.Close()
			return err
		}
		n, err := copyCtx(ctx, f, in, func(done int64) { onProgress(base + done) })
		in.Close()
		if err != nil {
			f.Close()
			return err
		}
		base += n
	}
	return f.Close()
}
