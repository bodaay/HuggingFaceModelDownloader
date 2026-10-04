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

// Export copies by default: the result is independent of the cache, so
// editing an exported file can't change the cached data.
func TestExport_CopiesByDefault(t *testing.T) {
	_, rd, commit, files := downloadFake(t, Settings{})
	dest := filepath.Join(t.TempDir(), "out")

	res, err := rd.Export(dest, ExportOptions{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if res.Commit != commit || res.Files != 2 || res.Copied != 2 || res.Hardlinked != 0 {
		t.Errorf("result = %+v; want 2 copies", res)
	}
	exported := filepath.Join(dest, "model.gguf")
	if sameFile(exported, rd.SnapshotPath(commit, "model.gguf")) {
		t.Fatal("default export shares data with the cache")
	}
	os.WriteFile(exported, []byte("edited"), 0o644)
	if !bytes.Equal(readFile(t, rd.SnapshotPath(commit, "model.gguf")), files["model.gguf"]) {
		t.Error("editing the export changed the cache")
	}
	again, err := rd.Export(dest, ExportOptions{})
	if err != nil || again.Copied != 1 || again.Unchanged != 1 {
		t.Errorf("re-export = %+v, %v; want the edited file re-copied and the other unchanged", again, err)
	}
}

func TestExport_HardlinkMode(t *testing.T) {
	_, rd, commit, files := downloadFake(t, Settings{})
	dest := filepath.Join(t.TempDir(), "out")

	res, err := rd.Export(dest, ExportOptions{Mode: LinkHardlink})
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

func TestExport_HardlinkModeFailsWithoutLinks(t *testing.T) {
	_, rd, _, files := downloadFake(t, Settings{})
	simulateLinks(t, true, true) // destination on a drive without links
	if _, err := rd.Export(t.TempDir(), ExportOptions{Mode: LinkHardlink}); err == nil {
		t.Error("hardlink mode silently fell back")
	}
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

// QA found export accepted revision "../../../../secret": it hardlinked files
// from outside the cache and leaked file contents in an error message.
func TestExport_RejectsRevisionTraversal(t *testing.T) {
	cache, rd, _, _ := downloadFake(t, Settings{})
	secret := filepath.Join(filepath.Dir(cache.Root), "secret")
	os.MkdirAll(secret, 0o755)
	os.WriteFile(filepath.Join(secret, "key.txt"), []byte("TOPSECRET"), 0o644)

	for _, rev := range []string{"../../../../secret", "../../../../secret/key.txt", "/etc", `..\..\x`, "a/../../b", "main\x00"} {
		dest := t.TempDir()
		_, err := rd.Export(dest, ExportOptions{Revision: rev})
		if err == nil {
			t.Errorf("revision %q accepted", rev)
		} else if strings.Contains(err.Error(), "TOPSECRET") {
			t.Errorf("revision %q leaked file content: %v", rev, err)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 0 {
			t.Errorf("revision %q wrote %d entries", rev, len(entries))
		}
	}
	if _, err := rd.ReadRef("../../x"); err == nil {
		t.Error("ReadRef accepted a traversal ref")
	}
	if err := rd.WriteRef("../../x", "abc"); err == nil {
		t.Error("WriteRef accepted a traversal ref")
	}
}

func TestValidRevision(t *testing.T) {
	for _, ok := range []string{"main", "v1.0", "refs/pr/12", "4.00bpw", "SC_6.00bpw_H6_V6", "0123456789abcdef0123456789abcdef01234567"} {
		if !ValidRevision(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "..", "../x", "a/../b", "/abs", `a\b`, "a//b", "x\x00", "C:foo"} {
		if ValidRevision(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := validate(Job{Repo: "o/r", Revision: "../../x"}, Settings{}); err == nil {
		t.Error("download validate accepted a traversal revision")
	}
}

func TestExport_FilterMatchingNothingFails(t *testing.T) {
	_, rd, _, _ := downloadFake(t, Settings{})
	if _, err := rd.Export(t.TempDir(), ExportOptions{Filters: []string{"q8_0"}}); err == nil || !strings.Contains(err.Error(), "no files matched") {
		t.Errorf("err = %v, want a no-match error", err)
	}
}

func TestExport_DestInsideCacheViaSymlink(t *testing.T) {
	cache, rd, _, _ := downloadFake(t, Settings{})
	link := filepath.Join(t.TempDir(), "cache-link")
	if err := os.Symlink(cache.Root, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := rd.Export(filepath.Join(link, "out"), ExportOptions{}); err == nil {
		t.Error("export into the cache through a symlink accepted")
	}
}

// Mirroring a hardlink-mode cache must not store each file twice.
func TestCopyRepoCache_PreservesHardlinks(t *testing.T) {
	cache, rd, commit, _ := downloadFake(t, Settings{LinkMode: "hardlink"})
	dst := t.TempDir()
	if err := CopyRepoCache(rd.Path(), cache.Root, dst); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(cache.Root, rd.SnapshotPath(commit, "model.gguf"))
	copiedSnap := filepath.Join(dst, rel)
	blobRel, _ := filepath.Rel(cache.Root, rd.BlobsDir())
	blobs, _ := os.ReadDir(filepath.Join(dst, blobRel))
	shared := false
	for _, b := range blobs {
		if sameFile(copiedSnap, filepath.Join(dst, blobRel, b.Name())) {
			shared = true
		}
	}
	if !shared {
		t.Error("mirrored snapshot entry is a separate copy of its blob")
	}
}
