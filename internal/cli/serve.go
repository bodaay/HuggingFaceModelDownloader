// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/server"
	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func newServeCmd(ro *RootOpts) *cobra.Command {
	var (
		addr               string
		port               int
		modelsDir          string
		datasetsDir        string
		cacheDir           string
		localDir           string
		conns              int
		active             int
		multipartThreshold string
		verify             string
		retries            int
		endpoint           string
		authUser           string
		authPass           string
		allowOrigins       []string
		linkMode           string
		exportDir          string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server for web-based downloads",
		Long: `Start an HTTP server that provides:
  - REST API for download management
  - WebSocket for live progress updates
  - Web UI for browser-based downloads
  - Repository analysis (smart downloader)
  - Cache browser for downloaded models

Output paths are configured server-side only (not via API) for security.

Examples:
  hfdownloader serve                              # Start on port 8080
  hfdownloader serve --port 3000                  # Custom port
  hfdownloader serve --auth-user admin --auth-pass secret  # With authentication
  hfdownloader serve --endpoint https://hf-mirror.com      # Use mirror
  hfdownloader serve --allow-origin https://hfd.example.com # Behind a reverse proxy

Browser requests from other origins (other websites) are rejected; the web UI
itself is always allowed. Use --allow-origin when a reverse proxy or another
web app on a different origin needs to call the API.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build server config from CLI flags
			cfg := server.Config{
				Addr:           addr,
				Port:           port,
				ModelsDir:      modelsDir,
				DatasetsDir:    datasetsDir,
				CacheDir:       cacheDir,
				LocalDir:       localDir,
				AuthUser:       authUser,
				AuthPass:       authPass,
				AllowedOrigins: allowOrigins,
				LinkMode:       linkMode,
				ExportDir:      exportDir,
				Version:        cmd.Root().Version, // injected build version (ldflags)
			}

			// Apply config file settings first (for values not set by CLI)
			if err := server.ApplyConfigToServer(&cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load config file: %v\n", err)
			}
			if _, err := hfdownloader.ParseLinkMode(cfg.LinkMode); err != nil {
				return err
			}
			// --auth-user alone used to enable auth with an empty password, and
			// --auth-pass alone silently left auth off.
			if (authUser == "") != (authPass == "") {
				return fmt.Errorf("--auth-user and --auth-pass must be used together")
			}

			// Then override with CLI flags if explicitly set
			if cmd.Flags().Changed("connections") {
				cfg.Concurrency = conns
			} else if cfg.Concurrency == 0 {
				cfg.Concurrency = 8 // Default
			}
			if cmd.Flags().Changed("max-active") {
				cfg.MaxActive = active
			} else if cfg.MaxActive == 0 {
				cfg.MaxActive = 3 // Default
			}
			if cmd.Flags().Changed("multipart-threshold") {
				cfg.MultipartThreshold = multipartThreshold
			} else if cfg.MultipartThreshold == "" {
				cfg.MultipartThreshold = "32MiB" // Default
			}
			if cmd.Flags().Changed("verify") {
				cfg.Verify = verify
			} else if cfg.Verify == "" {
				cfg.Verify = "size" // Default
			}
			if cmd.Flags().Changed("retries") {
				cfg.Retries = retries
			} else if cfg.Retries == 0 {
				cfg.Retries = 4 // Default
			}
			if cmd.Flags().Changed("endpoint") {
				cfg.Endpoint = endpoint
			}

			// Get token from flag, env, or config (in that order)
			token := strings.TrimSpace(ro.Token)
			if token == "" {
				token = strings.TrimSpace(os.Getenv("HF_TOKEN"))
			}
			if token != "" {
				cfg.Token = token
			}

			// Create and start server
			srv := server.New(cfg)

			// Handle shutdown signals
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			fmt.Println()
			fmt.Println("╭────────────────────────────────────────────────────────────╮")
			fmt.Println("│               🤗  HuggingFace Downloader                   │")
			fmt.Println("│                    Web Server Mode                         │")
			fmt.Println("╰────────────────────────────────────────────────────────────╯")
			fmt.Println()

			return srv.ListenAndServe(ctx)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "0.0.0.0", "Address to bind to")
	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")
	cmd.Flags().StringVar(&modelsDir, "models-dir", "./Models", "Output directory for models (legacy mode)")
	cmd.Flags().StringVar(&datasetsDir, "datasets-dir", "./Datasets", "Output directory for datasets (legacy mode)")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface)")
	cmd.Flags().StringVar(&localDir, "local-dir", "", "Save real files (not HF cache symlinks) into this directory; puts the whole server in flat/local-file mode for all downloads")
	cmd.Flags().IntVarP(&conns, "connections", "c", 8, "Connections per file")
	cmd.Flags().IntVar(&active, "max-active", 3, "Max concurrent file downloads")
	cmd.Flags().StringVar(&multipartThreshold, "multipart-threshold", "32MiB", "Use multipart for files >= this size")
	cmd.Flags().StringVar(&verify, "verify", "size", "Verification for files without a known SHA256: none|size|etag|sha256")
	cmd.Flags().IntVar(&retries, "retries", 4, "Max retry attempts per HTTP request")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Custom HuggingFace endpoint URL (e.g., https://hf-mirror.com)")

	// Authentication
	cmd.Flags().StringVar(&authUser, "auth-user", "", "Username for basic auth (enables auth when set)")
	cmd.Flags().StringVar(&authPass, "auth-pass", "", "Password for basic auth")

	// Storage
	cmd.Flags().StringVar(&linkMode, "link-mode", "", "How cache entries refer to downloaded data: auto (symlink, else hardlink, else copy), symlink, hardlink, copy")
	cmd.Flags().StringVar(&exportDir, "export-dir", "", "Enable \"Export as real files\" in the web UI, writing to <export-dir>/<owner>/<name>")

	// Cross-origin access
	cmd.Flags().StringSliceVar(&allowOrigins, "allow-origin", nil, "Extra browser origin allowed to call the API, e.g. https://hfd.example.com (repeatable; \"*\" allows any)")

	return cmd
}
