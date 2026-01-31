// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"runtime"
	"testing"
)

func TestGetBuildInfo(t *testing.T) {
	version := "v3.0.1-test"
	info := GetBuildInfo(version)

	t.Run("version", func(t *testing.T) {
		if info.Version != version {
			t.Errorf("Version = %q, want %q", info.Version, version)
		}
	})

	t.Run("go version", func(t *testing.T) {
		if info.GoVersion != runtime.Version() {
			t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
		}
	})

	t.Run("os", func(t *testing.T) {
		if info.OS != runtime.GOOS {
			t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
		}
	})

	t.Run("arch", func(t *testing.T) {
		if info.Arch != runtime.GOARCH {
			t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
		}
	})

	t.Run("commit has value", func(t *testing.T) {
		// During tests, commit might be "unknown" or an actual value
		if info.Commit == "" {
			t.Error("Commit should not be empty")
		}
	})

	t.Run("build time has value", func(t *testing.T) {
		// During tests, build time might be "unknown" or an actual value
		if info.BuildTime == "" {
			t.Error("BuildTime should not be empty")
		}
	})
}

func TestBuildInfo_Fields(t *testing.T) {
	info := BuildInfo{
		Version:   "v1.0.0",
		GoVersion: "go1.22.0",
		OS:        "linux",
		Arch:      "amd64",
		Commit:    "abc1234",
		BuildTime: "2025-01-01T00:00:00Z",
	}

	if info.Version != "v1.0.0" {
		t.Errorf("Version = %q", info.Version)
	}
	if info.GoVersion != "go1.22.0" {
		t.Errorf("GoVersion = %q", info.GoVersion)
	}
	if info.OS != "linux" {
		t.Errorf("OS = %q", info.OS)
	}
	if info.Arch != "amd64" {
		t.Errorf("Arch = %q", info.Arch)
	}
	if info.Commit != "abc1234" {
		t.Errorf("Commit = %q", info.Commit)
	}
	if info.BuildTime != "2025-01-01T00:00:00Z" {
		t.Errorf("BuildTime = %q", info.BuildTime)
	}
}

func TestNewVersionCmd(t *testing.T) {
	t.Run("command creation", func(t *testing.T) {
		cmd := newVersionCmd("v1.0.0")
		if cmd == nil {
			t.Fatal("cmd should not be nil")
		}
		if cmd.Use != "version" {
			t.Errorf("Use = %q, want version", cmd.Use)
		}
	})

	t.Run("short flag exists", func(t *testing.T) {
		cmd := newVersionCmd("v1.0.0")
		shortFlag := cmd.Flags().Lookup("short")
		if shortFlag == nil {
			t.Error("short flag should exist")
		}
	})
}
