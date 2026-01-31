// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
	"gopkg.in/yaml.v3"
)

// ConfigFile represents the persistent configuration file format.
// This matches the CLI config file format for consistency.
type ConfigFile struct {
	CacheDir           string       `json:"cache-dir,omitempty" yaml:"cache-dir,omitempty"`
	Token              string       `json:"token,omitempty" yaml:"token,omitempty"`
	Connections        int          `json:"connections,omitempty" yaml:"connections,omitempty"`
	MaxActive          int          `json:"max-active,omitempty" yaml:"max-active,omitempty"`
	MultipartThreshold string       `json:"multipart-threshold,omitempty" yaml:"multipart-threshold,omitempty"`
	Verify             string       `json:"verify,omitempty" yaml:"verify,omitempty"`
	Retries            int          `json:"retries,omitempty" yaml:"retries,omitempty"`
	Endpoint           string       `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	BackoffInitial     string       `json:"backoff-initial,omitempty" yaml:"backoff-initial,omitempty"`
	BackoffMax         string       `json:"backoff-max,omitempty" yaml:"backoff-max,omitempty"`
	Proxy              *ProxyConfig `json:"proxy,omitempty" yaml:"proxy,omitempty"`
}

// ProxyConfig holds proxy settings for the config file.
type ProxyConfig struct {
	URL                string `json:"url,omitempty" yaml:"url,omitempty"`
	Username           string `json:"username,omitempty" yaml:"username,omitempty"`
	Password           string `json:"password,omitempty" yaml:"password,omitempty"`
	NoProxy            string `json:"no_proxy,omitempty" yaml:"no_proxy,omitempty"`
	NoEnvProxy         bool   `json:"no_env_proxy,omitempty" yaml:"no_env_proxy,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty" yaml:"insecure_skip_verify,omitempty"`
}

var configMu sync.Mutex

// ConfigPath returns the path to the config file.
// Checks in order: hfdownloader.json, hfdownloader.yaml, hfdownloader.yml
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	configDir := filepath.Join(home, ".config")

	// Check for existing files in order of preference
	jsonPath := filepath.Join(configDir, "hfdownloader.json")
	yamlPath := filepath.Join(configDir, "hfdownloader.yaml")
	ymlPath := filepath.Join(configDir, "hfdownloader.yml")

	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath
	}
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath
	}
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath
	}

	// Default to JSON if no file exists
	return jsonPath
}

// LoadConfigFile loads configuration from the config file.
// Returns empty config if file doesn't exist (not an error).
func LoadConfigFile() (*ConfigFile, error) {
	path := ConfigPath()
	if path == "" {
		return &ConfigFile{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfigFile{}, nil
		}
		return nil, err
	}

	cfg := &ConfigFile{}
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	default:
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// SaveConfigFile saves configuration to the config file.
func SaveConfigFile(cfg *ConfigFile) error {
	configMu.Lock()
	defer configMu.Unlock()

	path := ConfigPath()
	if path == "" {
		return nil
	}

	// Ensure config directory exists
	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(path))
	var data []byte
	var err error

	switch ext {
	case ".yaml", ".yml":
		data, err = yaml.Marshal(cfg)
	default:
		data, err = json.MarshalIndent(cfg, "", "  ")
	}

	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

// ApplyConfigToServer applies config file settings to server config.
// CLI flags take precedence (non-zero values are not overwritten).
func ApplyConfigToServer(serverCfg *Config) error {
	fileCfg, err := LoadConfigFile()
	if err != nil {
		return err
	}

	// Only apply values that are not already set via CLI
	if serverCfg.CacheDir == "" && fileCfg.CacheDir != "" {
		serverCfg.CacheDir = fileCfg.CacheDir
	}
	if serverCfg.Token == "" && fileCfg.Token != "" {
		serverCfg.Token = fileCfg.Token
	}
	if serverCfg.Concurrency == 0 && fileCfg.Connections > 0 {
		serverCfg.Concurrency = fileCfg.Connections
	}
	if serverCfg.MaxActive == 0 && fileCfg.MaxActive > 0 {
		serverCfg.MaxActive = fileCfg.MaxActive
	}
	if serverCfg.MultipartThreshold == "" && fileCfg.MultipartThreshold != "" {
		serverCfg.MultipartThreshold = fileCfg.MultipartThreshold
	}
	if serverCfg.Verify == "" && fileCfg.Verify != "" {
		serverCfg.Verify = fileCfg.Verify
	}
	if serverCfg.Retries == 0 && fileCfg.Retries > 0 {
		serverCfg.Retries = fileCfg.Retries
	}
	if serverCfg.Endpoint == "" && fileCfg.Endpoint != "" {
		serverCfg.Endpoint = fileCfg.Endpoint
	}

	// Apply proxy settings if not already set
	if serverCfg.Proxy == nil && fileCfg.Proxy != nil {
		serverCfg.Proxy = &hfdownloader.ProxyConfig{
			URL:                fileCfg.Proxy.URL,
			Username:           fileCfg.Proxy.Username,
			Password:           fileCfg.Proxy.Password,
			NoProxy:            fileCfg.Proxy.NoProxy,
			NoEnvProxy:         fileCfg.Proxy.NoEnvProxy,
			InsecureSkipVerify: fileCfg.Proxy.InsecureSkipVerify,
		}
	}

	return nil
}
