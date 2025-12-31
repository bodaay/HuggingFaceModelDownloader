// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IsValidModelName checks if the model name is in "owner/name" format.
func IsValidModelName(modelName string) bool {
	if modelName == "" || !strings.Contains(modelName, "/") {
		return false
	}
	parts := strings.Split(modelName, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// validate checks that the job and settings are valid.
func validate(job Job, cfg Settings) error {
	if job.Repo == "" {
		return errors.New("missing repo")
	}
	if !IsValidModelName(job.Repo) {
		return fmt.Errorf("invalid repo id %q (expected owner/name)", job.Repo)
	}
	return nil
}

// backoff implements exponential backoff with jitter.
type backoff struct {
	next   time.Duration
	max    time.Duration
	mult   float64
	jitter time.Duration
}

// newRetry creates a new backoff instance from settings.
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

// Next returns the next backoff duration.
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

// parseSizeString parses a human-readable size string (e.g., "32MiB") to bytes.
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

// defaultString returns s if non-empty, otherwise def.
func defaultString(s string, def string) string {
	if s == "" {
		return def
	}
	return s
}

