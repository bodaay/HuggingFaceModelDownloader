// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Target represents a mirror destination.
type Target struct {
	Path        string `yaml:"path" json:"path"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// TargetsConfig holds all configured mirror targets.
type TargetsConfig struct {
	Targets map[string]Target `yaml:"targets" json:"targets"`
}

// DefaultTargetsPath returns the default path for targets config.
func DefaultTargetsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hfdownloader", "targets.yaml")
}

// LoadTargets loads targets from the config file.
func LoadTargets(path string) (*TargetsConfig, error) {
	if path == "" {
		path = DefaultTargetsPath()
	}

	cfg := &TargetsConfig{
		Targets: make(map[string]Target),
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil // Empty config is fine
	}
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse targets: %w", err)
	}

	if cfg.Targets == nil {
		cfg.Targets = make(map[string]Target)
	}

	return cfg, nil
}

// Save writes the targets config to disk.
func (c *TargetsConfig) Save(path string) error {
	if path == "" {
		path = DefaultTargetsPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal targets: %w", err)
	}

	header := "# HFDownloader Mirror Targets\n"
	header += "# Configure with: hfdownloader mirror target add <name> <path>\n\n"
	content := header + string(data)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write targets: %w", err)
	}

	return nil
}

// Add adds or updates a target.
func (c *TargetsConfig) Add(name, path, description string) {
	c.Targets[name] = Target{
		Path:        path,
		Description: description,
	}
}

// Remove removes a target.
func (c *TargetsConfig) Remove(name string) bool {
	if _, ok := c.Targets[name]; ok {
		delete(c.Targets, name)
		return true
	}
	return false
}

// Get returns a target by name.
func (c *TargetsConfig) Get(name string) (Target, bool) {
	t, ok := c.Targets[name]
	return t, ok
}

// ResolvePath returns the cache path for a target name or path.
// If the input matches a target name, returns the target's path.
// Otherwise assumes it's a direct path.
func (c *TargetsConfig) ResolvePath(nameOrPath string) string {
	if t, ok := c.Targets[nameOrPath]; ok {
		return t.Path
	}
	return nameOrPath
}
