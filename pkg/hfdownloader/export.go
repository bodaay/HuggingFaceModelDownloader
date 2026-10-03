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
	// Mode is how exported files refer to cached data. Auto (default) uses
	// hardlinks — real files that share disk space with the cache — and
	// copies when the destination is on another drive or hardlinks aren't
	// supported. Symlinks are used only when asked for explicitly: tools
	// like LM Studio need real files.
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
// are hardlinked from the cache when possible, else copied.
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
	if mode == "" {
		mode = LinkAuto
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
	entries = filterExportEntries(entries, opts.Filters, r.repoType == RepoTypeDataset)

	for _, e := range entries {
		dst := filepath.Join(dest, filepath.FromSlash(e.rel))
		info, err := os.Stat(e.src)
		if err != nil {
			res.MissingBlob = append(res.MissingBlob, e.rel)
			continue
		}
		res.Files++
		res.Bytes += info.Size()
		if sameFile(dst, e.src) {
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

// exportOne places one file: auto = hardlink, else copy.
func exportOne(src, dst string, mode LinkMode) (LinkMode, error) {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", err
	}
	switch mode {
	case LinkAuto:
		if used, err := placeLink(real, dst, "", "", LinkHardlink); err == nil {
			return used, nil
		}
		return placeLink(real, dst, "", "", LinkCopy)
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
	if c, err := r.ReadRef(rev); err == nil && c != "" {
		return c, nil
	}
	if fi, err := os.Stat(r.SnapshotDir(rev)); err == nil && fi.IsDir() {
		return rev, nil // a commit hash
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

// within reports whether path is root or inside it.
func within(path, root string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
