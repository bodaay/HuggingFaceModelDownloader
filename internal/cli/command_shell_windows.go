// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

func detectCommandShell() commandShell {
	// PSModulePath can be inherited by cmd.exe, so inspect the actual parent
	// instead. Unknown callers use Windows command-prompt quoting.
	parent, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(os.Getppid()))
	if err != nil {
		return cmdShell
	}
	defer windows.CloseHandle(parent)
	var name [32768]uint16
	size := uint32(len(name))
	if err := windows.QueryFullProcessImageName(parent, 0, &name[0], &size); err != nil {
		return cmdShell
	}
	return shellForParent(windows.UTF16ToString(name[:size]), cmdShell)
}
