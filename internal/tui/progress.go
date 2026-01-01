// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// LiveRenderer renders a cross-platform, adaptive, colorful progress table.
// - Uses ANSI when available; plain text fallback otherwise.
// - Adapts to terminal width/height.
// - Shows job header + totals + active file rows with progress bars.
type LiveRenderer struct {
	job hfdownloader.Job
	cfg hfdownloader.Settings

	mu         sync.Mutex
	start      time.Time
	events     chan hfdownloader.ProgressEvent
	done       chan struct{}
	stopped    bool
	hideCur    bool
	supports   bool // ANSI + interactive
	noColor    bool
	lastRedraw time.Time

	// aggregate
	totalFiles int
	totalBytes int64

	// per-file state
	files map[string]*fileState

	// overall rolling speed (EMA smoothed)
	lastTotalBytes int64
	lastTick       time.Time
	smoothedSpeed  float64 // EMA smoothed overall speed
}

type fileState struct {
	path   string
	total  int64
	bytes  int64
	status string // "queued","downloading","done","skip","error"
	err    string

	// rolling speed (EMA smoothed)
	lastBytes     int64
	lastTime      time.Time
	smoothedSpeed float64 // EMA smoothed per-file speed

	// metrics
	started time.Time
}

// EMA smoothing factor (0.1 = very smooth, 0.5 = responsive)
const speedSmoothingFactor = 0.3

func smoothSpeed(current, previous float64) float64 {
	if previous == 0 {
		return current
	}
	// Exponential moving average
	return speedSmoothingFactor*current + (1-speedSmoothingFactor)*previous
}

// NewLiveRenderer creates a new live TUI renderer.
func NewLiveRenderer(job hfdownloader.Job, cfg hfdownloader.Settings) *LiveRenderer {
	lr := &LiveRenderer{
		job:     job,
		cfg:     cfg,
		start:   time.Now(),
		events:  make(chan hfdownloader.ProgressEvent, 2048),
		done:    make(chan struct{}),
		files:   map[string]*fileState{},
		noColor: os.Getenv("NO_COLOR") != "",
	}
	// Detect interactive + ANSI support
	lr.supports = isInteractive() && ansiOkay()
	if lr.supports && !lr.noColor {
		// Hide cursor
		fmt.Fprint(os.Stdout, "\x1b[?25l")
		lr.hideCur = true
	}
	go lr.loop()
	return lr
}

// Close stops the renderer and restores the terminal.
func (lr *LiveRenderer) Close() {
	lr.mu.Lock()
	if lr.stopped {
		lr.mu.Unlock()
		return
	}
	lr.stopped = true
	close(lr.done)
	lr.mu.Unlock()
	// Wait a tick
	time.Sleep(60 * time.Millisecond)
	if lr.hideCur {
		fmt.Fprint(os.Stdout, "\x1b[?25h") // show cursor
	}
	// Final newline to separate from prompt
	fmt.Fprintln(os.Stdout)
}

// Handler returns a ProgressFunc that feeds events to the renderer.
func (lr *LiveRenderer) Handler() hfdownloader.ProgressFunc {
	return func(ev hfdownloader.ProgressEvent) {
		select {
		case lr.events <- ev:
		default:
			// Drop events if UI is congested; we keep rendering smoothly.
		}
	}
}

func (lr *LiveRenderer) loop() {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-lr.done:
			lr.render(true)
			return
		case ev := <-lr.events:
			lr.apply(ev)
		case <-ticker.C:
			lr.render(false)
		}
	}
}

func (lr *LiveRenderer) apply(ev hfdownloader.ProgressEvent) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	switch ev.Event {
	case "plan_item":
		fs := lr.ensure(ev.Path)
		fs.total = ev.Total
		fs.status = "queued"
		lr.totalFiles++
		lr.totalBytes += ev.Total
	case "file_start":
		fs := lr.ensure(ev.Path)
		fs.total = ev.Total
		fs.status = "downloading"
		if fs.started.IsZero() {
			fs.started = time.Now()
		}
	case "file_progress":
		fs := lr.ensure(ev.Path)
		// Only update total if it's provided and reasonable
		if ev.Total > 0 {
			fs.total = ev.Total
		}
		// Prefer Downloaded (cumulative) over Bytes (legacy/delta)
		if ev.Downloaded > 0 {
			fs.bytes = ev.Downloaded
		} else if ev.Bytes > 0 {
			fs.bytes = ev.Bytes
		}
		if fs.lastTime.IsZero() {
			fs.lastTime = time.Now()
			fs.lastBytes = fs.bytes
		}
	case "file_done":
		fs := lr.ensure(ev.Path)
		if strings.HasPrefix(strings.ToLower(ev.Message), "skip") {
			fs.status = "skip"
		} else {
			fs.status = "done"
		}
		fs.bytes = fs.total
	case "retry":
		// Could record attempts if you want a column
	case "error":
		fs := lr.ensure(ev.Path)
		fs.status = "error"
		fs.err = ev.Message
	case "done":
		// mark all as done if any left
	}
}

func (lr *LiveRenderer) ensure(path string) *fileState {
	if fs, ok := lr.files[path]; ok {
		return fs
	}
	fs := &fileState{path: path}
	lr.files[path] = fs
	return fs
}

func (lr *LiveRenderer) render(final bool) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	// compute size
	w, h := termSize()
	minW := 70
	if w < minW {
		w = minW
	}
	if h < 12 {
		h = 12
	}

	// aggregate totals - compute from individual file states (more accurate)
	var aggBytes int64
	var aggTotal int64
	var active []*fileState
	var doneCnt, skipCnt, errCnt int
	for _, fs := range lr.files {
		if fs.status == "downloading" {
			active = append(active, fs)
		}
		if fs.status == "done" {
			doneCnt++
		}
		if fs.status == "skip" {
			skipCnt++
		}
		if fs.status == "error" {
			errCnt++
		}
		// Accumulate total from each file's known total (more accurate than plan_item accumulation)
		aggTotal += fs.total
		if fs.bytes > 0 {
			aggBytes += fs.bytes
		} else if fs.status == "done" || fs.status == "skip" {
			aggBytes += fs.total
		}
	}
	// Use computed total if we have files, otherwise fall back to plan_item total
	if aggTotal > 0 {
		lr.totalBytes = aggTotal
	}
	queued := lr.totalFiles - (len(active) + doneCnt + skipCnt + errCnt)
	if queued < 0 {
		queued = 0
	}

	// overall speed (EMA smoothed)
	now := time.Now()
	if !lr.lastTick.IsZero() && now.After(lr.lastTick) {
		deltaB := aggBytes - lr.lastTotalBytes
		deltaT := now.Sub(lr.lastTick).Seconds()
		if deltaT > 0.05 { // Only update if enough time passed (50ms min)
			instantSpeed := float64(deltaB) / deltaT
			if instantSpeed >= 0 { // Ignore negative deltas (can happen with rounding)
				lr.smoothedSpeed = smoothSpeed(instantSpeed, lr.smoothedSpeed)
			}
			lr.lastTick = now
			lr.lastTotalBytes = aggBytes
		}
	} else if lr.lastTick.IsZero() {
		lr.lastTick = now
		lr.lastTotalBytes = aggBytes
	}
	speed := lr.smoothedSpeed

	// overall ETA
	var etaStr string
	if speed > 0 && lr.totalBytes > 0 && aggBytes < lr.totalBytes {
		rem := float64(lr.totalBytes-aggBytes) / speed
		etaStr = fmtDuration(time.Duration(rem) * time.Second)
	} else {
		etaStr = "—"
	}

	// Clear + render (ANSI) or plain
	if lr.supports {
		// Clear screen and go home
		fmt.Fprint(os.Stdout, "\x1b[H\x1b[2J")
	}

	// Header
	rev := lr.job.Revision
	if rev == "" {
		rev = "main"
	}
	jobline := fmt.Sprintf("Repo: %s   Rev: %s   Dataset: %v", lr.job.Repo, rev, lr.job.IsDataset)
	fmt.Fprintln(os.Stdout, colorize(bold(jobline), "fg=cyan", lr))
	cfgline := fmt.Sprintf("Out: %s   Conns: %d   MaxActive: %d   Verify: %s   Retries: %d   Threshold: %s",
		lr.cfg.OutputDir, lr.cfg.Concurrency, lr.cfg.MaxActiveDownloads, lr.cfg.Verify, lr.cfg.Retries, lr.cfg.MultipartThreshold)
	fmt.Fprintln(os.Stdout, dim(cfgline))

	// Totals line with bar
	prog := float64(0)
	if lr.totalBytes > 0 {
		prog = float64(aggBytes) / float64(lr.totalBytes)
		if prog < 0 {
			prog = 0
		}
		if prog > 1 {
			prog = 1
		}
	}
	bar := renderBar(int(float64(w)*0.4), prog, lr) // 40% of width
	speedStr := humanBytes(int64(speed)) + "/s"
	fmt.Fprintf(os.Stdout, "%s  %s  %s/%s  %s  ETA %s\n",
		colorize(bar, "fg=green", lr),
		percent(prog),
		humanBytes(aggBytes), humanBytes(lr.totalBytes),
		speedStr, etaStr,
	)

	// Table header
	fmt.Fprintln(os.Stdout)
	cols := []string{"Status", "File", "Progress", "Speed", "ETA"}
	fmt.Fprintln(os.Stdout, headerRow(cols, w))

	// Determine rows to show
	maxRows := h - 8 // header+totals+footer allowance
	if maxRows < 3 {
		maxRows = 3
	}

	// Sort active by bytes desc (more movement first)
	sort.Slice(active, func(i, j int) bool { return active[i].bytes > active[j].bytes })

	// Compose rows
	shown := 0
	for _, fs := range active {
		if shown >= maxRows {
			break
		}
		shown++
		fmt.Fprintln(os.Stdout, renderFileRow(fs, w, lr))
	}

	// If space remains, show recently finished or queued small set
	if shown < maxRows {
		var rest []*fileState
		for _, fs := range lr.files {
			if fs.status == "done" || fs.status == "skip" || fs.status == "error" {
				rest = append(rest, fs)
			}
		}
		sort.Slice(rest, func(i, j int) bool { return rest[i].started.After(rest[j].started) })
		for _, fs := range rest {
			if shown >= maxRows {
				break
			}
			fmt.Fprintln(os.Stdout, renderFileRow(fs, w, lr))
			shown++
		}
	}

	// Footer hint
	if lr.supports {
		fmt.Fprintln(os.Stdout, dim(fmt.Sprintf("Press Ctrl+C to cancel • %s %s",
			runtime.GOOS, runtime.GOARCH)))
	}
}

func renderFileRow(fs *fileState, w int, lr *LiveRenderer) string {
	// column widths (adaptive)
	statusW := 9
	speedW := 10
	etaW := 9
	// remaining for filename + progress
	remain := w - (statusW + speedW + etaW + 8) // gutters
	if remain < 20 {
		remain = 20
	}
	// split for file/progress
	fileW := int(float64(remain) * 0.50)
	if fileW < 18 {
		fileW = 18
	}
	progressW := remain - fileW

	// status
	var st, col string
	switch fs.status {
	case "downloading":
		st, col = "▶", "fg=yellow"
	case "done":
		st, col = "✓", "fg=green"
	case "skip":
		st, col = "•", "fg=blue"
	case "error":
		st, col = "×", "fg=red"
	default:
		st, col = "…", "fg=magenta"
	}
	status := pad(colorize(st+" "+fs.status, col, lr), statusW)

	// filename
	name := ellipsizeMiddle(fs.path, fileW)

	// progress
	var p float64
	if fs.total > 0 {
		p = float64(fs.bytes) / float64(fs.total)
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
	}
	bar := renderBar(progressW-18, p, lr) // leave room for numbers
	progTxt := fmt.Sprintf(" %s/%s %s", humanBytes(fs.bytes), humanBytes(fs.total), percent(p))
	progress := bar + progTxt
	if utf8.RuneCountInString(progress) > progressW {
		// simple cut if needed
		runes := []rune(progress)
		progress = string(runes[:progressW])
	}

	// speed (per-file, EMA smoothed)
	now := time.Now()
	if !fs.lastTime.IsZero() {
		dt := now.Sub(fs.lastTime).Seconds()
		if dt > 0.05 { // Only update if enough time passed (50ms min)
			delta := fs.bytes - fs.lastBytes
			instantSpeed := float64(delta) / dt
			if instantSpeed >= 0 {
				fs.smoothedSpeed = smoothSpeed(instantSpeed, fs.smoothedSpeed)
			}
			fs.lastTime = now
			fs.lastBytes = fs.bytes
		}
	} else {
		fs.lastTime = now
		fs.lastBytes = fs.bytes
	}
	speed := fs.smoothedSpeed
	speedTxt := pad(humanBytes(int64(speed))+"/s", speedW)

	// eta (use smoothed speed for stable ETA)
	eta := "—"
	if speed > 0 && fs.total > 0 && fs.bytes < fs.total {
		rem := float64(fs.total-fs.bytes) / speed
		eta = fmtDuration(time.Duration(rem) * time.Second)
	}
	etaTxt := pad(eta, etaW)

	return fmt.Sprintf("%s  %s  %s  %s  %s", status, pad(name, fileW), progress, speedTxt, etaTxt)
}

func headerRow(cols []string, w int) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = bold(c)
	}
	s := strings.Join(parts, "  ")
	if utf8.RuneCountInString(s) > w {
		runes := []rune(s)
		return string(runes[:w])
	}
	return s
}

func ellipsizeMiddle(s string, w int) string {
	if w <= 3 || utf8.RuneCountInString(s) <= w {
		return pad(s, w)
	}
	runes := []rune(s)
	half := (w - 3) / 2
	if 2*half+3 > len(runes) {
		return pad(s, w)
	}
	return pad(string(runes[:half])+"..."+string(runes[len(runes)-half:]), w)
}

func pad(s string, w int) string {
	r := utf8.RuneCountInString(s)
	if r >= w {
		return s
	}
	return s + strings.Repeat(" ", w-r)
}

func renderBar(width int, p float64, lr *LiveRenderer) string {
	if width < 3 {
		width = 3
	}
	filled := int(p * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return bar
}

func percent(p float64) string {
	return fmt.Sprintf("%3.0f%%", p*100)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 6 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func termSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 100, 30
	}
	return w, h
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func ansiOkay() bool {
	if runtime.GOOS == "windows" {
		// On modern Windows 10+ terminals this is typically fine.
		// Fall back to plain output when TERM=dumb or NO_COLOR set.
	}
	termEnv := strings.ToLower(os.Getenv("TERM"))
	if termEnv == "dumb" {
		return false
	}
	return true
}

func colorize(s, style string, lr *LiveRenderer) string {
	if lr.noColor || !lr.supports {
		return s
	}
	switch style {
	case "fg=green":
		return "\x1b[32m" + s + "\x1b[0m"
	case "fg=yellow":
		return "\x1b[33m" + s + "\x1b[0m"
	case "fg=red":
		return "\x1b[31m" + s + "\x1b[0m"
	case "fg=blue":
		return "\x1b[34m" + s + "\x1b[0m"
	case "fg=magenta":
		return "\x1b[35m" + s + "\x1b[0m"
	case "fg=cyan":
		return "\x1b[36m" + s + "\x1b[0m"
	default:
		return s
	}
}

func bold(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
func dim(s string) string  { return "\x1b[2m" + s + "\x1b[0m" }

