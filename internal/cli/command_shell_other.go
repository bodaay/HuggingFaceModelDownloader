// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package cli

import (
	"os"
	"strconv"
)

func detectCommandShell() commandShell {
	// PowerShell also runs on Linux. If procfs is unavailable, retain the
	// usual POSIX convention rather than guessing from inherited env vars.
	parent, _ := os.Readlink("/proc/" + strconv.Itoa(os.Getppid()) + "/exe")
	return shellForParent(parent, posixShell)
}
