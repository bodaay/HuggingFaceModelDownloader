package hfdownloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Windows symlink warning - only show once per session
type RepoType string

const (
	RepoTypeModel   RepoType = "model"
	RepoTypeDataset RepoType = "dataset"
)

// HFCache represents the HuggingFace cache root directory.
// It follows the official HuggingFace Hub cache structure.
type HFCache struct {
	// Root is the cache root directory (e.g., ~/.cache/huggingface)
	Root string

	// StaleTimeout is the duration after which an .incomplete file
	// with no writes is considered stale and can be taken over.
	StaleTimeout time.Duration

	// LinkMode controls how snapshot and friendly-view entries refer to
	// blobs (see LinkMode). Empty means auto.
	LinkMode LinkMode
}

// copyOnly reports whether auto mode found that neither symlinks nor
// hardlinks work in this cache (so entries are copies).
func (c *HFCache) copyOnly() bool {
	_, sym := linkFallbacks.Load(c.Root + "|" + string(LinkSymlink))
	_, hard := linkFallbacks.Load(c.Root + "|" + string(LinkHardlink))
	return sym && hard
}

// DefaultStaleTimeout is the default timeout for stale .incomplete files.
const DefaultStaleTimeout = 5 * time.Minute

// DefaultCacheDir returns the default HuggingFace cache directory.
// Priority: HF_HOME env > ~/.cache/huggingface
func DefaultCacheDir() string {
	if hfHome := os.Getenv("HF_HOME"); hfHome != "" {
		return hfHome
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cache/huggingface"
	}
	return filepath.Join(home, ".cache", "huggingface")
}

// NewHFCache creates a new HFCache with the given root directory.
// If root is empty, it uses DefaultCacheDir().
func NewHFCache(root string, staleTimeout time.Duration) *HFCache {
	if root == "" {
		root = DefaultCacheDir()
	}
	if staleTimeout == 0 {
		staleTimeout = DefaultStaleTimeout
	}
	return &HFCache{
		Root:         root,
		StaleTimeout: staleTimeout,
	}
}

// HubDir returns the path to the hub/ directory.
func (c *HFCache) HubDir() string {
	// Check HF_HUB_CACHE env for override
	if hubCache := os.Getenv("HF_HUB_CACHE"); hubCache != "" {
		return hubCache
	}
	return filepath.Join(c.Root, "hub")
}

// ModelsDir returns the path to the friendly models/ directory.
func (c *HFCache) ModelsDir() string {
	return filepath.Join(c.Root, "models")
}

// DatasetsDir returns the path to the friendly datasets/ directory.
func (c *HFCache) DatasetsDir() string {
	return filepath.Join(c.Root, "datasets")
}

// RepoDir represents a single repository within the HF cache.
type RepoDir struct {
	cache    *HFCache
	repoType RepoType
	owner    string
	name     string
}

// Repo returns a RepoDir for the given repository.
// repoID should be in the format "owner/name".
func (c *HFCache) Repo(repoID string, repoType RepoType) (*RepoDir, error) {
	// Validate here, at the one place repo IDs become cache paths, so no
	// caller can build a path outside the cache (e.g. "x/../../etc").
	if !IsValidModelName(repoID) {
		return nil, fmt.Errorf("invalid repo ID: %q (expected owner/name)", repoID)
	}
	parts := strings.SplitN(repoID, "/", 2)
	return &RepoDir{
		cache:    c,
		repoType: repoType,
		owner:    parts[0],
		name:     parts[1],
	}, nil
}

// dirName returns the HF cache directory name for this repo.
// Format: models--{owner}--{name} or datasets--{owner}--{name}
func (r *RepoDir) dirName() string {
	prefix := "models"
	if r.repoType == RepoTypeDataset {
		prefix = "datasets"
	}
	return fmt.Sprintf("%s--%s--%s", prefix, r.owner, r.name)
}

// Path returns the full path to this repo's cache directory.
// Example: ~/.cache/huggingface/hub/models--TheBloke--Mistral-7B-GGUF
func (r *RepoDir) Path() string {
	return filepath.Join(r.cache.HubDir(), r.dirName())
}

// BlobsDir returns the path to the blobs/ directory.
func (r *RepoDir) BlobsDir() string {
	return filepath.Join(r.Path(), "blobs")
}

// RefsDir returns the path to the refs/ directory.
func (r *RepoDir) RefsDir() string {
	return filepath.Join(r.Path(), "refs")
}

// SnapshotsDir returns the path to the snapshots/ directory.
func (r *RepoDir) SnapshotsDir() string {
	return filepath.Join(r.Path(), "snapshots")
}

// BlobPath returns the path where a blob with the given SHA256 should be stored.
func (r *RepoDir) BlobPath(sha256 string) string {
	return filepath.Join(r.BlobsDir(), sha256)
}

// IncompletePath returns the path for an incomplete download.
// The .incomplete file contains the partial data.
func (r *RepoDir) IncompletePath(sha256 string) string {
	return filepath.Join(r.BlobsDir(), sha256+".incomplete")
}

// IncompleteMetaPath returns the path for the incomplete metadata file.
func (r *RepoDir) IncompleteMetaPath(sha256 string) string {
	return filepath.Join(r.BlobsDir(), sha256+".incomplete.meta")
}

// RefPath returns the path to a ref file (e.g., refs/main).
func (r *RepoDir) RefPath(ref string) string {
	return filepath.Join(r.RefsDir(), ref)
}

// SnapshotDir returns the path to a snapshot directory for a given commit.
func (r *RepoDir) SnapshotDir(commit string) string {
	return filepath.Join(r.SnapshotsDir(), commit)
}

// EnsureDirs creates all necessary directories for this repo.
func (r *RepoDir) EnsureDirs() error {
	dirs := []string{
		r.BlobsDir(),
		r.RefsDir(),
		r.SnapshotsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// IncompleteMeta contains metadata about an incomplete download.
type IncompleteMeta struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
}

// BlobStatus represents the status of a blob in the cache.
type BlobStatus int

const (
	// BlobMissing means the blob doesn't exist and no download is in progress.
	BlobMissing BlobStatus = iota

	// BlobComplete means the blob exists and is complete.
	BlobComplete

	// BlobDownloading means another process is currently downloading this blob.
	BlobDownloading

	// BlobStale means an incomplete download exists but is stale (can be taken over).
	BlobStale
)

// String returns a human-readable status.
func (s BlobStatus) String() string {
	switch s {
	case BlobMissing:
		return "missing"
	case BlobComplete:
		return "complete"
	case BlobDownloading:
		return "downloading"
	case BlobStale:
		return "stale"
	default:
		return "unknown"
	}
}

// CheckBlob checks the status of a blob in the cache.
// Returns the status and any existing incomplete metadata.
func (r *RepoDir) CheckBlob(sha256 string) (BlobStatus, *IncompleteMeta, error) {
	blobPath := r.BlobPath(sha256)
	incompletePath := r.IncompletePath(sha256)
	metaPath := r.IncompleteMetaPath(sha256)

	// Check if complete blob exists
	if _, err := os.Stat(blobPath); err == nil {
		return BlobComplete, nil, nil
	}

	// Check if incomplete download exists
	incompleteStat, err := os.Stat(incompletePath)
	if errors.Is(err, os.ErrNotExist) {
		return BlobMissing, nil, nil
	}
	if err != nil {
		return BlobMissing, nil, fmt.Errorf("stat incomplete file: %w", err)
	}

	// Read metadata
	meta, err := r.readIncompleteMeta(metaPath)
	if err != nil {
		// If we can't read meta, treat as stale
		return BlobStale, nil, nil
	}

	// Check if the process is still alive
	if isProcessAlive(meta.PID) {
		// Check if file was recently modified
		if time.Since(incompleteStat.ModTime()) < r.cache.StaleTimeout {
			return BlobDownloading, meta, nil
		}
	}

	// Process is dead or file is stale
	return BlobStale, meta, nil
}

// readIncompleteMeta reads the metadata file for an incomplete download.
func (r *RepoDir) readIncompleteMeta(path string) (*IncompleteMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta IncompleteMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. We need to send signal 0 to check.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// FriendlyPath returns the path in the friendly view for this repo.
// Example: ~/.cache/huggingface/models/TheBloke/Mistral-7B-Instruct-v0.2-GGUF
func (r *RepoDir) FriendlyPath() string {
	if r.repoType == RepoTypeDataset {
		return filepath.Join(r.cache.DatasetsDir(), r.owner, r.name)
	}
	return filepath.Join(r.cache.ModelsDir(), r.owner, r.name)
}

// RepoID returns the repository ID in owner/name format.
func (r *RepoDir) RepoID() string {
	return r.owner + "/" + r.name
}

// Owner returns the repository owner.
func (r *RepoDir) Owner() string {
	return r.owner
}

// Name returns the repository name.
func (r *RepoDir) Name() string {
	return r.name
}

// Type returns the repository type (model or dataset).
func (r *RepoDir) Type() RepoType {
	return r.repoType
}

// --- Refs Management ---

// WriteRef writes a commit hash to a ref file.
// Example: WriteRef("main", "a1b2c3d4...") writes to refs/main
func (r *RepoDir) WriteRef(ref, commit string) error {
	if !ValidRevision(ref) || !ValidRevision(commit) || strings.Contains(commit, "/") {
		return fmt.Errorf("invalid ref %q -> %q", ref, commit)
	}
	refPath := r.RefPath(ref)
	if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
		return fmt.Errorf("create refs directory: %w", err)
	}
	return os.WriteFile(refPath, []byte(commit), 0644)
}

// ReadRef reads the commit hash from a ref file.
// Returns empty string if the ref doesn't exist.
func (r *RepoDir) ReadRef(ref string) (string, error) {
	if !ValidRevision(ref) {
		return "", fmt.Errorf("invalid revision %q", ref)
	}
	refPath := r.RefPath(ref)
	data, err := os.ReadFile(refPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// --- Snapshots Management ---

// SnapshotFile represents a file in a snapshot with its blob hash.
type SnapshotFile struct {
	// RelativePath is the path within the repo (e.g., "config.json" or "subdir/file.txt")
	RelativePath string
	// SHA256 is the blob hash for this file
	SHA256 string
}

// CreateSnapshot creates or updates a snapshot directory with symlinks to blobs.
// Uses relative symlinks for portability.
func (r *RepoDir) CreateSnapshot(commit string, files []SnapshotFile) error {
	snapshotDir := r.SnapshotDir(commit)

	// Create snapshot directory
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}

	for _, f := range files {
		if err := r.createSnapshotSymlink(commit, f.RelativePath, f.SHA256); err != nil {
			return fmt.Errorf("create symlink for %s: %w", f.RelativePath, err)
		}
	}

	return nil
}

// createSnapshotSymlink links snapshots/{commit}/{path} to blobs/{sha256}
// using the cache's link mode: a relative symlink like the HuggingFace Hub
// cache, or — where symlinks aren't available (Windows without Developer
// Mode) — a hardlink, else a copy. Windows used to skip snapshot links
// entirely, leaving files only as blobs/{sha256} that neither Python nor
// users could find.
func (r *RepoDir) createSnapshotSymlink(commit, relativePath, sha256 string) error {
	linkPath := filepath.Join(r.SnapshotDir(commit), filepath.FromSlash(relativePath))
	blobPath := r.BlobPath(sha256)
	target, err := filepath.Rel(filepath.Dir(linkPath), blobPath)
	if err != nil {
		return fmt.Errorf("calculate relative path: %w", err)
	}
	if _, err := placeLink(blobPath, linkPath, target, r.cache.Root, r.cache.LinkMode); err != nil {
		return err
	}
	return nil
}

// SnapshotPath returns the path to a file within a snapshot.
func (r *RepoDir) SnapshotPath(commit, relativePath string) string {
	return filepath.Join(r.SnapshotDir(commit), filepath.FromSlash(relativePath))
}

// ListSnapshots returns all commit hashes that have snapshots.
func (r *RepoDir) ListSnapshots() ([]string, error) {
	entries, err := os.ReadDir(r.SnapshotsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var commits []string
	for _, e := range entries {
		if e.IsDir() {
			commits = append(commits, e.Name())
		}
	}
	return commits, nil
}

// --- Friendly View Management ---

// CreateFriendlySymlink links the friendly view file
// models/{owner}/{name}/[filterSubdir/]{relativePath} to its snapshot file,
// using the cache's link mode (symlink, else hardlink). In copy mode — no
// links available at all — the friendly view is skipped rather than storing
// every file a third time.
func (r *RepoDir) CreateFriendlySymlink(commit, relativePath, filterSubdir string) error {
	if r.cache.LinkMode == LinkCopy || r.cache.copyOnly() {
		return nil
	}

	friendlyBase := r.FriendlyPath()
	var linkPath string
	if filterSubdir != "" {
		linkPath = filepath.Join(friendlyBase, filterSubdir, filepath.FromSlash(relativePath))
	} else {
		linkPath = filepath.Join(friendlyBase, filepath.FromSlash(relativePath))
	}

	snapshotPath := r.SnapshotPath(commit, relativePath)
	target, err := filepath.Rel(filepath.Dir(linkPath), snapshotPath)
	if err != nil {
		return fmt.Errorf("calculate relative path: %w", err)
	}
	mode := r.cache.LinkMode
	if mode == LinkAuto || mode == "" {
		// Never fall back to copying for the friendly view.
		if _, err := placeLink(snapshotPath, linkPath, target, r.cache.Root, LinkSymlink); err == nil {
			return nil
		}
		mode = LinkHardlink
	}
	if _, err := placeLink(snapshotPath, linkPath, target, r.cache.Root, mode); err != nil {
		if r.cache.LinkMode == LinkAuto || r.cache.LinkMode == "" {
			return nil // no links possible here; the HF cache itself is complete
		}
		return err
	}
	return nil
}

// EnsureFriendlyDir creates the friendly view directory for this repo.
func (r *RepoDir) EnsureFriendlyDir() error {
	return os.MkdirAll(r.FriendlyPath(), 0755)
}

// --- Download Workflow Helpers ---

// StoreFileResult contains the result of storing a downloaded file in the cache.
type StoreFileResult struct {
	BlobPath     string // Path to the blob file
	SnapshotPath string // Path to the snapshot symlink
	FriendlyPath string // Path to the friendly view symlink
	SHA256       string // The SHA256 hash used for the blob
}

// StoreDownloadedFile moves a downloaded file into the HF cache structure.
// It handles:
//  1. Computing SHA256 if not provided (for non-LFS files)
//  2. Moving/copying the file to blobs/{sha256}
//  3. Creating snapshot symlink
//  4. Creating friendly view symlink
//
// Parameters:
//   - tempFile: path to the downloaded file (will be moved/removed)
//   - relativePath: the file's path within the repo (e.g., "config.json")
//   - commit: the commit hash for the snapshot
//   - sha256: the known SHA256 (empty string if unknown, will be computed)
//   - filterSubdir: optional filter subdirectory for friendly view
//   - noFriendly: if true, skip creating friendly view symlink
func (r *RepoDir) StoreDownloadedFile(tempFile, relativePath, commit, sha256, filterSubdir string, noFriendly bool) (*StoreFileResult, error) {
	// Compute SHA256 if not provided
	if sha256 == "" {
		computed, err := computeSHA256(tempFile)
		if err != nil {
			return nil, fmt.Errorf("compute sha256: %w", err)
		}
		sha256 = computed
	}

	blobPath := r.BlobPath(sha256)

	// Check if blob already exists (deduplication)
	if _, err := os.Stat(blobPath); err == nil {
		// Blob exists, just remove temp file
		os.Remove(tempFile)
	} else {
		// Move temp file to blob location
		if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
			return nil, fmt.Errorf("create blobs directory: %w", err)
		}
		if err := os.Rename(tempFile, blobPath); err != nil {
			// Rename failed (cross-device?), try copy
			if err := copyFile(tempFile, blobPath); err != nil {
				return nil, fmt.Errorf("move file to blob: %w", err)
			}
			os.Remove(tempFile)
		}
	}

	// Create snapshot symlink
	if err := r.createSnapshotSymlink(commit, relativePath, sha256); err != nil {
		return nil, fmt.Errorf("create snapshot symlink: %w", err)
	}

	// Create friendly view symlink (unless disabled)
	if !noFriendly {
		if err := r.CreateFriendlySymlink(commit, relativePath, filterSubdir); err != nil {
			return nil, fmt.Errorf("create friendly symlink: %w", err)
		}
	}

	result := &StoreFileResult{
		BlobPath:     blobPath,
		SnapshotPath: r.SnapshotPath(commit, relativePath),
		SHA256:       sha256,
	}
	if !noFriendly {
		if filterSubdir != "" {
			result.FriendlyPath = filepath.Join(r.FriendlyPath(), filterSubdir, relativePath)
		} else {
			result.FriendlyPath = filepath.Join(r.FriendlyPath(), relativePath)
		}
	}

	return result, nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
