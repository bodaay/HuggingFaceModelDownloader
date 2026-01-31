// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyConfig holds proxy configuration settings.
// Supports HTTP, HTTPS, and SOCKS5 proxies with optional authentication.
type ProxyConfig struct {
	// URL is the proxy server URL.
	// Supported schemes:
	//   - http://host:port
	//   - https://host:port
	//   - socks5://host:port
	//   - socks5h://host:port (DNS resolution via proxy)
	//
	// Authentication can be embedded in the URL:
	//   - http://user:pass@host:port
	//   - socks5://user:pass@host:port
	//
	// If empty, falls back to environment variables (HTTP_PROXY, HTTPS_PROXY)
	// unless NoEnvProxy is set.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// Username for proxy authentication (alternative to embedding in URL).
	Username string `json:"username,omitempty" yaml:"username,omitempty"`

	// Password for proxy authentication (alternative to embedding in URL).
	Password string `json:"password,omitempty" yaml:"password,omitempty"`

	// NoProxy is a comma-separated list of hosts that should bypass the proxy.
	// Supports:
	//   - Exact hostnames: "localhost"
	//   - Domain suffixes: ".internal.com"
	//   - CIDR ranges: "10.0.0.0/8"
	//   - Wildcard: "*" (bypass all)
	//
	// If empty, falls back to NO_PROXY environment variable.
	NoProxy string `json:"no_proxy,omitempty" yaml:"no_proxy,omitempty"`

	// NoEnvProxy disables reading proxy settings from environment variables.
	// When true, only explicit ProxyConfig settings are used.
	NoEnvProxy bool `json:"no_env_proxy,omitempty" yaml:"no_env_proxy,omitempty"`

	// InsecureSkipVerify disables TLS certificate verification for HTTPS proxies.
	// WARNING: Only use for testing or with trusted proxies.
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty" yaml:"insecure_skip_verify,omitempty"`
}

// IsConfigured returns true if explicit proxy settings are provided.
func (p *ProxyConfig) IsConfigured() bool {
	return p != nil && p.URL != ""
}

// GetProxyURL returns the parsed proxy URL with authentication applied.
// Returns nil if no proxy is configured.
func (p *ProxyConfig) GetProxyURL() (*url.URL, error) {
	if p == nil || p.URL == "" {
		return nil, nil
	}

	proxyURL, err := url.Parse(p.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	// Apply username/password if not already in URL
	if proxyURL.User == nil && p.Username != "" {
		if p.Password != "" {
			proxyURL.User = url.UserPassword(p.Username, p.Password)
		} else {
			proxyURL.User = url.User(p.Username)
		}
	}

	return proxyURL, nil
}

// IsSocks returns true if the proxy uses SOCKS5 protocol.
func (p *ProxyConfig) IsSocks() bool {
	if p == nil || p.URL == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(p.URL), "socks5://") ||
		strings.HasPrefix(strings.ToLower(p.URL), "socks5h://")
}

// BuildHTTPClient creates an HTTP client with proxy support.
// If proxy is nil or not configured, falls back to environment proxy settings.
func BuildHTTPClient(proxy *ProxyConfig) (*http.Client, error) {
	if proxy != nil && proxy.IsSocks() {
		return buildSOCKS5Client(proxy)
	}
	return buildHTTPProxyClient(proxy)
}

// buildHTTPProxyClient creates a client for HTTP/HTTPS proxies.
func buildHTTPProxyClient(proxyCfg *ProxyConfig) (*http.Client, error) {
	tr := &http.Transport{
		MaxIdleConns:          64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Configure proxy
	if proxyCfg != nil && proxyCfg.URL != "" {
		proxyURL, err := proxyCfg.GetProxyURL()
		if err != nil {
			return nil, err
		}

		// Build no_proxy function
		noProxyList := proxyCfg.NoProxy
		if noProxyList == "" && !proxyCfg.NoEnvProxy {
			noProxyList = os.Getenv("NO_PROXY")
			if noProxyList == "" {
				noProxyList = os.Getenv("no_proxy")
			}
		}

		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			if shouldBypassProxy(req.URL.Host, noProxyList) {
				return nil, nil
			}
			return proxyURL, nil
		}

		// Handle TLS settings for HTTPS proxy
		if proxyCfg.InsecureSkipVerify {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
	} else if proxyCfg != nil && proxyCfg.NoEnvProxy {
		// Explicit no proxy
		tr.Proxy = nil
	} else {
		// Use environment proxy
		tr.Proxy = http.ProxyFromEnvironment
	}

	return &http.Client{Transport: tr}, nil
}

// buildSOCKS5Client creates a client for SOCKS5 proxies.
func buildSOCKS5Client(proxyCfg *ProxyConfig) (*http.Client, error) {
	proxyURL, err := proxyCfg.GetProxyURL()
	if err != nil {
		return nil, err
	}

	// Parse SOCKS5 address
	host := proxyURL.Host
	if proxyURL.Port() == "" {
		host = net.JoinHostPort(proxyURL.Hostname(), "1080") // Default SOCKS5 port
	}

	// Setup authentication
	var auth *proxy.Auth
	if proxyURL.User != nil {
		pass, _ := proxyURL.User.Password()
		auth = &proxy.Auth{
			User:     proxyURL.User.Username(),
			Password: pass,
		}
	}

	// Create SOCKS5 dialer
	dialer, err := proxy.SOCKS5("tcp", host, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	// Build no_proxy list
	noProxyList := proxyCfg.NoProxy
	if noProxyList == "" && !proxyCfg.NoEnvProxy {
		noProxyList = os.Getenv("NO_PROXY")
		if noProxyList == "" {
			noProxyList = os.Getenv("no_proxy")
		}
	}

	// Create transport with SOCKS5 dialer
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Check if we should bypass proxy
			host, _, _ := net.SplitHostPort(addr)
			if shouldBypassProxy(host, noProxyList) {
				return (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext(ctx, network, addr)
			}
			// Use SOCKS5 proxy
			return dialer.Dial(network, addr)
		},
		MaxIdleConns:          64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if proxyCfg.InsecureSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{Transport: tr}, nil
}

// shouldBypassProxy checks if a host should bypass the proxy.
func shouldBypassProxy(host, noProxy string) bool {
	if noProxy == "" {
		return false
	}

	host = strings.ToLower(strings.TrimSpace(host))
	// Remove port if present
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	for _, pattern := range strings.Split(noProxy, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}

		// Wildcard - bypass all
		if pattern == "*" {
			return true
		}

		// Exact match
		if pattern == host {
			return true
		}

		// Domain suffix match (pattern starts with .)
		if strings.HasPrefix(pattern, ".") && strings.HasSuffix(host, pattern) {
			return true
		}

		// Domain suffix match (pattern without leading .)
		if strings.HasSuffix(host, "."+pattern) {
			return true
		}

		// CIDR match
		if strings.Contains(pattern, "/") {
			_, cidr, err := net.ParseCIDR(pattern)
			if err == nil {
				if ip := net.ParseIP(host); ip != nil && cidr.Contains(ip) {
					return true
				}
			}
		}
	}

	return false
}

// ProxyTestResult holds the result of a proxy connectivity test.
type ProxyTestResult struct {
	Success    bool          `json:"success"`
	StatusCode int           `json:"status_code"`
	Status     string        `json:"status"`
	Duration   time.Duration `json:"duration"`
	RemoteAddr string        `json:"remote_addr,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// TestProxy tests connectivity through the configured proxy.
// Returns a test result and error if the test failed.
func TestProxy(ctx context.Context, proxyCfg *ProxyConfig, testURL string) (*ProxyTestResult, error) {
	if testURL == "" {
		testURL = "https://huggingface.co/api/whoami-v2"
	}

	client, err := BuildHTTPClient(proxyCfg)
	if err != nil {
		return &ProxyTestResult{
			Success: false,
			Error:   fmt.Sprintf("failed to build client: %v", err),
		}, fmt.Errorf("failed to build client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", testURL, nil)
	if err != nil {
		return &ProxyTestResult{
			Success: false,
			Error:   fmt.Sprintf("failed to create request: %v", err),
		}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "hfdownloader/2")

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return &ProxyTestResult{
			Success:  false,
			Duration: duration,
			Error:    fmt.Sprintf("proxy connection failed: %v", err),
		}, fmt.Errorf("proxy connection failed: %w", err)
	}
	defer resp.Body.Close()

	// Any response (even 401/403) means the proxy is working
	return &ProxyTestResult{
		Success:    true,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Duration:   duration,
	}, nil
}

// ProxyInfo contains information about proxy configuration.
type ProxyInfo struct {
	HTTPProxy      string `json:"http_proxy,omitempty"`
	HTTPSProxy     string `json:"https_proxy,omitempty"`
	NoProxy        string `json:"no_proxy,omitempty"`
	AllProxy       string `json:"all_proxy,omitempty"`
	EffectiveProxy string `json:"effective_proxy,omitempty"`
	Source         string `json:"source,omitempty"` // "config", "environment", "none"
}

// GetProxyInfo returns detailed proxy configuration information.
func GetProxyInfo() *ProxyInfo {
	info := &ProxyInfo{}

	// Collect environment variables
	info.HTTPProxy = os.Getenv("HTTP_PROXY")
	if info.HTTPProxy == "" {
		info.HTTPProxy = os.Getenv("http_proxy")
	}

	info.HTTPSProxy = os.Getenv("HTTPS_PROXY")
	if info.HTTPSProxy == "" {
		info.HTTPSProxy = os.Getenv("https_proxy")
	}

	info.NoProxy = os.Getenv("NO_PROXY")
	if info.NoProxy == "" {
		info.NoProxy = os.Getenv("no_proxy")
	}

	info.AllProxy = os.Getenv("ALL_PROXY")
	if info.AllProxy == "" {
		info.AllProxy = os.Getenv("all_proxy")
	}

	// Determine effective proxy
	if info.HTTPSProxy != "" {
		info.EffectiveProxy = info.HTTPSProxy
		info.Source = "environment"
	} else if info.HTTPProxy != "" {
		info.EffectiveProxy = info.HTTPProxy
		info.Source = "environment"
	} else if info.AllProxy != "" {
		info.EffectiveProxy = info.AllProxy
		info.Source = "environment"
	} else {
		info.Source = "none"
	}

	return info
}

// GetProxyInfoString returns a human-readable description of the proxy configuration.
func GetProxyInfoString(proxyCfg *ProxyConfig) string {
	if proxyCfg == nil || proxyCfg.URL == "" {
		// Check environment
		if httpProxy := os.Getenv("HTTP_PROXY"); httpProxy != "" {
			return fmt.Sprintf("Environment: %s", httpProxy)
		}
		if httpProxy := os.Getenv("http_proxy"); httpProxy != "" {
			return fmt.Sprintf("Environment: %s", httpProxy)
		}
		if httpsProxy := os.Getenv("HTTPS_PROXY"); httpsProxy != "" {
			return fmt.Sprintf("Environment: %s", httpsProxy)
		}
		if httpsProxy := os.Getenv("https_proxy"); httpsProxy != "" {
			return fmt.Sprintf("Environment: %s", httpsProxy)
		}
		return "None (direct connection)"
	}

	// Parse URL to hide password
	u, err := url.Parse(proxyCfg.URL)
	if err != nil {
		return proxyCfg.URL
	}

	// Apply auth if separate
	if u.User == nil && proxyCfg.Username != "" {
		if proxyCfg.Password != "" {
			u.User = url.UserPassword(proxyCfg.Username, "****")
		} else {
			u.User = url.User(proxyCfg.Username)
		}
	} else if u.User != nil {
		// Mask existing password
		if _, hasPass := u.User.Password(); hasPass {
			u.User = url.UserPassword(u.User.Username(), "****")
		}
	}

	return u.String()
}
