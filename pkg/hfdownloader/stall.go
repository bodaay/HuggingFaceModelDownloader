// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultStallTimeout is how long a response body may go without delivering a
// single byte before the attempt is abandoned and retried (github issues #87,
// #88). TCP keepalive only detects a dead peer; a server or proxy that keeps
// the connection open but stops sending would otherwise block a read forever.
const DefaultStallTimeout = 60 * time.Second

// ErrStalled reports that a transfer delivered no data for longer than the
// stall timeout. It is retryable.
var ErrStalled = errors.New("transfer stalled")

// stallTimeout returns the configured stall timeout. "0" disables the watchdog.
func stallTimeout(cfg Settings) time.Duration {
	if cfg.StallTimeout == "" {
		return DefaultStallTimeout
	}
	d, err := time.ParseDuration(cfg.StallTimeout)
	if err != nil || d < 0 {
		return DefaultStallTimeout
	}
	return d
}

// stallGuard wraps a response body and cancels the request's context when no
// bytes arrive for timeout. Cancelling the context closes the underlying
// connection (or HTTP/2 stream), which unblocks the pending Read.
type stallGuard struct {
	r       io.Reader
	last    atomic.Int64 // unix nanos of the last successful read
	stalled atomic.Bool
	done    chan struct{}
	once    sync.Once
}

func newStallGuard(r io.Reader, timeout time.Duration, cancel context.CancelFunc) *stallGuard {
	g := &stallGuard{r: r, done: make(chan struct{})}
	g.last.Store(time.Now().UnixNano())
	if timeout <= 0 {
		return g
	}
	tick := timeout / 4
	if tick > time.Second {
		tick = time.Second
	}
	if tick < time.Millisecond {
		tick = time.Millisecond // NewTicker panics on a non-positive period
	}
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-g.done:
				return
			case now := <-t.C:
				if now.Sub(time.Unix(0, g.last.Load())) >= timeout {
					g.stalled.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	return g
}

func (g *stallGuard) Read(p []byte) (int, error) {
	n, err := g.r.Read(p)
	if n > 0 {
		g.last.Store(time.Now().UnixNano())
	}
	return n, err
}

// stop ends the watchdog. Safe to call more than once.
func (g *stallGuard) stop() { g.once.Do(func() { close(g.done) }) }

// wrapErr converts the context-cancellation error caused by a stall into a
// descriptive, retryable ErrStalled.
func (g *stallGuard) wrapErr(err error, timeout time.Duration) error {
	if err != nil && g.stalled.Load() {
		return fmt.Errorf("%w: no data received for %s", ErrStalled, timeout)
	}
	return err
}

// copyCtx copies src to dst like io.Copy but stops promptly when ctx is
// cancelled, and reports cumulative progress via onProgress (may be nil) at
// most every interval.
func copyCtx(ctx context.Context, dst io.Writer, src io.Reader, onProgress func(done int64)) (int64, error) {
	buf := make([]byte, 1<<20)
	var written int64
	var lastEmit time.Time
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			written += int64(nw)
			if werr != nil {
				return written, werr
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
			if onProgress != nil && time.Since(lastEmit) >= 200*time.Millisecond {
				onProgress(written)
				lastEmit = time.Now()
			}
		}
		if rerr == io.EOF {
			if onProgress != nil {
				onProgress(written)
			}
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}
