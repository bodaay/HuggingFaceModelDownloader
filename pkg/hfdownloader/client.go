// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultEndpoint is the default HuggingFace Hub URL.
// Can be overridden via Settings.Endpoint for mirrors or enterprise deployments.
// Credits: Custom endpoint feature suggested by windtail (#38)
const DefaultEndpoint = "https://huggingface.co"

// getEndpoint returns the endpoint to use, falling back to default if empty.
func getEndpoint(endpoint string) string {
	if endpoint == "" {
		return DefaultEndpoint
	}
	return strings.TrimSuffix(endpoint, "/")
}

// hfNode represents a file or directory in the HuggingFace repo tree.
type hfNode struct {
	Type   string     `json:"type"` // "file"|"directory" (sometimes "blob"|"tree")
	Path   string     `json:"path"`
	Size   int64      `json:"size,omitempty"`
	LFS    *hfLfsInfo `json:"lfs,omitempty"`
	Sha256 string     `json:"sha256,omitempty"`
}

// hfLfsInfo contains LFS metadata for large files.
type hfLfsInfo struct {
	Oid    string `json:"oid,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// buildHTTPClient creates an HTTP client with sensible defaults.
// Deprecated: Use BuildHTTPClient(proxy) for proxy support.
func buildHTTPClient() *http.Client {
	client, _ := BuildHTTPClient(nil)
	return client
}

// buildHTTPClientWithProxy creates an HTTP client with proxy support.
func buildHTTPClientWithProxy(proxy *ProxyConfig) *http.Client {
	client, err := BuildHTTPClient(proxy)
	if err != nil {
		// Fallback to no proxy on error
		client, _ = BuildHTTPClient(&ProxyConfig{NoEnvProxy: true})
	}
	return client
}

// addAuth adds authentication and user-agent headers to a request.
func addAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "hfdownloader/2")
}

// quickHeadAcceptRanges checks if a URL supports range requests.
func quickHeadAcceptRanges(ctx context.Context, httpc *http.Client, token string, urlStr string) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	return true, strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes")
}

// headForETag fetches ETag and SHA256 headers for a file.
func headForETag(ctx context.Context, httpc *http.Client, token string, it PlanItem) (etag string, remoteSha string, _ error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", it.URL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	return resp.Header.Get("ETag"), resp.Header.Get("x-amz-meta-sha256"), nil
}

// walkTree recursively walks the HuggingFace repo tree.
func walkTree(ctx context.Context, httpc *http.Client, token, endpoint string, job Job, prefix string, fn func(hfNode) error) error {
	reqURL := treeURL(endpoint, job, prefix)
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("401 unauthorized: repo requires token or you do not have access (visit %s)", agreementURL(endpoint, job))
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("403 forbidden: please accept the repository terms: %s", agreementURL(endpoint, job))
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("tree API failed: %s", resp.Status)
	}

	var nodes []hfNode
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&nodes); err != nil {
		return err
	}

	for _, n := range nodes {
		switch n.Type {
		case "directory", "tree":
			if err := walkTree(ctx, httpc, token, endpoint, job, n.Path, fn); err != nil {
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

// URL builders - all accept endpoint to support custom mirrors

func rawURL(endpoint string, job Job, path string) string {
	ep := getEndpoint(endpoint)
	// Note: job.Repo contains "/" which must NOT be escaped (HuggingFace requires literal slash)
	if job.IsDataset {
		return fmt.Sprintf("%s/datasets/%s/raw/%s/%s", ep, job.Repo, url.PathEscape(job.Revision), pathEscapeAll(path))
	}
	return fmt.Sprintf("%s/%s/raw/%s/%s", ep, job.Repo, url.PathEscape(job.Revision), pathEscapeAll(path))
}

func lfsURL(endpoint string, job Job, path string) string {
	ep := getEndpoint(endpoint)
	if job.IsDataset {
		return fmt.Sprintf("%s/datasets/%s/resolve/%s/%s", ep, job.Repo, url.PathEscape(job.Revision), pathEscapeAll(path))
	}
	return fmt.Sprintf("%s/%s/resolve/%s/%s", ep, job.Repo, url.PathEscape(job.Revision), pathEscapeAll(path))
}

func treeURL(endpoint string, job Job, prefix string) string {
	ep := getEndpoint(endpoint)
	// Build URL without trailing slash when prefix is empty
	if prefix == "" {
		if job.IsDataset {
			return fmt.Sprintf("%s/api/datasets/%s/tree/%s", ep, job.Repo, url.PathEscape(job.Revision))
		}
		return fmt.Sprintf("%s/api/models/%s/tree/%s", ep, job.Repo, url.PathEscape(job.Revision))
	}
	if job.IsDataset {
		return fmt.Sprintf("%s/api/datasets/%s/tree/%s/%s", ep, job.Repo, url.PathEscape(job.Revision), pathEscapeAll(prefix))
	}
	return fmt.Sprintf("%s/api/models/%s/tree/%s/%s", ep, job.Repo, url.PathEscape(job.Revision), pathEscapeAll(prefix))
}

func agreementURL(endpoint string, job Job) string {
	ep := getEndpoint(endpoint)
	if job.IsDataset {
		return fmt.Sprintf("%s/datasets/%s", ep, job.Repo)
	}
	return fmt.Sprintf("%s/%s", ep, job.Repo)
}

func pathEscapeAll(p string) string {
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return strings.Join(segs, "/")
}

// RepoInfo contains metadata about a HuggingFace repository.
type RepoInfo struct {
	SHA          string `json:"sha"`           // Commit hash
	LastModified string `json:"lastModified"`  // ISO timestamp
}

// fetchRepoInfo fetches repository metadata including the commit SHA for a given revision.
func fetchRepoInfo(ctx context.Context, httpc *http.Client, token, endpoint string, job Job) (*RepoInfo, error) {
	ep := getEndpoint(endpoint)
	var reqURL string
	if job.IsDataset {
		reqURL = fmt.Sprintf("%s/api/datasets/%s/revision/%s", ep, job.Repo, url.PathEscape(job.Revision))
	} else {
		reqURL = fmt.Sprintf("%s/api/models/%s/revision/%s", ep, job.Repo, url.PathEscape(job.Revision))
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	addAuth(req, token)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("repo info API failed: %s", resp.Status)
	}

	var info RepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}


