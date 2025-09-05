// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/hfdownloader"
)

var Version = "2.0.0"

// Global (root) CLI options.
type rootOpts struct {
	token   string
	jsonOut bool
	quiet   bool
	verbose bool
	config  string
}

func main() {
	ro := &rootOpts{}
	ctx, cancel := signalContext(context.Background())
	defer cancel()

	root := &cobra.Command{
		Use:           "hfdownloader",
		Short:         "Fast, resumable downloader for Hugging Face models & datasets",
		Long:          "hfdownloader scans Hugging Face repos and downloads files with filtering, retries, verification, and structured progress.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	// Global flags
	root.PersistentFlags().StringVarP(&ro.token, "token", "t", "", "Hugging Face access token (also reads HF_TOKEN env)")
	root.PersistentFlags().BoolVar(&ro.jsonOut, "json", false, "Emit machine-readable JSON events (progress, plan, results)")
	root.PersistentFlags().BoolVarP(&ro.quiet, "quiet", "q", false, "Quiet mode (minimal logs)")
	root.PersistentFlags().BoolVarP(&ro.verbose, "verbose", "v", false, "Verbose logs (debug details)")
	root.PersistentFlags().StringVar(&ro.config, "config", "", "Path to config file (JSON). If provided (or default exists), values initialize flag defaults")

	// download command
	opts := &hfdownloader.Options{}
	downloadCmd := &cobra.Command{
		Use:   "download [REPO]",
		Short: "Download a model or dataset from the Hugging Face Hub",
		Long: `Download a model or dataset from the Hub.

REPO format: "owner/name" or "owner/name:filter1,filter2".
Examples:
  hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF
  hfdownloader download TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q5_0`,
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Apply config defaults either from --config or default location, but do NOT override flags set by user.
			return applyConfigDefaults(cmd, ro, opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			final, err := finalizeOptions(cmd, ro, args, opts)
			if err != nil {
				return err
			}
			if final.Progress == nil {
				final.Progress = cliProgress(ro)
			}
			return hfdownloader.Download(ctx, final)
		},
	}

	// V2 clean flag set
	downloadCmd.Flags().StringVarP(&opts.Repo, "repo", "r", "", "Repository ID (owner/name). If omitted, positional REPO is used")
	downloadCmd.Flags().BoolVar(&opts.IsDataset, "dataset", false, "Treat repo as a dataset")
	downloadCmd.Flags().StringVarP(&opts.OutputDir, "output", "o", "Storage", "Destination base directory")
	downloadCmd.Flags().StringVarP(&opts.Revision, "revision", "b", "main", "Revision/branch to download (e.g. main, refs/pr/1)")
	downloadCmd.Flags().StringSliceVarP(&opts.Filters, "filters", "F", nil, "Comma-separated filters to match LFS artifacts (e.g. q4_0,q5_0). If not set, will parse from REPO suffix ':f1,f2' when present")
	downloadCmd.Flags().BoolVar(&opts.AppendFilterSubdir, "append-filter-subdir", false, "Append each filter as a subdirectory (useful for GGUF/GGML variants)")
	downloadCmd.Flags().IntVarP(&opts.Concurrency, "connections", "c", 8, "Per-file concurrent connections for LFS range requests")
	downloadCmd.Flags().IntVar(&opts.MaxActiveDownloads, "max-active", 3, "Maximum number of files downloading at once")
	downloadCmd.Flags().StringVar(&opts.MultipartThreshold, "multipart-threshold", "32MiB", "Use multipart/range downloads only for files >= this size (units: KB, MB, MiB, GB)")
	downloadCmd.Flags().StringVar(&opts.Verify, "verify", "size", "Verification for non-LFS files: none|size|etag|sha256 (LFS verifies sha256 when provided unless --verify=none)")
	downloadCmd.Flags().IntVar(&opts.Retries, "retries", 4, "Max retry attempts per HTTP request/part")
	downloadCmd.Flags().StringVar(&opts.BackoffInitial, "backoff-initial", "400ms", "Initial retry backoff duration")
	downloadCmd.Flags().StringVar(&opts.BackoffMax, "backoff-max", "10s", "Maximum retry backoff duration")
	downloadCmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Plan only: print the file list and exit")
	downloadCmd.Flags().StringVar(&opts.PlanFormat, "plan-format", "table", "Plan output format for --dry-run: table|json")
	downloadCmd.Flags().BoolVar(&opts.Resume, "resume", true, "Resume partially downloaded files when possible")
	downloadCmd.Flags().BoolVar(&opts.Overwrite, "overwrite", false, "Overwrite existing files (disables resume)")

	root.AddCommand(downloadCmd)

	// generate-config command
	genCfg := &cobra.Command{
		Use:   "generate-config",
		Short: "Write an example config file to ~/.config/hfdownloader.json (or --config path)",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ro.config
			if target == "" {
				home, _ := os.UserHomeDir()
				target = filepath.Join(home, ".config", "hfdownloader.json")
			}
			if err := writeExampleConfig(target); err != nil {
				return err
			}
			fmt.Println("Wrote config to", target)
			return nil
		},
	}
	root.AddCommand(genCfg)

	// Default to "download" when no subcommand
	root.RunE = downloadCmd.RunE
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true})

	if err := root.ExecuteContext(ctx); err != nil {
		if ro.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(map[string]any{"level": "error", "error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

// signalContext cancels when the user hits Ctrl-C or the process receives SIGTERM.
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

func finalizeOptions(cmd *cobra.Command, ro *rootOpts, args []string, bound *hfdownloader.Options) (hfdownloader.Options, error) {
	// Start from the bound struct (already has flags bound)
	opts := *bound

	// Token precedence: flag -> env HF_TOKEN -> none
	tok := strings.TrimSpace(ro.token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("HF_TOKEN"))
	}
	opts.Token = tok

	// Positional repo
	if opts.Repo == "" && len(args) > 0 {
		opts.Repo = args[0]
	}

	// Allow REPO suffix filters
	if strings.Contains(opts.Repo, ":") && len(opts.Filters) == 0 {
		parts := strings.SplitN(opts.Repo, ":", 2)
		opts.Repo = parts[0]
		if strings.TrimSpace(parts[1]) != "" {
			opts.Filters = splitComma(parts[1])
		}
	}

	if opts.Repo == "" {
		return opts, errors.New("missing REPO (owner/name). Pass as positional arg or --repo")
	}
	if !hfdownloader.IsValidModelName(opts.Repo) {
		return opts, fmt.Errorf("invalid repo id %q (expected owner/name)", opts.Repo)
	}

	// Structured logging
	opts.JSON = ro.jsonOut
	opts.Quiet = ro.quiet
	opts.Verbose = ro.verbose

	return opts, nil
}

// applyConfigDefaults applies config file values as defaults (only when a flag wasn't explicitly set).
func applyConfigDefaults(cmd *cobra.Command, ro *rootOpts, dst *hfdownloader.Options) error {
	// Choose config file: --config, or default path if exists
	path := ro.config
	if path == "" {
		home, _ := os.UserHomeDir()
		def := filepath.Join(home, ".config", "hfdownloader.json")
		if _, err := os.Stat(def); err == nil {
			path = def
		}
	}
	if path == "" {
		return nil // nothing to apply
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("invalid config file: %w", err)
	}

	// helper to set only if flag NOT changed by user
	setStr := func(flagName string, set func(string)) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		if v, ok := cfg[flagName]; ok && v != nil {
			set(fmt.Sprint(v))
		}
	}
	setBool := func(flagName string, set func(bool)) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		if v, ok := cfg[flagName]; ok && v != nil {
			switch vv := v.(type) {
			case bool:
				set(vv)
			case string:
				set(vv == "true" || vv == "1")
			}
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

	setStr("repo", func(v string) { dst.Repo = v })
	setBool("dataset", func(v bool) { dst.IsDataset = v })
	setStr("output", func(v string) { dst.OutputDir = v })
	setStr("revision", func(v string) { dst.Revision = v })
	if v, ok := cfg["filters"]; ok && v != nil && !cmd.Flags().Changed("filters") {
		switch vv := v.(type) {
		case string:
			dst.Filters = splitComma(vv)
		case []any:
			dst.Filters = dst.Filters[:0]
			for _, it := range vv {
				dst.Filters = append(dst.Filters, fmt.Sprint(it))
			}
		}
	}
	setBool("append-filter-subdir", func(v bool) { dst.AppendFilterSubdir = v })
	setInt("connections", func(v int) { dst.Concurrency = v })
	setInt("max-active", func(v int) { dst.MaxActiveDownloads = v })
	setStr("multipart-threshold", func(v string) { dst.MultipartThreshold = v })
	setStr("verify", func(v string) { dst.Verify = v })
	setInt("retries", func(v int) { dst.Retries = v })
	setStr("backoff-initial", func(v string) { dst.BackoffInitial = v })
	setStr("backoff-max", func(v string) { dst.BackoffMax = v })
	setBool("dry-run", func(v bool) { dst.DryRun = v })
	setStr("plan-format", func(v string) { dst.PlanFormat = v })
	setBool("resume", func(v bool) { dst.Resume = v })
	setBool("overwrite", func(v bool) { dst.Overwrite = v })

	// token is global; only apply if user didn't pass flag and no env var
	if !cmd.Flags().Changed("token") && os.Getenv("HF_TOKEN") == "" {
		if v, ok := cfg["token"]; ok && v != nil {
			ro.token = fmt.Sprint(v)
		}
	}

	return nil
}

// writeExampleConfig writes a valid JSON config to the given path.
func writeExampleConfig(path string) error {
	ex := map[string]any{
		"repo":                 "TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_0",
		"dataset":              false,
		"revision":             "main",
		"output":               "Storage",
		"filters":              []string{"q4_0", "q5_0"},
		"append-filter-subdir": true,
		"connections":          8,
		"max-active":           3,
		"multipart-threshold":  "32MiB",
		"verify":               "size",
		"retries":              4,
		"backoff-initial":      "400ms",
		"backoff-max":          "10s",
		"dry-run":              false,
		"plan-format":          "table",
		"resume":               true,
		"overwrite":            false,
		"token":                "",
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(ex, "", "  ")
	return os.WriteFile(path, b, 0o644)
}

// cliProgress returns a progress callback honoring JSON/quiet modes.
func cliProgress(ro *rootOpts) hfdownloader.ProgressFunc {
	if ro.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		return func(ev hfdownloader.ProgressEvent) {
			_ = enc.Encode(ev) // ignore encode errors
		}
	}
	if ro.quiet {
		return func(ev hfdownloader.ProgressEvent) {
			// only print final errors and summary
			if ev.Level == "error" || ev.Event == "done" {
				fmt.Fprintf(os.Stderr, "%s\n", ev.Message)
			}
		}
	}
	// human-friendly
	return func(ev hfdownloader.ProgressEvent) {
		switch ev.Event {
		case "scan_start":
			fmt.Printf("Scanning %s@%s ...\n", ev.Repo, ev.Revision)
		case "retry":
			fmt.Printf("retry %s (attempt %d): %s\n", ev.Path, ev.Attempt, ev.Message)
		case "file_start":
			fmt.Printf("downloading: %s (%d bytes)\n", ev.Path, ev.Total)
		case "file_progress":
			if ev.Total > 0 {
				p := float64(ev.Bytes) / float64(ev.Total) * 100
				fmt.Printf("  %s: %.1f%%\n", ev.Path, p)
			}
		case "file_done":
			fmt.Printf("done: %s\n", ev.Path)
		case "error":
			fmt.Fprintf(os.Stderr, "error: %s\n", ev.Message)
		case "done":
			fmt.Println(ev.Message)
		}
	}
}

// splitComma splits a comma-separated list into a []string, trimming spaces.
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
