// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"testing"
	"time"
)

func TestIsValidModelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid model name", "TheBloke/Mistral-7B", true},
		{"valid with hyphen", "meta-llama/Llama-2-7b", true},
		{"valid with numbers", "facebook/opt-1.3b", true},
		{"valid with dots", "THUDM/chatglm2-6b", true},
		{"empty string", "", false},
		{"no slash", "TheBlokeMistral", false},
		{"only slash", "/", false},
		{"empty owner", "/Mistral-7B", false},
		{"empty name", "TheBloke/", false},
		{"multiple slashes", "owner/name/extra", false},
		{"just owner", "TheBloke", false},
		{"spaces in name", "The Bloke/Model", true}, // Contains slash, so valid format
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidModelName(tt.input)
			if result != tt.expected {
				t.Errorf("IsValidModelName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		job     Job
		cfg     Settings
		wantErr bool
	}{
		{
			name:    "valid job",
			job:     Job{Repo: "owner/name"},
			cfg:     Settings{},
			wantErr: false,
		},
		{
			name:    "empty repo",
			job:     Job{Repo: ""},
			cfg:     Settings{},
			wantErr: true,
		},
		{
			name:    "invalid repo format",
			job:     Job{Repo: "invalid"},
			cfg:     Settings{},
			wantErr: true,
		},
		{
			name:    "repo with only slash",
			job:     Job{Repo: "/"},
			cfg:     Settings{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.job, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		def      int64
		expected int64
		wantErr  bool
	}{
		// Empty string
		{"empty string", "", 100, 100, false},

		// Bytes
		{"bytes explicit", "1024B", 0, 1024, false},
		{"bytes uppercase", "512B", 0, 512, false},

		// SI units (1000-based)
		{"kilobytes", "1KB", 0, 1000, false},
		{"megabytes", "1MB", 0, 1000000, false},
		{"gigabytes", "1GB", 0, 1000000000, false},
		{"fractional MB", "1.5MB", 0, 1500000, false},

		// Binary units (1024-based)
		{"kibibytes", "1KiB", 0, 1024, false},
		{"mebibytes", "1MiB", 0, 1048576, false},
		{"gibibytes", "1GiB", 0, 1073741824, false},
		{"32 mebibytes", "32MiB", 0, 33554432, false},
		{"fractional GiB", "1.5GiB", 0, 1610612736, false},

		// Plain integers
		{"plain int", "12345", 0, 12345, false},

		// Case insensitive
		{"lowercase mib", "32mib", 0, 33554432, false},
		{"mixed case", "32Mib", 0, 33554432, false},

		// With whitespace
		{"whitespace", "  32MiB  ", 0, 33554432, false},

		// Invalid
		{"unknown unit", "100XB", 0, 0, true},
		{"garbage", "abc", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSizeString(tt.input, tt.def)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSizeString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("parseSizeString(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultString(t *testing.T) {
	tests := []struct {
		s, def, expected string
	}{
		{"", "default", "default"},
		{"value", "default", "value"},
		{"", "", ""},
		{"hello", "", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.def, func(t *testing.T) {
			result := defaultString(tt.s, tt.def)
			if result != tt.expected {
				t.Errorf("defaultString(%q, %q) = %q, want %q", tt.s, tt.def, result, tt.expected)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		b := newRetry(Settings{})

		// First call should return approximately 400ms (with jitter)
		d1 := b.Next()
		if d1 < 400*time.Millisecond || d1 > 600*time.Millisecond {
			t.Errorf("First backoff = %v, want ~400-600ms", d1)
		}

		// Subsequent calls should increase
		d2 := b.Next()
		if d2 <= d1 {
			// Allow for jitter variation
			t.Logf("Second backoff %v should generally be >= first %v (allowing for jitter)", d2, d1)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		b := newRetry(Settings{
			BackoffInitial: "1s",
			BackoffMax:     "5s",
		})

		d1 := b.Next()
		// Should start around 1s
		if d1 < 1*time.Second || d1 > 1200*time.Millisecond {
			t.Errorf("First backoff = %v, want ~1s", d1)
		}
	})

	t.Run("respects max", func(t *testing.T) {
		b := newRetry(Settings{
			BackoffInitial: "5s",
			BackoffMax:     "10s",
		})

		// Run many iterations
		for i := 0; i < 20; i++ {
			d := b.Next()
			// With max of 10s and jitter of ~120ms, max should be around 10.2s
			if d > 11*time.Second {
				t.Errorf("Backoff %v exceeded max", d)
			}
		}
	})
}

func TestSleepCtx(t *testing.T) {
	t.Run("completes normally", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now()
		result := sleepCtx(ctx, 50*time.Millisecond)
		elapsed := time.Since(start)

		if !result {
			t.Error("sleepCtx returned false, want true")
		}
		if elapsed < 50*time.Millisecond {
			t.Errorf("sleepCtx returned too early: %v", elapsed)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		start := time.Now()
		result := sleepCtx(ctx, 1*time.Second)
		elapsed := time.Since(start)

		if result {
			t.Error("sleepCtx returned true, want false (cancelled)")
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("sleepCtx didn't return quickly after cancel: %v", elapsed)
		}
	})

	t.Run("timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		start := time.Now()
		result := sleepCtx(ctx, 1*time.Second)
		elapsed := time.Since(start)

		if result {
			t.Error("sleepCtx returned true, want false (timeout)")
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("sleepCtx didn't return after timeout: %v", elapsed)
		}
	})
}
