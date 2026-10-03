// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// TestFinalize_LocalDirSetsOutputAndClearsCache verifies that --local-dir
// (with no --legacy) produces the same post-finalize Settings as
// --legacy -o <dir>: OutputDir set to the chosen path, CacheDir cleared to
// force flat-file mode. This is the feature added for github issue #73.
func TestFinalize_LocalDirSetsOutputAndClearsCache(t *testing.T) {
	ro := &RootOpts{}
	job := &hfdownloader.Job{Repo: "owner/repo"}
	cfg := &hfdownloader.Settings{CacheDir: "/should/be/cleared"}

	j, c, err := finalize(nil, ro, nil, job, cfg,
		false,             // legacy
		"",                // legacyOutput
		"/tmp/my-models",  // localDir
		"", "", "", false, // proxy
	)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if c.OutputDir != "/tmp/my-models" {
		t.Errorf("OutputDir = %q, want %q", c.OutputDir, "/tmp/my-models")
	}
	if c.CacheDir != "" {
		t.Errorf("CacheDir = %q, want empty (flat-file mode)", c.CacheDir)
	}
	if j.Repo != "owner/repo" {
		t.Errorf("Repo = %q", j.Repo)
	}
}

// TestFinalize_LocalDirEquivalentToLegacyOutput verifies byte-for-byte that
// --local-dir <path> and --legacy -o <path> produce identical Settings. The
// two flag spellings must stay interchangeable forever — the user explicitly
// asked that neither be a breaking change.
func TestFinalize_LocalDirEquivalentToLegacyOutput(t *testing.T) {
	const path = "/tmp/equiv"

	ro := &RootOpts{}
	_, cLocal, err := finalize(nil, ro, nil,
		&hfdownloader.Job{Repo: "owner/repo"},
		&hfdownloader.Settings{},
		false, "", path, "", "", "", false,
	)
	if err != nil {
		t.Fatalf("finalize (--local-dir): %v", err)
	}

	_, cLegacy, err := finalize(nil, ro, nil,
		&hfdownloader.Job{Repo: "owner/repo"},
		&hfdownloader.Settings{},
		true, path, "", "", "", "", false,
	)
	if err != nil {
		t.Fatalf("finalize (--legacy -o): %v", err)
	}

	if cLocal.OutputDir != cLegacy.OutputDir {
		t.Errorf("OutputDir mismatch: local=%q legacy=%q", cLocal.OutputDir, cLegacy.OutputDir)
	}
	if cLocal.CacheDir != cLegacy.CacheDir {
		t.Errorf("CacheDir mismatch: local=%q legacy=%q", cLocal.CacheDir, cLegacy.CacheDir)
	}
}

// TestFinalize_LocalDirConflictsWithOutput ensures the two flag spellings
// can't be combined — it's a user error and we reject it clearly rather
// than silently letting one override the other.
func TestFinalize_LocalDirConflictsWithOutput(t *testing.T) {
	ro := &RootOpts{}
	_, _, err := finalize(nil, ro, nil,
		&hfdownloader.Job{Repo: "owner/repo"},
		&hfdownloader.Settings{},
		true,     // legacy
		"/tmp/a", // legacyOutput
		"/tmp/b", // localDir
		"", "", "", false,
	)
	if err == nil {
		t.Fatal("expected an error when both --local-dir and --output are given")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error message should mention mutual exclusion, got: %v", err)
	}
}

// TestFinalize_DefaultUsesHFCache is a guard that the no-flag default still
// uses the HF cache structure (empty OutputDir means Download() will pick
// the cache path).
func TestFinalize_DefaultUsesHFCache(t *testing.T) {
	ro := &RootOpts{}
	_, c, err := finalize(nil, ro, nil,
		&hfdownloader.Job{Repo: "owner/repo"},
		&hfdownloader.Settings{},
		false, "", "", "", "", "", false,
	)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if c.OutputDir != "" {
		t.Errorf("OutputDir should be empty in default mode, got %q", c.OutputDir)
	}
}
