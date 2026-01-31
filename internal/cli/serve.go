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
)

func newServeCmd(ro *RootOpts) *cobra.Command {
	var (
		addr               string
		port               int
		modelsDir          string
		datasetsDir        string
		cacheDir           string
		conns              int
		active             int
		multipartThreshold string
		verify             string
		retries            int
		endpoint           string
		authUser           string
		authPass           string
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
  hfdownloader serve --endpoint https://hf-mirror.com      # Use mirror`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build server config from CLI flags
			cfg := server.Config{
				Addr:        addr,
				Port:        port,
				ModelsDir:   modelsDir,
				DatasetsDir: datasetsDir,
				CacheDir:    cacheDir,
				AuthUser:    authUser,
				AuthPass:    authPass,
			}

			// Apply config file settings first (for values not set by CLI)
			if err := server.ApplyConfigToServer(&cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load config file: %v\n", err)
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
	cmd.Flags().IntVarP(&conns, "connections", "c", 8, "Connections per file")
	cmd.Flags().IntVar(&active, "max-active", 3, "Max concurrent file downloads")
	cmd.Flags().StringVar(&multipartThreshold, "multipart-threshold", "32MiB", "Use multipart for files >= this size")
	cmd.Flags().StringVar(&verify, "verify", "size", "Verification mode: none|size|sha256")
	cmd.Flags().IntVar(&retries, "retries", 4, "Max retry attempts per HTTP request")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Custom HuggingFace endpoint URL (e.g., https://hf-mirror.com)")

	// Authentication
	cmd.Flags().StringVar(&authUser, "auth-user", "", "Username for basic auth (enables auth when set)")
	cmd.Flags().StringVar(&authPass, "auth-pass", "", "Password for basic auth")

	return cmd
}
