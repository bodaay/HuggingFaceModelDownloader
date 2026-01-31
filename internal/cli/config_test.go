// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		key      string
		expected any
	}{
		{"cache-dir", ""},
		{"connections", 8},
		{"max-active", 3},
		{"multipart-threshold", "32MiB"},
		{"verify", "size"},
		{"retries", 4},
		{"backoff-initial", "400ms"},
		{"backoff-max", "10s"},
		{"token", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val, ok := cfg[tt.key]
			if !ok {
				t.Errorf("DefaultConfig missing key %q", tt.key)
				return
			}
			if val != tt.expected {
				t.Errorf("DefaultConfig[%q] = %v, want %v", tt.key, val, tt.expected)
			}
		})
	}
}

func TestLoadConfigMap_NoConfigFile(t *testing.T) {
	// Save original HOME and restore after test
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Use temp dir with no config file
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	cfg := loadConfigMap()
	if cfg != nil {
		t.Errorf("loadConfigMap() = %v, want nil when no config file exists", cfg)
	}
}

func TestLoadConfigMap_JSONConfig(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create config directory
	configDir := filepath.Join(tmpDir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write JSON config
	configPath := filepath.Join(configDir, "hfdownloader.json")
	configData := map[string]any{
		"cache-dir":   "/custom/cache",
		"connections": 16,
		"token":       "hf_test_token",
	}
	data, _ := json.Marshal(configData)
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigMap()
	if cfg == nil {
		t.Fatal("loadConfigMap() returned nil, want config map")
	}

	if v, ok := cfg["cache-dir"].(string); !ok || v != "/custom/cache" {
		t.Errorf("cache-dir = %v, want /custom/cache", cfg["cache-dir"])
	}
	if v, ok := cfg["connections"].(float64); !ok || v != 16 {
		t.Errorf("connections = %v, want 16", cfg["connections"])
	}
	if v, ok := cfg["token"].(string); !ok || v != "hf_test_token" {
		t.Errorf("token = %v, want hf_test_token", cfg["token"])
	}
}

func TestLoadConfigMap_YAMLConfig(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write YAML config
	configPath := filepath.Join(configDir, "hfdownloader.yaml")
	configData := map[string]any{
		"cache-dir":   "/yaml/cache",
		"connections": 12,
		"verify":      "sha256",
	}
	data, _ := yaml.Marshal(configData)
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigMap()
	if cfg == nil {
		t.Fatal("loadConfigMap() returned nil, want config map")
	}

	if v, ok := cfg["cache-dir"].(string); !ok || v != "/yaml/cache" {
		t.Errorf("cache-dir = %v, want /yaml/cache", cfg["cache-dir"])
	}
	if v, ok := cfg["connections"].(int); !ok || v != 12 {
		t.Errorf("connections = %v, want 12", cfg["connections"])
	}
}

func TestLoadConfigMap_JSONPrecedence(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write both JSON and YAML - JSON should take precedence
	jsonPath := filepath.Join(configDir, "hfdownloader.json")
	yamlPath := filepath.Join(configDir, "hfdownloader.yaml")

	jsonData, _ := json.Marshal(map[string]any{"cache-dir": "/json/path"})
	yamlData, _ := yaml.Marshal(map[string]any{"cache-dir": "/yaml/path"})

	os.WriteFile(jsonPath, jsonData, 0o644)
	os.WriteFile(yamlPath, yamlData, 0o644)

	cfg := loadConfigMap()
	if cfg == nil {
		t.Fatal("loadConfigMap() returned nil")
	}

	if v := cfg["cache-dir"]; v != "/json/path" {
		t.Errorf("cache-dir = %v, want /json/path (JSON should take precedence)", v)
	}
}

func TestLoadConfigMap_InvalidJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config")
	os.MkdirAll(configDir, 0o755)

	// Write invalid JSON
	configPath := filepath.Join(configDir, "hfdownloader.json")
	os.WriteFile(configPath, []byte("{ invalid json }"), 0o644)

	cfg := loadConfigMap()
	if cfg != nil {
		t.Errorf("loadConfigMap() = %v, want nil for invalid JSON", cfg)
	}
}

func TestLoadConfigMap_InvalidYAML(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config")
	os.MkdirAll(configDir, 0o755)

	// Write invalid YAML
	configPath := filepath.Join(configDir, "hfdownloader.yaml")
	os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0o644)

	cfg := loadConfigMap()
	if cfg != nil {
		t.Errorf("loadConfigMap() = %v, want nil for invalid YAML", cfg)
	}
}
