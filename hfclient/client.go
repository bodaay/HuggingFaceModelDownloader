package hfclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	token      string
	httpClient *http.Client
}

type File struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Type    string `json:"type"`
	Sha     string `json:"oid"`
	IsLFS   bool
	Pattern string   // The pattern that matched this file
	RepoRef *RepoRef // Reference to the repository this file belongs to
	Lfs     *LfsInfo `json:"lfs,omitempty"`
}

type LfsInfo struct {
	Oid  string `json:"oid"` // in lfs, oid is sha256 of the file
	Size int64  `json:"size"`
}

// GetSha returns the correct SHA based on whether the file is LFS or not
func (f *File) GetSha() string {
	if f.IsLFS && f.Lfs != nil {
		return f.Lfs.Oid
	}
	return f.Sha
}

type DownloadTask struct {
	File        *File
	Destination string
}

func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *Client) ListFiles(repo *RepoRef) ([]*File, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/%s/%s",
		repo.FullName(), repo.Ref, "")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized: please provide a valid token using -t flag or HF_TOKEN environment variable")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	var files []*File
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}

	// Post-process files to identify LFS objects and set RepoRef
	for _, file := range files {
		file.IsLFS = file.Size > 0 && file.Sha != ""
		file.RepoRef = repo
	}

	return files, nil
}

// encodeFilePath URL encodes each path segment separately to handle spaces and special characters
func (c *Client) encodeFilePath(path string) string {
	pathParts := strings.Split(path, "/")
	for i, part := range pathParts {
		pathParts[i] = url.PathEscape(part)
	}
	return strings.Join(pathParts, "/")
}

// getResolverURL constructs the HuggingFace resolver URL for a file
func (c *Client) getResolverURL(file *File) string {
	encodedPath := c.encodeFilePath(file.Path)
	return fmt.Sprintf("https://huggingface.co/%s/resolve/%s/%s",
		file.RepoRef.FullName(), file.RepoRef.Ref, encodedPath)
}

func (c *Client) getDownloadURL(file *File) (string, error) {
	if file.IsLFS {
		// For LFS files, we need to get the actual download URL via the resolver
		return c.getLFSDownloadURL(file)
	}

	// For regular files, we can construct the URL directly
	return c.getResolverURL(file), nil
}

func (c *Client) getLFSDownloadURL(file *File) (string, error) {
	// First get the resolver URL
	resolverURL := c.getResolverURL(file)
	fmt.Printf("DEBUG: Resolver URL: %s\n", resolverURL)

	// Create request to get the actual download URL
	req, err := http.NewRequest("GET", resolverURL, nil)
	if err != nil {
		return "", err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	// Use a client that doesn't follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("unauthorized: please provide a valid token using -t flag or HF_TOKEN environment variable")
	}

	// Handle both redirect and direct file serving cases
	switch resp.StatusCode {
	case http.StatusFound:
		// Got a redirect - use the Location header
		location := resp.Header.Get("Location")
		return location, nil
	case http.StatusOK:
		// File is being served directly - use the original resolver URL
		return resolverURL, nil
	default:
		// Any other status code is an error
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
