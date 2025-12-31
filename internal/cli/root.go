// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/tui"
	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

// RootOpts holds global CLI options.
type RootOpts struct {
	Token    string
	JSONOut  bool
	Quiet    bool
	Verbose  bool
	Config   string
	LogFile  string
	LogLevel string
}

// Execute runs the CLI with the given version string.
func Execute(version string) error {
	ro := &RootOpts{}
	ctx, cancel := signalContext(context.Background())
	defer cancel()

	root := &cobra.Command{
		Use:           "hfdownloader",
		Short:         "Fast, resumable downloader for Hugging Face models & datasets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	// Global flags
	root.PersistentFlags().StringVarP(&ro.Token, "token", "t", "", "Hugging Face access token (also reads HF_TOKEN env)")
	root.PersistentFlags().BoolVar(&ro.JSONOut, "json", false, "Emit machine-readable JSON events (progress, plan, results)")
	root.PersistentFlags().BoolVarP(&ro.Quiet, "quiet", "q", false, "Quiet mode (minimal logs)")
	root.PersistentFlags().BoolVarP(&ro.Verbose, "verbose", "v", false, "Verbose logs (debug details)")
	root.PersistentFlags().StringVar(&ro.Config, "config", "", "Path to config file (JSON or YAML)")
	root.PersistentFlags().StringVar(&ro.LogFile, "log-file", "", "Write logs to file (in addition to stderr)")
	root.PersistentFlags().StringVar(&ro.LogLevel, "log-level", "info", "Log level: debug, info, warn, error")

	// Add commands
	downloadCmd := newDownloadCmd(ctx, ro)
	root.AddCommand(downloadCmd)
	root.AddCommand(newVersionCmd(version))
	root.AddCommand(newServeCmd(ro))
	root.AddCommand(newConfigCmd())

	// Make download the default command when no subcommand is given
	root.RunE = downloadCmd.RunE
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true})

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return err
	}
	return nil
}

func newDownloadCmd(ctx context.Context, ro *RootOpts) *cobra.Command {
	job := &hfdownloader.Job{}
	cfg := &hfdownloader.Settings{}
	var dryRun bool
	var planFmt string

	cmd := &cobra.Command{
		Use:   "download [REPO]",
		Short: "Download a model or dataset from the Hugging Face Hub",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return applySettingsDefaults(cmd, ro, cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			finalJob, finalCfg, err := finalize(cmd, ro, args, job, cfg)
			if err != nil {
				return err
			}

			// Plan-only mode
			if dryRun {
				p, err := hfdownloader.PlanRepo(ctx, finalJob, finalCfg)
				if err != nil {
					return err
				}
				if strings.ToLower(planFmt) == "json" || ro.JSONOut {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				}
				rev := finalJob.Revision
				if rev == "" {
					rev = "main"
				}
				fmt.Printf("Plan for %s@%s (%d files):\n", finalJob.Repo, rev, len(p.Items))
				for _, it := range p.Items {
					fmt.Printf("  %s  %8d  lfs=%t\n", it.RelativePath, it.Size, it.LFS)
				}
				return nil
			}

			// Progress mode selection
			var progress hfdownloader.ProgressFunc
			if ro.JSONOut {
				progress = jsonProgress(os.Stdout)
			} else if ro.Quiet {
				progress = cliProgress(ro, finalJob)
			} else {
				// Live TUI
				ui := tui.NewLiveRenderer(finalJob, finalCfg)
				defer ui.Close()
				progress = ui.Handler()
			}

			return hfdownloader.Download(ctx, finalJob, finalCfg, progress)
		},
	}

	// Job flags
	cmd.Flags().StringVarP(&job.Repo, "repo", "r", "", "Repository ID (owner/name). If omitted, positional REPO is used")
	cmd.Flags().BoolVar(&job.IsDataset, "dataset", false, "Treat repo as a dataset")
	cmd.Flags().StringVarP(&job.Revision, "revision", "b", "main", "Revision/branch to download (e.g. main, refs/pr/1)")
	cmd.Flags().StringSliceVarP(&job.Filters, "filters", "F", nil, "Comma-separated filters to match LFS artifacts (e.g. q4_0,q5_0)")
	cmd.Flags().BoolVar(&job.AppendFilterSubdir, "append-filter-subdir", false, "Append each filter as a subdirectory")

	// Settings flags
	cmd.Flags().StringVarP(&cfg.OutputDir, "output", "o", "Storage", "Destination base directory")
	cmd.Flags().IntVarP(&cfg.Concurrency, "connections", "c", 8, "Per-file concurrent connections for LFS range requests")
	cmd.Flags().IntVar(&cfg.MaxActiveDownloads, "max-active", 3, "Maximum number of files downloading at once")
	cmd.Flags().StringVar(&cfg.MultipartThreshold, "multipart-threshold", "32MiB", "Use multipart/range downloads only for files >= this size")
	cmd.Flags().StringVar(&cfg.Verify, "verify", "size", "Verification for non-LFS files: none|size|etag|sha256")
	cmd.Flags().IntVar(&cfg.Retries, "retries", 4, "Max retry attempts per HTTP request/part")
	cmd.Flags().StringVar(&cfg.BackoffInitial, "backoff-initial", "400ms", "Initial retry backoff duration")
	cmd.Flags().StringVar(&cfg.BackoffMax, "backoff-max", "10s", "Maximum retry backoff duration")

	// CLI-only flags
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan only: print the file list and exit")
	cmd.Flags().StringVar(&planFmt, "plan-format", "table", "Plan output format for --dry-run: table|json")

	return cmd
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func finalize(cmd *cobra.Command, ro *RootOpts, args []string, job *hfdownloader.Job, cfg *hfdownloader.Settings) (hfdownloader.Job, hfdownloader.Settings, error) {
	j := *job
	c := *cfg

	// Token
	tok := strings.TrimSpace(ro.Token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("HF_TOKEN"))
	}
	c.Token = tok

	// Repo from args
	if j.Repo == "" && len(args) > 0 {
		j.Repo = args[0]
	}

	// Parse filters from repo:filter syntax
	if strings.Contains(j.Repo, ":") && len(j.Filters) == 0 {
		parts := strings.SplitN(j.Repo, ":", 2)
		j.Repo = parts[0]
		if strings.TrimSpace(parts[1]) != "" {
			j.Filters = splitComma(parts[1])
		}
	}

	if j.Repo == "" {
		return j, c, fmt.Errorf("missing REPO (owner/name). Pass as positional arg or --repo")
	}
	if !hfdownloader.IsValidModelName(j.Repo) {
		return j, c, fmt.Errorf("invalid repo id %q (expected owner/name)", j.Repo)
	}

	return j, c, nil
}

func applySettingsDefaults(cmd *cobra.Command, ro *RootOpts, dst *hfdownloader.Settings) error {
	path := ro.Config
	if path == "" {
		home, _ := os.UserHomeDir()
		// Try JSON first, then YAML
		jsonPath := filepath.Join(home, ".config", "hfdownloader.json")
		yamlPath := filepath.Join(home, ".config", "hfdownloader.yaml")
		ymlPath := filepath.Join(home, ".config", "hfdownloader.yml")

		if _, err := os.Stat(jsonPath); err == nil {
			path = jsonPath
		} else if _, err := os.Stat(yamlPath); err == nil {
			path = yamlPath
		} else if _, err := os.Stat(ymlPath); err == nil {
			path = ymlPath
		}
	}
	if path == "" {
		return nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg map[string]any

	// Parse based on file extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("invalid YAML config file: %w", err)
		}
	default: // .json or unknown
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("invalid JSON config file: %w", err)
		}
	}

	setStr := func(flagName string, set func(string)) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		if v, ok := cfg[flagName]; ok && v != nil {
			set(fmt.Sprint(v))
		}
	}
	setInt := func(flagName string, set func(int)) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		if v, ok := cfg[flagName]; ok && v != nil {
			var x int
			fmt.Sscan(fmt.Sprint(v), &x)
			set(x)
		}
	}

	setStr("output", func(v string) { dst.OutputDir = v })
	setInt("connections", func(v int) { dst.Concurrency = v })
	setInt("max-active", func(v int) { dst.MaxActiveDownloads = v })
	setStr("multipart-threshold", func(v string) { dst.MultipartThreshold = v })
	setStr("verify", func(v string) { dst.Verify = v })
	setInt("retries", func(v int) { dst.Retries = v })
	setStr("backoff-initial", func(v string) { dst.BackoffInitial = v })
	setStr("backoff-max", func(v string) { dst.BackoffMax = v })

	if !cmd.Flags().Changed("token") && os.Getenv("HF_TOKEN") == "" {
		if v, ok := cfg["token"]; ok && v != nil {
			ro.Token = fmt.Sprint(v)
		}
	}

	return nil
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cliProgress returns a simple text-based progress handler.
func cliProgress(ro *RootOpts, job hfdownloader.Job) hfdownloader.ProgressFunc {
	return func(ev hfdownloader.ProgressEvent) {
		rev := job.Revision
		if rev == "" {
			rev = "main"
		}
		switch ev.Event {
		case "scan_start":
			fmt.Printf("Scanning %s@%s ...\n", job.Repo, rev)
		case "retry":
			fmt.Printf("retry %s (attempt %d): %s\n", ev.Path, ev.Attempt, ev.Message)
		case "file_start":
			fmt.Printf("downloading: %s (%d bytes)\n", ev.Path, ev.Total)
		case "file_done":
			if strings.HasPrefix(ev.Message, "skip") {
				fmt.Printf("skip: %s %s\n", ev.Path, ev.Message)
			} else {
				fmt.Printf("done: %s\n", ev.Path)
			}
		case "error":
			fmt.Fprintf(os.Stderr, "error: %s\n", ev.Message)
		case "done":
			fmt.Println(ev.Message)
		}
	}
}

// jsonProgress returns a JSON-lines progress handler.
func jsonProgress(w io.Writer) hfdownloader.ProgressFunc {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	var mu sync.Mutex
	return func(ev hfdownloader.ProgressEvent) {
		mu.Lock()
		_ = enc.Encode(ev)
		mu.Unlock()
	}
}

