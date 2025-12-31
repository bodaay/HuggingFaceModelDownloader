// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

// Package assets provides embedded static files for the web UI.
package assets

import (
	"embed"
	"io/fs"
)

// staticFiles contains the embedded static files for the web interface.
//
//go:embed static/*
var staticFiles embed.FS

// StaticFS returns the filesystem for serving static files.
// Use with http.FileServer(http.FS(assets.StaticFS()))
func StaticFS() fs.FS {
	sub, _ := fs.Sub(staticFiles, "static")
	return sub
}

