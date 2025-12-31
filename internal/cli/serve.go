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
		addr        string
		port        int
		modelsDir   string
		datasetsDir string
		conns       int
		active      int
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server for web-based downloads",
		Long: `Start an HTTP server that provides:
  - REST API for download management
  - WebSocket for live progress updates  
  - Web UI for browser-based downloads

Output paths are configured server-side only (not via API) for security.

Example:
  hfdownloader serve
  hfdownloader serve --port 3000
  hfdownloader serve --models-dir ./Models --datasets-dir ./Datasets`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build server config
			cfg := server.Config{
				Addr:        addr,
				Port:        port,
				ModelsDir:   modelsDir,
				DatasetsDir: datasetsDir,
				Concurrency: conns,
				MaxActive:   active,
			}

			// Get token from flag or env
			token := strings.TrimSpace(ro.Token)
			if token == "" {
				token = strings.TrimSpace(os.Getenv("HF_TOKEN"))
			}
			cfg.Token = token

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
	cmd.Flags().StringVar(&modelsDir, "models-dir", "./Models", "Output directory for models")
	cmd.Flags().StringVar(&datasetsDir, "datasets-dir", "./Datasets", "Output directory for datasets")
	cmd.Flags().IntVarP(&conns, "connections", "c", 8, "Connections per file")
	cmd.Flags().IntVar(&active, "max-active", 3, "Max concurrent file downloads")

	return cmd
}
