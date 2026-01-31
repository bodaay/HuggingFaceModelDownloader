// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
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
	files, detectedIsDataset, err := a.fetchFileTreeAutoDetect(ctx, repo, isDataset, revision)
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
	}

	// Fetch and parse metadata files based on detected type
	if err := a.fetchMetadata(ctx, repo, isDataset, info); err != nil {
		// Non-fatal: continue with partial info
		_ = err
	}

	// Run type-specific analysis
	a.analyzeTypeSpecific(info)

	// Populate SelectableItems based on type
	populateSelectableItems(info)

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
// Returns the files, whether it's a dataset, and any error.
// If both model and dataset exist, returns ErrBothExist.
func (a *Analyzer) fetchFileTreeAutoDetect(ctx context.Context, repo string, isDataset bool, revision string) ([]FileInfo, bool, error) {
	// If explicitly marked as dataset, fetch as dataset directly
	if isDataset {
		files, err := a.fetchFileTree(ctx, repo, true, revision)
		return files, true, err
	}

	// Try as model first
	modelFiles, modelErr := a.fetchFileTree(ctx, repo, false, revision)

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
		datasetFiles, datasetErr := a.fetchFileTree(ctx, repo, true, revision)
		if datasetErr == nil {
			return datasetFiles, true, nil
		}
		// If dataset also fails with not found/unauthorized, return helpful error
		if isModelNotFound(datasetErr) {
			return nil, false, fmt.Errorf("repository not found as model or dataset: %s", repo)
		}
		// Dataset failed with different error (actual auth issue, network, etc.)
		return nil, false, datasetErr
	}

	// If model found, check if dataset also exists
	if modelErr == nil {
		_, datasetErr := a.fetchFileTree(ctx, repo, true, revision)
		if datasetErr == nil {
			// Both exist - return error so caller can ask user
			return nil, false, ErrBothExist
		}
		// Only model exists
		return modelFiles, false, nil
	}

	// Return original error for other failures (network, etc.)
	return nil, false, modelErr
}

// fetchFileTree recursively fetches the file tree from HuggingFace API.
func (a *Analyzer) fetchFileTree(ctx context.Context, repo string, isDataset bool, revision string) ([]FileInfo, error) {
	var files []FileInfo
	err := a.walkTree(ctx, repo, isDataset, revision, "", func(node hfTreeNode) error {
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
	return files, err
}

// walkTree recursively walks the repository tree.
func (a *Analyzer) walkTree(ctx context.Context, repo string, isDataset bool, revision, prefix string, fn func(hfTreeNode) error) error {
	reqURL := a.treeURL(repo, isDataset, revision, prefix)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	a.addAuth(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("unauthorized: repo requires token or you do not have access")
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("forbidden: please accept the repository terms at %s", a.repoURL(repo, isDataset))
	}
	if resp.StatusCode == 404 {
		return fmt.Errorf("repository not found: %s", repo)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("API error: %s", resp.Status)
	}

	var nodes []hfTreeNode
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	for _, n := range nodes {
		switch n.Type {
		case "directory", "tree":
			if err := a.walkTree(ctx, repo, isDataset, revision, n.Path, fn); err != nil {
				return err
			}
		default:
			if err := fn(n); err != nil {
				return err
			}
		}
	}
	return nil
}

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
	Name      string `json:"name"`
	Ref       string `json:"ref"`
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
	hasFile := make(map[string]bool)
	var extensions []string
	for _, f := range files {
		hasFile[f.Path] = true
		hasFile[f.Name] = true
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

	// 3. LoRA/Adapter - adapter_config.json
	if hasFile["adapter_config.json"] {
		return TypeLoRA
	}

	// 4. GPTQ/AWQ - quantize_config.json
	if hasFile["quantize_config.json"] {
		// Will refine to GPTQ vs AWQ when we parse the config
		return TypeGPTQ
	}

	// 5. ONNX - presence of .onnx files
	for _, ext := range extensions {
		if ext == ".onnx" {
			return TypeONNX
		}
	}

	// 6. Transformers - config.json + safetensors/bin
	if hasFile["config.json"] {
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

	// 7. ONNX - presence of .onnx files (if not already detected as other type)
	for _, ext := range extensions {
		if ext == ".onnx" {
			return TypeONNX
		}
	}

	return TypeGeneric
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
	case TypeGPTQ, TypeAWQ:
		filesToFetch = []string{"quantize_config.json", "config.json"}
	case TypeTransformers:
		filesToFetch = []string{"config.json", "tokenizer_config.json", "generation_config.json", "preprocessor_config.json", "processor_config.json"}
	case TypeONNX:
		filesToFetch = []string{"config.json"}
	default:
		// For generic/undetected types, fetch all possible config files to help refine detection
		filesToFetch = []string{"config.json", "preprocessor_config.json", "processor_config.json"}
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

	// Limit to 10MB for config files
	const maxSize = 10 * 1024 * 1024
	buf := make([]byte, maxSize)
	n, _ := resp.Body.Read(buf)
	return buf[:n], nil
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
	case TypeGPTQ, TypeAWQ:
		info.Quantized = analyzeQuantized(info.Metadata)
		// Refine type based on actual method
		if info.Quantized != nil && info.Quantized.Method == "awq" {
			info.Type = TypeAWQ
			info.TypeDescription = info.Type.Description()
		}
	case TypeDataset:
		info.Dataset = analyzeDataset(info.Files)
	case TypeONNX:
		info.ONNX = analyzeONNX(info.Files)
	case TypeTransformers:
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
	case TypeGPTQ, TypeAWQ:
		info.SelectableItems = QuantizedToSelectableItems(info.Quantized, info.Files)
	}
}
