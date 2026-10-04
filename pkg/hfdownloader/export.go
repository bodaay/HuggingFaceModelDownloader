// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/filtermatch"
)

// ExportOptions configures RepoDir.Export.
type ExportOptions struct {
	// Revision is a branch, tag or commit; empty means "main", falling back
	// to the only snapshot when there is no ref.
	Revision string
	// Mode is how exported files are written. Copy (the default; "" and
	// auto mean copy) produces independent files: an export is yours to
	// move, edit or delete without touching the cache. Hardlink (no extra
	// disk space, but edits in place change the cache) and symlink are
	// opt-in.
	Mode LinkMode
	// Filters limits which weight/data files are exported (exact matching,
	// like `download -F ... --exact`); other files are always exported.
	Filters []string
}

// ExportResult summarizes an export.
type ExportResult struct {
	Commit      string   `json:"commit"`
	Dest        string   `json:"dest"`
	Files       int      `json:"files"`
	Hardlinked  int      `json:"hardlinked"`
	Copied      int      `json:"copied"`
	Symlinked   int      `json:"symlinked"`
	Unchanged   int      `json:"unchanged"`
	Bytes       int64    `json:"bytes"`
	FromBlobs   bool     `json:"fromBlobs,omitempty"`
	MissingBlob []string `json:"missingBlob,omitempty"`
}

// exportEntry is one file to export: its repo path and where its data is.
type exportEntry struct {
	rel, src string
}

// Export writes a cached repo snapshot to dest as plain files in the repo's
// own layout, without downloading anything (github issues #83, #91). Files
// are copied unless ExportOptions.Mode asks for hardlinks or symlinks.
//
// It also works on caches whose snapshot links were never created (Windows
// before v3.4.0 stored files only as blobs/<sha256>): the download manifest
// (hfd.yaml) maps file names to blobs.
func (r *RepoDir) Export(dest string, opts ExportOptions) (*ExportResult, error) {
	if dest == "" {
		return nil, errors.New("export: destination is required")
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if within(dest, r.cache.Root) {
		return nil, fmt.Errorf("export: destination %s is inside the cache %s", dest, r.cache.Root)
	}
	mode := opts.Mode
	if mode == "" || mode == LinkAuto {
		mode = LinkCopy
	}

	commit, err := r.resolveCommit(opts.Revision)
	if err != nil {
		return nil, err
	}
	res := &ExportResult{Commit: commit, Dest: dest}

	entries, err := r.snapshotEntries(commit)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		entries, res.MissingBlob, err = r.manifestEntries(commit)
		if err != nil {
			return nil, err
		}
		res.FromBlobs = true
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("export: no files found for %s at %s (snapshot is empty and there is no download manifest)", r.RepoID(), shortCommit(commit))
	}
	all := entries
	entries = filterExportEntries(entries, opts.Filters, r.repoType == RepoTypeDataset)
	if len(opts.Filters) > 0 && countPayload(entries, r.repoType == RepoTypeDataset) == 0 && countPayload(all, r.repoType == RepoTypeDataset) > 0 {
		return nil, fmt.Errorf("export: no files matched filter(s) %s", strings.Join(opts.Filters, ","))
	}

	for _, e := range entries {
		dst := filepath.Join(dest, filepath.FromSlash(e.rel))
		info, err := os.Stat(e.src)
		if err != nil {
			res.MissingBlob = append(res.MissingBlob, e.rel)
			continue
		}
		res.Files++
		res.Bytes += info.Size()
		if sameFile(dst, e.src) || upToDateCopy(dst, info) {
			res.Unchanged++
			continue
		}
		used, err := exportOne(e.src, dst, mode)
		if err != nil {
			return res, fmt.Errorf("export %s: %w", e.rel, err)
		}
		switch used {
		case LinkHardlink:
			res.Hardlinked++
		case LinkSymlink:
			res.Symlinked++
		default:
			res.Copied++
		}
	}
	if len(res.MissingBlob) > 0 {
		return res, fmt.Errorf("export: %d file(s) missing from the cache: %s", len(res.MissingBlob), strings.Join(res.MissingBlob, ", "))
	}
	return res, nil
}

// exportOne places one file with the given mode (copy by default).
func exportOne(src, dst string, mode LinkMode) (LinkMode, error) {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", err
	}
	switch mode {
	case LinkSymlink:
		return placeLink(real, dst, real, "", LinkSymlink)
	default:
		return placeLink(real, dst, "", "", mode)
	}
}

// resolveCommit maps a revision to a snapshot commit.
func (r *RepoDir) resolveCommit(revision string) (string, error) {
	rev := revision
	if rev == "" {
		rev = "main"
	}
	if !ValidRevision(rev) {
		return "", fmt.Errorf("export: invalid revision %q", revision)
	}
	if c, err := r.ReadRef(rev); err == nil && c != "" {
		// A ref file holds a commit hash; never follow anything else.
		if !ValidRevision(c) || strings.Contains(c, "/") {
			return "", fmt.Errorf("export: ref %q does not hold a commit hash", rev)
		}
		return c, nil
	}
	if !strings.Contains(rev, "/") {
		if fi, err := os.Stat(r.SnapshotDir(rev)); err == nil && fi.IsDir() {
			return rev, nil // a commit hash
		}
	}
	snaps, _ := r.ListSnapshots()
	if revision == "" && len(snaps) == 1 {
		return snaps[0], nil
	}
	if len(snaps) == 0 {
		return "", fmt.Errorf("export: %s is not in the cache (no snapshots under %s)", r.RepoID(), r.Path())
	}
	return "", fmt.Errorf("export: revision %q not found for %s; cached snapshots: %s", rev, r.RepoID(), strings.Join(snaps, ", "))
}

// snapshotEntries lists the files in a snapshot.
func (r *RepoDir) snapshotEntries(commit string) ([]exportEntry, error) {
	root := r.SnapshotDir(commit)
	var out []exportEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, exportEntry{rel: filepath.ToSlash(rel), src: p})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, err
}

// manifestEntries maps files to blobs using the download manifest, for
// caches without snapshot links.
func (r *RepoDir) manifestEntries(commit string) ([]exportEntry, []string, error) {
	m, err := ReadManifest(filepath.Join(r.FriendlyPath(), ManifestFilename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	if m.Commit != "" && commit != "" && m.Commit != commit {
		return nil, nil, fmt.Errorf("export: the download manifest is for commit %s, not %s", shortCommit(m.Commit), shortCommit(commit))
	}
	var out []exportEntry
	var missing []string
	for _, f := range m.Files {
		if unsafeRepoPath(f.Name) || !strings.HasPrefix(f.Blob, "blobs/") {
			continue
		}
		src := filepath.Join(r.Path(), filepath.FromSlash(f.Blob))
		if _, err := os.Stat(src); err != nil {
			missing = append(missing, f.Name)
			continue
		}
		out = append(out, exportEntry{rel: f.Name, src: src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, missing, nil
}

// RepairSnapshot recreates missing snapshot entries from the download
// manifest (caches written by Windows builds before v3.4.0 have blobs but no
// snapshot links). It returns how many entries were created.
func (r *RepoDir) RepairSnapshot() (int, error) {
	m, err := ReadManifest(filepath.Join(r.FriendlyPath(), ManifestFilename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if m.Commit == "" {
		return 0, nil
	}
	created := 0
	for _, f := range m.Files {
		if unsafeRepoPath(f.Name) || !strings.HasPrefix(f.Blob, "blobs/") {
			continue
		}
		sha := strings.TrimPrefix(f.Blob, "blobs/")
		if _, err := os.Stat(r.BlobPath(sha)); err != nil {
			continue
		}
		if _, err := os.Stat(r.SnapshotPath(m.Commit, f.Name)); err == nil {
			continue
		}
		if err := r.createSnapshotSymlink(m.Commit, f.Name, sha); err != nil {
			return created, err
		}
		created++
	}
	if created > 0 && m.Branch != "" {
		if _, err := r.ReadRef(m.Branch); err != nil {
			_ = r.WriteRef(m.Branch, m.Commit)
		}
	}
	return created, nil
}

// filterExportEntries keeps files matching a filter plus every file that
// isn't a weight/data file (tokenizers, configs and docs always come along).
func filterExportEntries(entries []exportEntry, filters []string, isDataset bool) []exportEntry {
	var fs []string
	for _, list := range filters {
		for _, f := range strings.Split(list, ",") {
			if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
				fs = append(fs, f)
			}
		}
	}
	if len(fs) == 0 {
		return entries
	}
	var out []exportEntry
	for _, e := range entries {
		lower := strings.ToLower(e.rel)
		keep := !isPayloadFile(e.rel, isDataset)
		for _, f := range fs {
			if keep || filtermatch.Match(lower, f, true) {
				keep = true
				break
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}

// within reports whether path is root or inside it, after resolving
// symlinks (on macOS /tmp is a symlink to /private/tmp).
func within(path, root string) bool {
	path, root = resolveExisting(path), resolveExisting(root)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExisting makes p absolute and resolves symlinks in its longest
// existing prefix.
func resolveExisting(p string) string {
	p, _ = filepath.Abs(p)
	rest := ""
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// upToDateCopy reports whether dst is a regular file with src's size that is
// not older than src — a previous copy-mode export (hardlinks are detected
// with sameFile).
func upToDateCopy(dst string, src os.FileInfo) bool {
	fi, err := os.Stat(dst)
	return err == nil && fi.Mode().IsRegular() && fi.Size() == src.Size() && !fi.ModTime().Before(src.ModTime())
}

// countPayload counts weight/data files among entries.
func countPayload(entries []exportEntry, isDataset bool) int {
	n := 0
	for _, e := range entries {
		if isPayloadFile(e.rel, isDataset) {
			n++
		}
	}
	return n
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
