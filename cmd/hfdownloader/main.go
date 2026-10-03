// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/cli"
)

// Version is set at build time via ldflags
// Keep in sync with VERSION file
var Version = "3.3.0"

func main() {
	if err := cli.Execute(Version); err != nil {
		os.Exit(1)
	}
}

