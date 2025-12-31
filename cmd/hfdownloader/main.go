// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/cli"
)

// Version is set at build time via ldflags
var Version = "2.3.0-dev"

func main() {
	if err := cli.Execute(Version); err != nil {
		os.Exit(1)
	}
}

