// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/hubtree"
)

const defaultEndpoint = "https://huggingface.co"

// AnalyzerOptions configures the Analyzer.
type AnalyzerOptions struct {
	// Token is the HuggingFace access token for private repos.
	Token string

	// Endpoint is the HuggingFace Hub base URL (default: https://huggingface.co).
	Endpoint string

	// HTTPClient is an optional custom HTTP client.
	HTTPClient *http.Client
}

// Analyzer analyzes HuggingFace repositories to determine their type and structure.
type Analyzer struct {
	token    string
	endpoint string
	client   *http.Client
}

// NewAnalyzer creates a new Analyzer with the given options.
func NewAnalyzer(opts AnalyzerOptions) *Analyzer {
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	endpoint = strings.TrimSuffix(endpoint, "/")

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	}

	return &Analyzer{
		token:    opts.Token,
		endpoint: endpoint,
		client:   client,
	}
}

// Analyze fetches and analyzes a HuggingFace repository using the default "main" revision.
// If isDataset is false, it will first try to fetch as a model, then as a dataset if not found.
func (a *Analyzer) Analyze(ctx context.Context, repo string, isDataset bool) (*RepoInfo, error) {
	return a.AnalyzeWithRevision(ctx, repo, isDataset, "main")
}

// AnalyzeWithRevision fetches and analyzes a HuggingFace repository at a specific revision.
// If isDataset is false, it will first try to fetch as a model, then as a dataset if not found.
func (a *Analyzer) AnalyzeWithRevision(ctx context.Context, repo string, isDataset bool, revision string) (*RepoInfo, error) {
	if revision == "" {
		revision = "main"
	}

	// Fetch file tree - auto-detect model vs dataset if not explicitly a dataset
	files, detectedIsDataset, commit, err := a.fetchFileTreeAutoDetect(ctx, repo, isDataset, revision)
	if err != nil {
		return nil, fmt.Errorf("fetch file tree: %w", err)
	}
	isDataset = detectedIsDataset

	// Build RepoInfo
	info := &RepoInfo{
		Repo:       repo,
		IsDataset:  isDataset,
		Files:      files,
		FileCount:  len(files),
		Commit:     commit,
		Branch:     revision,
		AnalyzedAt: time.Now().UTC(),
		Metadata:   make(map[string]interface{}),
	}

	// Calculate total size
	for _, f := range files {
		info.TotalSize += f.Size
	}
	info.TotalSizeHuman = humanSize(info.TotalSize)

	// Detect type
	info.Type = a.detectType(files, isDataset)
	info.TypeDescription = info.Type.Description()

	// Fetch refs (branches/tags) - non-fatal if fails
	if refs, err := a.fetchRefs(ctx, repo, isDataset); err == nil {
		info.Refs = refs
		// Fallback: if the tree API didn't return X-Repo-Commit, resolve the
		// commit by matching the requested revision against a branch/tag.
		if info.Commit == "" {
			for _, ref := range refs {
				if ref.Name == revision && ref.Commit != "" {
					info.Commit = ref.Commit
					break
				}
			}
		}
	}

	// Fetch and parse metadata files based on detected type
	if err := a.fetchMetadata(ctx, repo, isDataset, info); err != nil {
		// Non-fatal: continue with partial info
		_ = err
	}

	// Run type-specific analysis
	a.analyzeTypeSpecific(info)

	// Quantizations stored on branches (EXL2/EXL3, github issue #94): main
	// holds only measurement files; list the bitrate branches as choices.
	if !isDataset && revision == "main" {
		if branches := quantBranchesFromRefs(info.Refs); len(branches) > 0 && rootWeightBytes(files) < 50<<20 {
			method := a.branchQuantMethod(ctx, repo, branches)
			a.fillBranchSizes(ctx, repo, branches)
			info.QuantBranches = branches
			info.Type = TypeQuantized
			if info.Quantized == nil {
				info.Quantized = &QuantizedInfo{Method: method, MethodDescription: quantMethodDescriptions[method]}
				info.Quantized.Backends = detectBackends(info.Quantized)
			}
			name := strings.ToUpper(method)
			if name == "" {
				name = "Quantized"
			}
			info.TypeDescription = fmt.Sprintf("%s quantized model, %d bitrates on separate branches", name, len(branches))
		}
	}

	// Populate SelectableItems based on type
	populateSelectableItems(info)
	if len(info.QuantBranches) > 0 {
		method := ""
		if info.Quantized != nil {
			method = info.Quantized.Method
		}
		info.SelectableItems = append(info.SelectableItems, QuantBranchItems(info.QuantBranches, method)...)
	}

	// Generate CLI commands
	info.PopulateCLICommands()

	return info, nil
}

// hfTreeNode represents a node in the HF tree API response.
type hfTreeNode struct {
	Type string `json:"type"` // "file" or "directory"
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
	LFS  *struct {
		Size   int64  `json:"size,omitempty"`
		SHA256 string `json:"sha256,omitempty"`
		OID    string `json:"oid,omitempty"`
	} `json:"lfs,omitempty"`
}

// ErrBothExist is returned when a repo exists as both model and dataset.
var ErrBothExist = fmt.Errorf("repository exists as both model and dataset")

// fetchFileTreeAutoDetect tries to fetch as model first, then as dataset if 404.
// Returns the files, whether it's a dataset, the resolved commit SHA, and any
// error. If both model and dataset exist, returns ErrBothExist.
func (a *Analyzer) fetchFileTreeAutoDetect(ctx context.Context, repo string, isDataset bool, revision string) ([]FileInfo, bool, string, error) {
	// If explicitly marked as dataset, fetch as dataset directly
	if isDataset {
		files, commit, err := a.fetchFileTree(ctx, repo, true, revision)
		return files, true, commit, err
	}

	// Try as model first
	modelFiles, modelCommit, modelErr := a.fetchFileTree(ctx, repo, false, revision)

	// Helper to check if error indicates repo doesn't exist as model
	// HuggingFace returns "not found" for missing repos, but also "unauthorized"
	// when trying to access a datasets-only repo via the models API
	isModelNotFound := func(err error) bool {
		if err == nil {
			return false
		}
		errStr := strings.ToLower(err.Error())
		return strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "unauthorized") ||
			strings.Contains(errStr, "401")
	}

	// If model not found or unauthorized, try as dataset
	if isModelNotFound(modelErr) {
		datasetFiles, datasetCommit, datasetErr := a.fetchFileTree(ctx, repo, true, revision)
		if datasetErr == nil {
			return datasetFiles, true, datasetCommit, nil
		}
		// If dataset also fails with not found/unauthorized, return helpful error
		if isModelNotFound(datasetErr) {
			return nil, false, "", fmt.Errorf("repository not found as model or dataset: %s", repo)
		}
		// Dataset failed with different error (actual auth issue, network, etc.)
		return nil, false, "", datasetErr
	}

	// If model found, check if dataset also exists
	if modelErr == nil {
		_, _, datasetErr := a.fetchFileTree(ctx, repo, true, revision)
		if datasetErr == nil {
			// Both exist - return error so caller can ask user
			return nil, false, "", ErrBothExist
		}
		// Only model exists
		return modelFiles, false, modelCommit, nil
	}

	// Return original error for other failures (network, etc.)
	return nil, false, "", modelErr
}

// fetchFileTree recursively fetches the file tree from HuggingFace API.
// The second return value is the resolved commit SHA for the revision (from
// the X-Repo-Commit response header), or "" if the API did not provide it.
func (a *Analyzer) fetchFileTree(ctx context.Context, repo string, isDataset bool, revision string) ([]FileInfo, string, error) {
	var files []FileInfo
	var commit string
	err := a.walkTree(ctx, repo, isDataset, revision, "", &commit, func(node hfTreeNode) error {
		if node.Type == "file" || node.Type == "blob" {
			size := node.Size
			isLFS := false
			sha256 := ""
			if node.LFS != nil {
				size = node.LFS.Size
				isLFS = true
				sha256 = node.LFS.SHA256
				if sha256 == "" {
					sha256 = node.LFS.OID
				}
			}

			files = append(files, FileInfo{
				Path:      node.Path,
				Name:      filepath.Base(node.Path),
				Size:      size,
				SizeHuman: humanSize(size),
				IsLFS:     isLFS,
				SHA256:    sha256,
				Directory: filepath.Dir(node.Path),
			})
		}
		return nil
	})
	return files, commit, err
}

// walkTree lists every file in the repository tree (recursive, paginated;
// see internal/hubtree). When commitOut is non-nil and still empty, the
// resolved commit SHA is captured from the X-Repo-Commit response header
// that HuggingFace returns for the requested revision.
func (a *Analyzer) walkTree(ctx context.Context, repo string, isDataset bool, revision, prefix string, commitOut *string, fn func(hfTreeNode) error) error {
	return hubtree.Walk(ctx, hubtree.Options{
		Client: a.client,
		URL: func(dir string) string {
			// dir is a full path from the repo root ("" = where the walk starts).
			if dir == "" {
				dir = prefix
			}
			return a.treeURL(repo, isDataset, revision, dir)
		},
		Authorize: a.addAuth,
		OnResponse: func(resp *http.Response) {
			// Capture the resolved commit from the first response that carries it.
			if commitOut != nil && *commitOut == "" {
				if c := resp.Header.Get("X-Repo-Commit"); c != "" {
					*commitOut = c
				}
			}
		},
		Status: func(resp *http.Response) error {
			switch resp.StatusCode {
			case 401:
				return fmt.Errorf("unauthorized: repo requires token or you do not have access")
			case 403:
				return fmt.Errorf("forbidden: please accept the repository terms at %s", a.repoURL(repo, isDataset))
			case 404:
				return fmt.Errorf("repository not found: %s", repo)
			}
			return fmt.Errorf("API error: %s", resp.Status)
		},
	}, fn)
}

// NodePath and IsDir let hfTreeNode be listed by hubtree.Walk.
func (n hfTreeNode) NodePath() string { return n.Path }
func (n hfTreeNode) IsDir() bool      { return n.Type == "directory" || n.Type == "tree" }

// treeURL builds the tree API URL.
func (a *Analyzer) treeURL(repo string, isDataset bool, revision, prefix string) string {
	var base string
	if isDataset {
		base = fmt.Sprintf("%s/api/datasets/%s/tree/%s", a.endpoint, repo, url.PathEscape(revision))
	} else {
		base = fmt.Sprintf("%s/api/models/%s/tree/%s", a.endpoint, repo, url.PathEscape(revision))
	}
	if prefix != "" {
		base += "/" + pathEscapeAll(prefix)
	}
	return base
}

// repoURL builds the repository page URL.
func (a *Analyzer) repoURL(repo string, isDataset bool) string {
	if isDataset {
		return fmt.Sprintf("%s/datasets/%s", a.endpoint, repo)
	}
	return fmt.Sprintf("%s/%s", a.endpoint, repo)
}

// rawURL builds the raw file URL for fetching content.
func (a *Analyzer) rawURL(repo string, isDataset bool, revision, path string) string {
	if isDataset {
		return fmt.Sprintf("%s/datasets/%s/raw/%s/%s", a.endpoint, repo, url.PathEscape(revision), pathEscapeAll(path))
	}
	return fmt.Sprintf("%s/%s/raw/%s/%s", a.endpoint, repo, url.PathEscape(revision), pathEscapeAll(path))
}

// hfRefsResponse represents the HuggingFace refs API response.
type hfRefsResponse struct {
	Branches []hfRef `json:"branches"`
	Tags     []hfRef `json:"tags"`
}

type hfRef struct {
	Name         string `json:"name"`
	Ref          string `json:"ref"`
	TargetCommit string `json:"targetCommit"`
}

// fetchRefs fetches available branches and tags from the repository.
func (a *Analyzer) fetchRefs(ctx context.Context, repo string, isDataset bool) ([]RepoRef, error) {
	var apiPath string
	if isDataset {
		apiPath = fmt.Sprintf("%s/api/datasets/%s/refs", a.endpoint, repo)
	} else {
		apiPath = fmt.Sprintf("%s/api/models/%s/refs", a.endpoint, repo)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiPath, nil)
	if err != nil {
		return nil, err
	}
	a.addAuth(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch refs: %s", resp.Status)
	}

	var refsResp hfRefsResponse
	if err := json.NewDecoder(resp.Body).Decode(&refsResp); err != nil {
		return nil, fmt.Errorf("decode refs: %w", err)
	}

	var refs []RepoRef
	for _, b := range refsResp.Branches {
		refs = append(refs, RepoRef{
			Name:   b.Name,
			Type:   "branch",
			Commit: b.TargetCommit,
		})
	}
	for _, t := range refsResp.Tags {
		refs = append(refs, RepoRef{
			Name:   t.Name,
			Type:   "tag",
			Commit: t.TargetCommit,
		})
	}

	return refs, nil
}

// addAuth adds authentication headers to a request.
func (a *Analyzer) addAuth(req *http.Request) {
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("User-Agent", "hfdownloader/3")
}

// pathEscapeAll escapes each path segment.
func pathEscapeAll(p string) string {
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return strings.Join(segs, "/")
}

// detectType determines the repository type based on files present.
func (a *Analyzer) detectType(files []FileInfo, isDataset bool) RepoType {
	if isDataset {
		return TypeDataset
	}

	// Build file index for quick lookups
	hasFile := make(map[string]bool)  // by path or base name (any folder)
	rootFile := make(map[string]bool) // files in the repo root
	var extensions []string
	for _, f := range files {
		hasFile[f.Path] = true
		hasFile[f.Name] = true
		if !strings.Contains(f.Path, "/") {
			rootFile[f.Path] = true
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		extensions = append(extensions, ext)
	}

	// Priority-based detection

	// 1. GGUF - presence of .gguf files
	for _, ext := range extensions {
		if ext == ".gguf" {
			return TypeGGUF
		}
	}

	// 2. Diffusers - model_index.json is the definitive marker
	if hasFile["model_index.json"] {
		return TypeDiffusers
	}

	// 3. LoRA/Adapter - adapter_config.json, or a diffusers LoRA
	// (pytorch_lora_weights.safetensors, e.g. latent-consistency/lcm-lora-sdxl)
	if hasFile["adapter_config.json"] || hasFile["pytorch_lora_weights.safetensors"] || hasFile["pytorch_lora_weights.bin"] {
		return TypeLoRA
	}

	// 4. GPTQ/AWQ - quantize_config.json next to root PyTorch weights.
	// transformers.js repos (Xenova/*, onnx-community/*) carry an ONNX
	// runtime quantize_config.json with only onnx/ weights.
	if rootFile["quantize_config.json"] && hasRootWeights(files) {
		// Will refine to GPTQ vs AWQ when we parse the config
		return TypeGPTQ
	}

	// 5. Transformers - root config.json + safetensors/bin. Checked before
	// ONNX: many PyTorch repos also ship an onnx/ export (gpt2,
	// sentence-transformers, SmolVLM) and must not be labeled ONNX-only.
	if rootFile["config.json"] {
		hasSafetensors := false
		hasBin := false
		for _, ext := range extensions {
			if ext == ".safetensors" {
				hasSafetensors = true
			}
			if ext == ".bin" {
				hasBin = true
			}
		}
		if hasSafetensors || hasBin {
			return TypeTransformers
		}
	}

	// 6. ONNX - presence of .onnx files (if not already detected as other type)
	for _, ext := range extensions {
		if ext == ".onnx" {
			return TypeONNX
		}
	}

	return TypeGeneric
}

// fileSize returns the size of the file at path in files, or 0.
func fileSize(files []FileInfo, path string) int64 {
	for _, f := range files {
		if f.Path == path {
			return f.Size
		}
	}
	return 0
}

// rootWeightBytes is the total size of .safetensors/.bin files in the repo
// root. Branch-per-bitrate repos keep only small calibration files on main
// (turboderp's cal_trace.safetensors, ~4 MB).
func rootWeightBytes(files []FileInfo) int64 {
	var n int64
	for _, f := range files {
		if strings.Contains(f.Path, "/") {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(f.Path)); ext == ".safetensors" || ext == ".bin" {
			n += f.Size
		}
	}
	return n
}

// hasRootWeights reports whether the repo root holds PyTorch weights.
func hasRootWeights(files []FileInfo) bool {
	for _, f := range files {
		if strings.Contains(f.Path, "/") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Path))
		if ext == ".safetensors" || ext == ".bin" {
			return true
		}
	}
	return false
}

// fetchMetadata fetches and parses relevant config files.
func (a *Analyzer) fetchMetadata(ctx context.Context, repo string, isDataset bool, info *RepoInfo) error {
	// Determine which files to fetch based on detected type
	var filesToFetch []string
	switch info.Type {
	case TypeGGUF:
		filesToFetch = []string{"config.json", "README.md"}
	case TypeDiffusers:
		filesToFetch = []string{"model_index.json"}
	case TypeLoRA:
		filesToFetch = []string{"adapter_config.json"}
	case TypeGPTQ, TypeAWQ, TypeQuantized:
		filesToFetch = []string{"quantize_config.json", "config.json"}
	case TypeTransformers:
		filesToFetch = []string{"config.json", "quantization_config.json", "tokenizer_config.json", "generation_config.json", "preprocessor_config.json", "processor_config.json"}
	case TypeONNX:
		filesToFetch = []string{"config.json"}
	default:
		// For generic/undetected types, fetch all possible config files to help refine detection
		filesToFetch = []string{"config.json", "quantization_config.json", "preprocessor_config.json", "processor_config.json"}
	}

	for _, path := range filesToFetch {
		// Check if file exists
		found := false
		for _, f := range info.Files {
			if f.Path == path {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		if size := fileSize(info.Files, path); size > maxMetadataSize {
			if head, err := a.fetchJSONHead(ctx, repo, isDataset, info.Branch, path); err == nil && len(head) > 0 {
				info.Metadata[path] = head
			}
			continue
		}

		content, err := a.fetchFile(ctx, repo, isDataset, info.Branch, path)
		if err != nil {
			continue // Non-fatal
		}

		// Parse JSON content
		var data interface{}
		if err := json.Unmarshal(content, &data); err != nil {
			continue
		}
		info.Metadata[path] = data
	}

	return nil
}

// fetchFile fetches raw file content from the repository.
func (a *Analyzer) fetchFile(ctx context.Context, repo string, isDataset bool, revision, path string) ([]byte, error) {
	reqURL := a.rawURL(repo, isDataset, revision, path)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	a.addAuth(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch %s: %s", path, resp.Status)
	}

	// Read the whole file, up to maxMetadataSize. (A single Body.Read used
	// to return only the first network chunk, so any config over ~16-32 KB —
	// common for vision-language models — was truncated and then dropped as
	// invalid JSON.)
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataSize+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", path, err)
	}
	if len(data) > maxMetadataSize {
		return nil, fmt.Errorf("fetch %s: larger than %d bytes", path, maxMetadataSize)
	}
	return data, nil
}

// maxMetadataSize caps metadata files read whole.
const maxMetadataSize = 10 << 20

// metadataHeadSize is how much of an oversized JSON file is read to recover
// its leading top-level settings.
const metadataHeadSize = 64 << 10

// fetchJSONHead reads the first metadataHeadSize bytes of a JSON object and
// decodes the top-level members that fit, stopping at the first one that
// doesn't. EXL3's quantization_config.json is tens of MB of per-tensor
// settings, but quant_method/bits/head_bits come first.
func (a *Analyzer) fetchJSONHead(ctx context.Context, repo string, isDataset bool, revision, path string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.rawURL(repo, isDataset, revision, path), nil)
	if err != nil {
		return nil, err
	}
	a.addAuth(req)
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", metadataHeadSize-1))
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("fetch %s: %s", path, resp.Status)
	}
	return decodeJSONHead(io.LimitReader(resp.Body, metadataHeadSize))
}

// decodeJSONHead decodes the top-level members of a (possibly truncated)
// JSON object, returning those decoded before the data ran out.
func decodeJSONHead(r io.Reader) (map[string]interface{}, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("not a JSON object")
	}
	out := map[string]interface{}{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := tok.(string)
		if !ok {
			break
		}
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			break
		}
		out[key] = v
	}
	return out, nil
}

// analyzeTypeSpecific runs type-specific analysis.
func (a *Analyzer) analyzeTypeSpecific(info *RepoInfo) {
	switch info.Type {
	case TypeGGUF:
		info.GGUF = analyzeGGUF(info.Files)
	case TypeDiffusers:
		info.Diffusers = analyzeDiffusers(info.Files, info.Metadata)
	case TypeLoRA:
		info.LoRA = analyzeLoRA(info.Metadata)
	case TypeGPTQ, TypeAWQ, TypeQuantized:
		info.Quantized = analyzeQuantized(info.Metadata)
		// Refine type based on actual method
		if info.Quantized != nil {
			info.Type = quantizedRepoType(info.Quantized.Method)
			info.TypeDescription = quantizedTypeDescription(info.Quantized)
		}
	case TypeDataset:
		info.Dataset = analyzeDataset(info.Files)
	case TypeONNX:
		info.ONNX = analyzeONNX(info.Files)
	case TypeTransformers:
		// A quantization method declared in config.json (GPTQ, AWQ,
		// bitsandbytes, FP8, MLX, EXL3, ...) matters most for what to
		// download and run, so it takes precedence over the model family.
		if q := analyzeQuantized(info.Metadata); q != nil && q.Method != "" {
			info.Type = TypeQuantized
			a.analyzeTypeSpecific(info)
			return
		}
		// For transformers, first try to detect specialized types from metadata
		specializedType := detectSpecializedType(info.Files, info.Metadata)
		if specializedType != "" {
			info.Type = specializedType
			info.TypeDescription = info.Type.Description()
			// Re-run analysis for the specialized type
			a.analyzeTypeSpecific(info)
			return
		}
		// Standard transformers analysis
		info.Transformers = analyzeTransformers(info.Files, info.Metadata)
	case TypeGeneric:
		if q := analyzeQuantized(info.Metadata); q != nil && q.Method != "" {
			info.Type = TypeQuantized
			a.analyzeTypeSpecific(info)
			return
		}
		// For generic, try to detect specialized types from metadata
		specializedType := detectSpecializedType(info.Files, info.Metadata)
		if specializedType != "" {
			info.Type = specializedType
			info.TypeDescription = info.Type.Description()
			// Re-run analysis for the specialized type
			a.analyzeTypeSpecific(info)
			return
		}
	}

	// For specialized types, run the appropriate analyzer
	switch info.Type {
	case TypeAudio:
		info.Audio = analyzeAudio(info.Files, info.Metadata)
	case TypeVision:
		info.Vision = analyzeVision(info.Files, info.Metadata)
	case TypeMultimodal:
		info.Multimodal = analyzeMultimodal(info.Files, info.Metadata)
	}
}

// humanSize formats bytes as human-readable size.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// populateSelectableItems converts type-specific data to unified SelectableItems.
func populateSelectableItems(info *RepoInfo) {
	switch info.Type {
	case TypeGGUF:
		info.SelectableItems = GGUFToSelectableItems(info.GGUF)
	case TypeDiffusers:
		info.SelectableItems = DiffusersToSelectableItems(info.Diffusers)
	case TypeTransformers:
		info.SelectableItems = TransformersToSelectableItems(info.Transformers, info.Files)
	case TypeDataset:
		info.SelectableItems = DatasetToSelectableItems(info.Dataset)
	case TypeLoRA:
		info.RelatedDownloads = LoRAToRelatedDownloads(info.LoRA)
	case TypeGPTQ, TypeAWQ, TypeQuantized:
		info.SelectableItems = QuantizedToSelectableItems(info.Quantized, info.Files)
	case TypeAudio, TypeVision, TypeMultimodal:
		// e.g. whisper and SmolVLM ship PyTorch, ONNX, TF and Flax weights.
		info.SelectableItems = WeightFormatItems(info.Files)
	}
}
