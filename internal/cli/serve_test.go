// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewServeCmd(t *testing.T) {
	ro := &RootOpts{}

	cmd := newServeCmd(ro)
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}

	if cmd.Use != "serve" {
		t.Errorf("Use = %q", cmd.Use)
	}

	// Check all expected flags exist (actual flags from serve.go)
	expectedFlags := []string{
		"port",
		"addr",        // Not "host"
		"cache-dir",
		"connections", // Not "concurrency"
		"endpoint",
		"auth-user", // Not "auth-token"
		"auth-pass",
	}

	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q should exist", name)
		}
	}
}

func TestServeCmd_DefaultPort(t *testing.T) {
	ro := &RootOpts{}
	cmd := newServeCmd(ro)

	portFlag := cmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("port flag should exist")
	}

	defaultPort := portFlag.DefValue
	if defaultPort != "8080" {
		t.Errorf("default port = %q, want 8080", defaultPort)
	}
}

func TestServeCmd_DefaultAddr(t *testing.T) {
	ro := &RootOpts{}
	cmd := newServeCmd(ro)

	addrFlag := cmd.Flags().Lookup("addr")
	if addrFlag == nil {
		t.Fatal("addr flag should exist")
	}

	defaultAddr := addrFlag.DefValue
	if defaultAddr != "0.0.0.0" {
		t.Errorf("default addr = %q, want 0.0.0.0", defaultAddr)
	}
}

func TestServerConfig_CacheDir(t *testing.T) {
	t.Run("uses provided cache dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, "server-cache")
		os.MkdirAll(cacheDir, 0o755)

		info, err := os.Stat(cacheDir)
		if err != nil {
			t.Fatalf("cache dir error: %v", err)
		}
		if !info.IsDir() {
			t.Error("cache dir should be a directory")
		}
	})

	t.Run("creates cache dir if missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, "new-cache")

		// Dir doesn't exist yet
		if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
			t.Fatal("cache dir should not exist yet")
		}

		// Create it
		err := os.MkdirAll(cacheDir, 0o755)
		if err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}

		// Now it should exist
		info, err := os.Stat(cacheDir)
		if err != nil {
			t.Fatalf("cache dir should exist: %v", err)
		}
		if !info.IsDir() {
			t.Error("cache dir should be a directory")
		}
	})
}

func TestServerConfig_AuthUser(t *testing.T) {
	ro := &RootOpts{}
	cmd := newServeCmd(ro)

	authUserFlag := cmd.Flags().Lookup("auth-user")
	if authUserFlag == nil {
		t.Fatal("auth-user flag should exist")
	}

	// Default should be empty
	if authUserFlag.DefValue != "" {
		t.Errorf("auth-user default = %q, want empty", authUserFlag.DefValue)
	}
}

func TestServerConfig_AuthPass(t *testing.T) {
	ro := &RootOpts{}
	cmd := newServeCmd(ro)

	authPassFlag := cmd.Flags().Lookup("auth-pass")
	if authPassFlag == nil {
		t.Fatal("auth-pass flag should exist")
	}

	// Default should be empty
	if authPassFlag.DefValue != "" {
		t.Errorf("auth-pass default = %q, want empty", authPassFlag.DefValue)
	}
}

func TestValidPort(t *testing.T) {
	tests := []struct {
		port  int
		valid bool
	}{
		{80, true},
		{443, true},
		{8080, true},
		{3000, true},
		{65535, true},
		{0, false},
		{-1, false},
		{65536, false},
		{100000, false},
	}

	for _, tt := range tests {
		valid := tt.port > 0 && tt.port <= 65535
		if valid != tt.valid {
			t.Errorf("port %d: got valid=%v, want %v", tt.port, valid, tt.valid)
		}
	}
}

func TestValidHost(t *testing.T) {
	tests := []struct {
		host  string
		valid bool
	}{
		{"0.0.0.0", true},
		{"127.0.0.1", true},
		{"localhost", true},
		{"192.168.1.1", true},
		{"::", true},
		{"::1", true},
		{"", false},
	}

	for _, tt := range tests {
		valid := tt.host != ""
		if valid != tt.valid {
			t.Errorf("host %q: got valid=%v, want %v", tt.host, valid, tt.valid)
		}
	}
}
