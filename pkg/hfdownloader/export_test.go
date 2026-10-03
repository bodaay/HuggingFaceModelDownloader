// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// simulateLinks replaces the link functions for one test: noSymlink mimics
// Windows without Developer Mode, noLinks a FAT/exFAT drive.
func simulateLinks(t *testing.T, noSymlink, noHardlink bool) {
	t.Helper()
	origSym, origHard := symlinkFn, hardlinkFn
	if noSymlink {
		symlinkFn = func(string, string) error { return errors.New("A required privilege is not held by the client") }
	}
	if noHardlink {
		hardlinkFn = func(string, string) error { return errors.New("not supported") }
	}
	linkFallbacks = sync.Map{}
	noticeOnce = sync.Map{}
	t.Cleanup(func() {
		symlinkFn, hardlinkFn = origSym, origHard
		linkFallbacks = sync.Map{}
		noticeOnce = sync.Map{}
	})
}

func isSymlink(t *testing.T, p string) bool {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatalf("lstat %s: %v", p, err)
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return b
}

// downloadFake downloads a small two-file repo (one nested) from a fake Hub
// into a fresh cache and returns the cache, repo dir, commit and contents.
func downloadFake(t *testing.T, cfg Settings) (*HFCache, *RepoDir, string, map[string][]byte) {
	t.Helper()
	files := map[string][]byte{"model.gguf": testPayload(70000), "sub/config.bin": testPayload(5000)}
	hub := &fakeHub{commit: "0123456789abcdef0123456789abcdef01234567", files: files, treeAcceptSHA: true, fileAcceptSHA: true}
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)

	cfg.Endpoint = srv.URL
	cfg.CacheDir = t.TempDir()
	cfg.Retries = 1
	if err := Download(context.Background(), Job{Repo: "o/r"}, cfg, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	cache, _ := cfg.BuildHFCache()
	rd, _ := cache.Repo("o/r", RepoTypeModel)
	return cache, rd, hub.commit, files
}

// On Windows without Developer Mode symlinks fail. Snapshot entries used to
// be skipped entirely (files only as blobs/<sha256>); now they are hardlinks,
// so Python/HF tools and the friendly view work.
func TestDownload_SymlinksUnavailableUsesHardlinks(t *testing.T) {
	simulateLinks(t, true, false)
	_, rd, commit, files := downloadFake(t, Settings{})

	for rel, want := range files {
		snap := rd.SnapshotPath(commit, rel)
		if isSymlink(t, snap) {
			t.Errorf("%s: snapshot entry is a symlink although symlinks failed", rel)
		}
		if !bytes.Equal(readFile(t, snap), want) {
			t.Errorf("%s: snapshot content mismatch", rel)
		}
		friendly := filepath.Join(rd.FriendlyPath(), filepath.FromSlash(rel))
		if !sameFile(friendly, snap) {
			t.Errorf("%s: friendly view is not a hardlink of the snapshot file", rel)
		}
	}
}

// With no links at all (FAT/exFAT), snapshot entries are copies and the
// friendly view is skipped (it would store every file a third time).
func TestDownload_NoLinksCopiesAndSkipsFriendlyView(t *testing.T) {
	simulateLinks(t, true, true)
	_, rd, commit, files := downloadFake(t, Settings{})

	for rel, want := range files {
		if !bytes.Equal(readFile(t, rd.SnapshotPath(commit, rel)), want) {
			t.Errorf("%s: snapshot copy content mismatch", rel)
		}
		if _, err := os.Stat(filepath.Join(rd.FriendlyPath(), filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s: friendly view created in copy-only mode", rel)
		}
	}
}

func TestDownload_ExplicitLinkModes(t *testing.T) {
	for _, mode := range []string{"symlink", "hardlink", "copy"} {
		t.Run(mode, func(t *testing.T) {
			_, rd, commit, files := downloadFake(t, Settings{LinkMode: mode})
			snap := rd.SnapshotPath(commit, "sub/config.bin")
			if got := isSymlink(t, snap); got != (mode == "symlink") {
				t.Errorf("snapshot symlink = %v for mode %s", got, mode)
			}
			if !bytes.Equal(readFile(t, snap), files["sub/config.bin"]) {
				t.Error("content mismatch")
			}
			blob, _ := filepath.EvalSymlinks(snap)
			shared := sameFile(snap, blob) && !isSymlink(t, snap)
			if mode == "hardlink" && !shared {
				t.Error("hardlink mode did not share the blob")
			}
		})
	}
	if _, err := (Settings{LinkMode: "bogus"}).BuildHFCache(); err == nil {
		t.Error("invalid link mode accepted")
	}
}

func TestExport_HardlinksRealFiles(t *testing.T) {
	_, rd, commit, files := downloadFake(t, Settings{})
	dest := filepath.Join(t.TempDir(), "out")

	res, err := rd.Export(dest, ExportOptions{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if res.Commit != commit || res.Files != 2 || res.Hardlinked != 2 {
		t.Errorf("result = %+v", res)
	}
	for rel, want := range files {
		p := filepath.Join(dest, filepath.FromSlash(rel))
		if isSymlink(t, p) {
			t.Errorf("%s exported as a symlink; tools need real files", rel)
		}
		if !bytes.Equal(readFile(t, p), want) {
			t.Errorf("%s: content mismatch", rel)
		}
	}

	again, err := rd.Export(dest, ExportOptions{})
	if err != nil || again.Unchanged != 2 {
		t.Errorf("second export = %+v, %v; want everything unchanged", again, err)
	}
}

func TestExport_CopyWhenHardlinksFail(t *testing.T) {
	_, rd, _, files := downloadFake(t, Settings{})
	simulateLinks(t, true, true) // destination on a drive without links
	dest := t.TempDir()
	res, err := rd.Export(dest, ExportOptions{})
	if err != nil || res.Copied != 2 {
		t.Fatalf("Export = %+v, %v; want 2 copies", res, err)
	}
	if !bytes.Equal(readFile(t, filepath.Join(dest, "model.gguf")), files["model.gguf"]) {
		t.Error("content mismatch")
	}
}

func TestExport_FiltersKeepSupportFiles(t *testing.T) {
	_, rd, _, _ := downloadFake(t, Settings{})
	dest := t.TempDir()
	// "sub/config.bin" is a weight-format file not matching the filter.
	res, err := rd.Export(dest, ExportOptions{Filters: []string{"model"}})
	if err != nil || res.Files != 1 {
		t.Fatalf("Export = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "sub", "config.bin")); err == nil {
		t.Error("unmatched weight file exported")
	}
}

// Caches written by Windows builds before v3.4.0 have blobs and a manifest but
// no snapshot entries. Export still works, and rebuild repairs the cache.
func TestExport_AndRepair_LinklessCache(t *testing.T) {
	cache, rd, commit, files := downloadFake(t, Settings{})
	if err := os.RemoveAll(rd.SnapshotsDir()); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	res, err := rd.Export(dest, ExportOptions{})
	if err != nil || !res.FromBlobs || res.Files != 2 {
		t.Fatalf("Export from blobs = %+v, %v", res, err)
	}
	if !bytes.Equal(readFile(t, filepath.Join(dest, "sub", "config.bin")), files["sub/config.bin"]) {
		t.Error("content mismatch")
	}

	sync, err := cache.Sync(SyncOptions{})
	if err != nil || sync.SnapshotEntriesRepaired != 2 {
		t.Fatalf("Sync = %+v, %v; want 2 repaired entries", sync, err)
	}
	for rel, want := range files {
		if !bytes.Equal(readFile(t, rd.SnapshotPath(commit, rel)), want) {
			t.Errorf("%s: repaired snapshot entry wrong", rel)
		}
	}
}

func TestExport_Errors(t *testing.T) {
	cache, rd, _, _ := downloadFake(t, Settings{})
	if _, err := rd.Export(filepath.Join(cache.Root, "inside"), ExportOptions{}); err == nil || !strings.Contains(err.Error(), "inside the cache") {
		t.Errorf("export into the cache: %v", err)
	}
	if _, err := rd.Export(t.TempDir(), ExportOptions{Revision: "nope"}); err == nil {
		t.Error("unknown revision accepted")
	}
	missing, _ := cache.Repo("o/missing", RepoTypeModel)
	if _, err := missing.Export(t.TempDir(), ExportOptions{}); err == nil || !strings.Contains(err.Error(), "not in the cache") {
		t.Errorf("uncached repo: %v", err)
	}
}

func TestLinkSupport(t *testing.T) {
	if sym, hard := LinkSupport(t.TempDir()); !hard {
		t.Errorf("LinkSupport = %v, %v; hardlinks should work in a temp dir", sym, hard)
	}
	simulateLinks(t, true, true)
	if sym, hard := LinkSupport(t.TempDir()); sym || hard {
		t.Errorf("simulated no-link fs reported %v, %v", sym, hard)
	}
	_ = time.Second
}
