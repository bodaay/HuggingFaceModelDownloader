// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// BuildInfo holds version and build information.
type BuildInfo struct {
	Version   string
	GoVersion string
	OS        string
	Arch      string
	Commit    string
	BuildTime string
}

// GetBuildInfo returns the current build information.
func GetBuildInfo(version string) BuildInfo {
	info := BuildInfo{
		Version:   version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Commit:    "unknown",
		BuildTime: "unknown",
	}

	// Try to get VCS info from debug.BuildInfo
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				if len(setting.Value) >= 7 {
					info.Commit = setting.Value[:7]
				} else {
					info.Commit = setting.Value
				}
			case "vcs.time":
				info.BuildTime = setting.Value
			}
		}
	}

	return info
}

func newVersionCmd(version string) *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version and build information",
		Run: func(cmd *cobra.Command, args []string) {
			info := GetBuildInfo(version)

			if short {
				fmt.Println(info.Version)
				return
			}

			fmt.Printf("hfdownloader %s\n", info.Version)
			fmt.Printf("  Go:       %s\n", info.GoVersion)
			fmt.Printf("  OS/Arch:  %s/%s\n", info.OS, info.Arch)
			fmt.Printf("  Commit:   %s\n", info.Commit)
			fmt.Printf("  Built:    %s\n", info.BuildTime)
		},
	}

	cmd.Flags().BoolVarP(&short, "short", "s", false, "Print only the version number")

	return cmd
}

