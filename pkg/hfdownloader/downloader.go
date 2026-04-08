// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// countingReader wraps an io.Reader and atomically increments a counter by
// the number of bytes read. Used to track in-flight download bytes across
// multiple concurrent workers without holding any locks.
type countingReader struct {
	r       io.Reader
	counter *int64
}

func (cr *countingReader) Read(p []byte) (n int, err error) {
	n, err = cr.r.Read(p)
	if n > 0 {
		atomic.AddInt64(cr.counter, int64(n))
	}
	return
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

	partSize, err := parseSizeString(cfg.PartSize, 32<<20)
	if err != nil {
		return fmt.Errorf("invalid part-size: %w", err)
	}
	if partSize < 1<<20 {
		partSize = 1 << 20 // floor at 1 MiB to avoid degenerate behaviour
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
			var computedSHA string
			var dlErr error
			if it.Size >= thresholdBytes && it.AcceptRanges {
				computedSHA, dlErr = downloadMultipart(fileCtx, httpc, cfg.Token, job, cfg, itForIO, dst, emit, partSize)
			} else {
				computedSHA, dlErr = downloadSingle(fileCtx, httpc, cfg.Token, job, cfg, itForIO, dst, emit)
			}
			if dlErr != nil {
				select {
				case errCh <- fmt.Errorf("download %s: %w", finalRel, dlErr):
				default:
				}
				return
			}

			// Verify after download — use the hash computed during streaming to
			// avoid a full second read of the file.
			if it.LFS && it.SHA256 != "" {
				if !strings.EqualFold(computedSHA, it.SHA256) {
					select {
					case errCh <- fmt.Errorf("sha256 verify failed: %s: expected %s got %s", finalRel, it.SHA256, computedSHA):
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
				_, remoteSha, _ := headForETag(fileCtx, httpc, cfg.Token, itForIO)
				if remoteSha != "" && !strings.EqualFold(computedSHA, remoteSha) {
					select {
					case errCh <- fmt.Errorf("sha256 verify failed: %s: expected %s got %s", finalRel, remoteSha, computedSHA):
					default:
					}
					return
				}
			}

			// For HF Cache mode: move to blob and create symlinks.
			// Pass computedSHA so StoreDownloadedFile skips its own re-read.
			var finalSHA256 string
			if useHFCache {
				result, err := repoDir.StoreDownloadedFile(dst, it.RelativePath, plan.Commit, computedSHA, filterSubdir, cfg.NoFriendlyView)
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

	// Drain errors
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

// downloadSingle downloads a file in a single request and returns the
// SHA-256 hash computed incrementally during the download.
func downloadSingle(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings, it PlanItem, dst string, emit func(ProgressEvent)) (string, error) {
	tmp := dst + ".part"

	retry := newRetry(cfg)
	var lastErr error

	for attempt := 0; attempt <= cfg.Retries; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
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
				resp.Body.Close()
			} else {
				// Create/truncate the .part file fresh for each attempt so that
				// a partial write from a failed attempt doesn't corrupt the retry.
				out, err := os.Create(tmp)
				if err != nil {
					resp.Body.Close()
					return "", err
				}

				h := sha256.New()
				pr := newProgressReader(resp.Body, it.Size, it.RelativePath, emit)
				_, cerr := io.Copy(io.MultiWriter(out, h), pr)
				out.Close()
				resp.Body.Close()
				if cerr == nil {
					if err := os.Rename(tmp, dst); err != nil {
						return "", err
					}
					return hex.EncodeToString(h.Sum(nil)), nil
				}
				lastErr = cerr
			}
		}

		if attempt < cfg.Retries {
			emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
			if d := retry.Next(); !sleepCtx(ctx, d) {
				return "", ctx.Err()
			}
		}
	}
	return "", lastErr
}

// fetchPart downloads a single byte range, returns its bytes, and increments
// networkBytes atomically as each chunk arrives — allowing a ticker goroutine
// to report real-time progress across all concurrent workers.
func fetchPart(ctx context.Context, httpc *http.Client, token string, it PlanItem, start, end int64, cfg Settings, emit func(ProgressEvent), networkBytes *int64) ([]byte, error) {
	retry := newRetry(cfg)
	var lastErr error
	for attempt := 0; attempt <= cfg.Retries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
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
			rs.Body.Close()
		} else {
			data, err := io.ReadAll(&countingReader{r: rs.Body, counter: networkBytes})
			rs.Body.Close()
			if err == nil {
				return data, nil
			}
			lastErr = err
		}
		if attempt < cfg.Retries {
			emit(ProgressEvent{Event: "retry", Path: it.RelativePath, Attempt: attempt + 1, Message: lastErr.Error()})
			if d := retry.Next(); !sleepCtx(ctx, d) {
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

// downloadMultipart downloads a file using parallel range requests.
//
// Parts are sized according to partSize and fetched into memory by a pool of
// cfg.Concurrency workers. Peak in-memory data is capped at 512 MiB
// regardless of partSize — on very large parts the semaphore automatically
// reduces concurrency to stay within that limit.
//
// A sequential writer consumes parts in index order, piping each directly
// to the output file while computing the SHA-256 hash in the same pass.
// No per-part temp files are created on disk.
//
// A 200 ms ticker reports real-time download progress as bytes arrive from
// the network, independent of how fast the writer flushes them to disk.
func downloadMultipart(ctx context.Context, httpc *http.Client, token string, job Job, cfg Settings, it PlanItem, dst string, emit func(ProgressEvent), partSize int64) (string, error) {
	// HEAD to resolve size and confirm range support.
	req, _ := http.NewRequestWithContext(ctx, "HEAD", it.URL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
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

	numParts := int((it.Size + partSize - 1) / partSize)

	type partResult struct {
		data        []byte
		err         error
		semAcquired bool // true when worker acquired a sem slot before fetching
	}
	// One buffered slot per part so a completed worker never blocks waiting
	// for the sequential writer to catch up.
	results := make([]chan partResult, numParts)
	for i := range results {
		results[i] = make(chan partResult, 1)
	}

	// Inner context cancelled when the writer stops (on error or after the
	// last part), which aborts in-flight HTTP requests promptly.
	dlCtx, dlCancel := context.WithCancel(ctx)
	defer dlCancel()

	// sem bounds how many parts can have their data in memory at once.
	// Workers acquire a slot before calling fetchPart; the writer releases
	// it after consuming (writing) the part's data.
	// The ceiling is cfg.Concurrency*2 slots OR however many fit in 512 MiB —
	// whichever is smaller — so peak RAM stays predictable regardless of partSize.
	// On cancellation the worker skips acquisition and sends an error
	// result instead (semAcquired=false), so the writer never releases for it.
	const maxInFlightMemory = 512 * 1024 * 1024 // 512 MiB hard cap
	maxInFlight := cfg.Concurrency * 2
	if memCap := int(maxInFlightMemory / partSize); memCap < maxInFlight {
		maxInFlight = memCap
	}
	if maxInFlight < 1 {
		maxInFlight = 1
	}
	if maxInFlight > numParts {
		maxInFlight = numParts
	}
	sem := make(chan struct{}, maxInFlight)

	// networkBytes is incremented atomically by countingReader inside fetchPart
	// as bytes arrive off the wire. The progress ticker below reads it.
	var networkBytes int64

	// Work queue pre-filled with all parts, then closed so workers exit
	// naturally once the queue is empty.
	type workItem struct {
		idx        int
		start, end int64
	}
	workCh := make(chan workItem, numParts)
	for i := 0; i < numParts; i++ {
		start := int64(i) * partSize
		end := start + partSize - 1
		if end >= it.Size {
			end = it.Size - 1
		}
		workCh <- workItem{i, start, end}
	}
	close(workCh)

	// Fixed-size worker pool: at most cfg.Concurrency goroutines running at once.
	workers := cfg.Concurrency
	if workers > numParts {
		workers = numParts
	}
	var dlWg sync.WaitGroup
	for w := 0; w < workers; w++ {
		dlWg.Add(1)
		go func() {
			defer dlWg.Done()
			for wi := range workCh {
				// Acquire a memory slot before fetching.
				// On cancellation: send an error immediately (semAcquired=false)
				// and continue draining workCh so every channel gets exactly one
				// send — the writer won't deadlock waiting for a result.
				select {
				case sem <- struct{}{}:
				case <-dlCtx.Done():
					results[wi.idx] <- partResult{nil, dlCtx.Err(), false}
					continue
				}
				data, err := fetchPart(dlCtx, httpc, token, it, wi.start, wi.end, cfg, emit, &networkBytes)
				results[wi.idx] <- partResult{data, err, true}
			}
		}()
	}

	// Progress ticker: emit file_progress events as bytes stream in from the
	// network. This runs independently of the writer, giving smooth feedback
	// even while large parts are still being fetched.
	go func() {
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-dlCtx.Done():
				return
			case <-t.C:
				emit(ProgressEvent{
					Event:      "file_progress",
					Path:       it.RelativePath,
					Downloaded: atomic.LoadInt64(&networkBytes),
					Total:      it.Size,
				})
			}
		}
	}()

	// Sequential writer: consume results in index order, pipe to dst + hasher.
	out, err := os.Create(dst + ".part")
	if err != nil {
		dlCancel()
		dlWg.Wait()
		return "", err
	}

	h := sha256.New()
	mw := io.MultiWriter(out, h)
	var writeErr error
	for i := 0; i < numParts; i++ {
		res := <-results[i]
		if res.semAcquired {
			<-sem // release slot: this part's data is about to be consumed/freed
		}
		if res.err != nil {
			writeErr = res.err
			dlCancel()
			break
		}
		if _, err := mw.Write(res.data); err != nil {
			writeErr = err
			dlCancel()
			break
		}
		res.data = nil // allow GC of the chunk
	}
	out.Close()
	dlWg.Wait()

	if writeErr != nil {
		_ = os.Remove(dst + ".part")
		return "", writeErr
	}

	// Emit one final progress event so the UI always reaches 100% received,
	// even on very fast connections where the 200ms ticker never fired.
	emit(ProgressEvent{
		Event:      "file_progress",
		Path:       it.RelativePath,
		Downloaded: atomic.LoadInt64(&networkBytes),
		Total:      it.Size,
	})

	computedSHA := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(dst+".part", dst); err != nil {
		return "", err
	}
	return computedSHA, nil
}
