// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LinkMode controls how cache entries (snapshot and friendly-view files)
// refer to downloaded blobs.
type LinkMode string

const (
	// LinkAuto tries a symlink, then a hardlink, then a copy (default).
	LinkAuto LinkMode = "auto"
	// LinkSymlink uses relative symlinks, like the HuggingFace Hub cache.
	LinkSymlink LinkMode = "symlink"
	// LinkHardlink uses hardlinks: real files that share the blob's disk
	// space. Works on Windows without admin rights or Developer Mode, as long
	// as the link and the blob are on the same drive.
	LinkHardlink LinkMode = "hardlink"
	// LinkCopy copies the data (uses extra disk space).
	LinkCopy LinkMode = "copy"
)

// ParseLinkMode validates a link mode string ("" means auto).
func ParseLinkMode(s string) (LinkMode, error) {
	switch m := LinkMode(strings.ToLower(strings.TrimSpace(s))); m {
	case "":
		return LinkAuto, nil
	case LinkAuto, LinkSymlink, LinkHardlink, LinkCopy:
		return m, nil
	}
	return "", fmt.Errorf("invalid link mode %q (want auto, symlink, hardlink or copy)", s)
}

// linkFallbacks remembers, per directory tree root, which link kinds failed,
// so auto mode doesn't retry a symlink for every file on Windows.
var linkFallbacks sync.Map // root+"|"+kind -> struct{}

// LinkNotice, when set, is called once per root the first time auto mode
// falls back from symlinks (e.g. Windows without Developer Mode).
var LinkNotice = func(msg string) { fmt.Fprintln(os.Stderr, "[INFO] "+msg) }

var noticeOnce sync.Map

// symlinkFn and hardlinkFn are os.Symlink and os.Link; tests replace them to
// simulate filesystems without symlinks (Windows without Developer Mode) or
// without any links (FAT/exFAT drives).
var (
	symlinkFn  = os.Symlink
	hardlinkFn = os.Link
)

// placeLink makes dst refer to the content of src. symlinkTarget is the
// (relative) target used when a symlink is created. root identifies the
// filesystem area for remembering failed link kinds. It returns the link
// kind actually used. Any existing file or link at dst is replaced.
func placeLink(src, dst, symlinkTarget, root string, mode LinkMode) (LinkMode, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("create parent directory: %w", err)
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return "", fmt.Errorf("remove existing entry: %w", err)
		}
	}

	try := func(kind LinkMode) error {
		switch kind {
		case LinkSymlink:
			return symlinkFn(symlinkTarget, dst)
		case LinkHardlink:
			real, err := filepath.EvalSymlinks(src)
			if err != nil {
				return err
			}
			return hardlinkFn(real, dst)
		default:
			return CopyFileStream(src, dst)
		}
	}

	if mode != LinkAuto && mode != "" {
		if err := try(mode); err != nil {
			return "", fmt.Errorf("create %s: %w", mode, err)
		}
		return mode, nil
	}

	var lastErr error
	for _, kind := range []LinkMode{LinkSymlink, LinkHardlink, LinkCopy} {
		key := root + "|" + string(kind)
		if _, failed := linkFallbacks.Load(key); failed && kind != LinkCopy {
			continue
		}
		err := try(kind)
		if err == nil {
			if kind != LinkSymlink {
				if _, done := noticeOnce.LoadOrStore(root, struct{}{}); !done {
					if kind == LinkHardlink {
						LinkNotice("symlinks are not available here; using hardlinks instead (real files, no extra disk space)")
					} else {
						LinkNotice("neither symlinks nor hardlinks are available here; copying files instead (uses extra disk space)")
					}
				}
			}
			return kind, nil
		}
		lastErr = err
		linkFallbacks.Store(key, struct{}{})
	}
	return "", fmt.Errorf("link %s: %w", dst, lastErr)
}

// LinkSupport reports which link kinds work in dir (created if missing).
func LinkSupport(dir string) (symlink, hardlink bool) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, false
	}
	probe, err := os.MkdirTemp(dir, ".hfd-linkprobe-")
	if err != nil {
		return false, false
	}
	defer os.RemoveAll(probe)
	src := filepath.Join(probe, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		return false, false
	}
	symlink = symlinkFn("src", filepath.Join(probe, "sym")) == nil
	hardlink = hardlinkFn(src, filepath.Join(probe, "hard")) == nil
	return symlink, hardlink
}
