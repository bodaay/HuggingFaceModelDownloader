// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points HOME (and its per-OS equivalents) at a throwaway directory
// so no test can read or overwrite the developer's real config or HF cache —
// settings tests used to persist ~/.config/hfdownloader.json.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "hfd-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "HF_HOME", "HF_HUB_CACHE", "HF_TOKEN"} {
		os.Unsetenv(k)
	}
	testCacheDir = filepath.Join(home, "cache")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
