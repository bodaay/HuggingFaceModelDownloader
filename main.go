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
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	root.PersistentFlags().StringVarP(&ro.token, "token", "t", "", "Hugging Face access token (also reads HF_TOKEN env)")
	root.PersistentFlags().BoolVar(&ro.jsonOut, "json", false, "Emit machine-readable JSON events (progress, plan, results)")
	root.PersistentFlags().BoolVarP(&ro.quiet, "quiet", "q", false, "Quiet mode (minimal logs)")
	root.PersistentFlags().BoolVarP(&ro.verbose, "verbose", "v", false, "Verbose logs (debug details)")
	root.PersistentFlags().StringVar(&ro.config, "config", "", "Path to config file (JSON).")

	job := &hfdownloader.Job{}
	cfg := &hfdownloader.Settings{}
	var dryRun bool
	var planFmt string

	downloadCmd := &cobra.Command{
		Use:   "download [REPO]",
		Short: "Download a model or dataset from the Hugging Face Hub",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return applySettingsDefaults(cmd, ro, cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			finalJob, finalCfg, err := finalize(cmd, ro, args, job, cfg, dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				p, err := hfdownloader.PlanRepo(ctx, finalJob, finalCfg)
				if err != nil {
					return err
				}
				if strings.ToLower(planFmt) == "json" {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				}
				fmt.Printf("Plan for %s@%s (%d files):\n", finalJob.Repo, orDefault(finalJob.Revision, "main"), len(p.Items))
				for _, it := range p.Items {
					fmt.Printf("  %s  %8d  lfs=%t\n", it.RelativePath, it.Size, it.LFS)
				}
				return nil
			}
			var progress hfdownloader.ProgressFunc
			if ro.jsonOut || ro.quiet {
				progress = cliProgress(ro, finalJob)
			} else {
				ui := newLiveRenderer(finalJob, finalCfg)
				defer ui.Close()
				progress = ui.Handler()
			}
			return hfdownloader.Download(ctx, finalJob, finalCfg, progress)
		},
	}

	// job flags
	downloadCmd.Flags().StringVarP(&job.Repo, "repo", "r", "", "Repository ID (owner/name). If omitted, positional REPO is used")
	downloadCmd.Flags().BoolVar(&job.IsDataset, "dataset", false, "Treat repo as a dataset")
	downloadCmd.Flags().StringVarP(&job.Revision, "revision", "b", "main", "Revision/branch to download (e.g. main, refs/pr/1)")
	downloadCmd.Flags().StringSliceVarP(&job.Filters, "filters", "F", nil, "Comma-separated filters to match LFS artifacts (e.g. q4_0,q5_0). If not set, will parse from REPO suffix ':f1,f2' when present")
	downloadCmd.Flags().BoolVar(&job.AppendFilterSubdir, "append-filter-subdir", false, "Append each filter as a subdirectory (useful for GGUF/GGML variants)")

	// settings flags
	downloadCmd.Flags().StringVarP(&cfg.OutputDir, "output", "o", "Storage", "Destination base directory")
	downloadCmd.Flags().IntVarP(&cfg.Concurrency, "connections", "c", 8, "Per-file concurrent connections for LFS range requests")
	downloadCmd.Flags().IntVar(&cfg.MaxActiveDownloads, "max-active", 3, "Maximum number of files downloading at once")
	downloadCmd.Flags().StringVar(&cfg.MultipartThreshold, "multipart-threshold", "2566MiB", "Use multipart/range downloads only for files >= this size")
	downloadCmd.Flags().StringVar(&cfg.Verify, "verify", "size", "Verification for non-LFS files: none|size|etag|sha256 (LFS verifies sha256 when provided unless --verify=none)")
	downloadCmd.Flags().IntVar(&cfg.Retries, "retries", 4, "Max retry attempts per HTTP request/part")
	downloadCmd.Flags().StringVar(&cfg.BackoffInitial, "backoff-initial", "400ms", "Initial retry backoff duration")
	downloadCmd.Flags().StringVar(&cfg.BackoffMax, "backoff-max", "10s", "Maximum retry backoff duration")

	// CLI-only
	downloadCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan only: print the file list and exit")
	downloadCmd.Flags().StringVar(&planFmt, "plan-format", "table", "Plan output format for --dry-run: table|json")

	root.AddCommand(downloadCmd)

	root.RunE = downloadCmd.RunE
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true})

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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

func finalize(cmd *cobra.Command, ro *rootOpts, args []string, job *hfdownloader.Job, cfg *hfdownloader.Settings, dryRun bool) (hfdownloader.Job, hfdownloader.Settings, error) {
	j := *job
	c := *cfg

	// token
	tok := strings.TrimSpace(ro.token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("HF_TOKEN"))
	}
	c.Token = tok

	if j.Repo == "" && len(args) > 0 {
		j.Repo = args[0]
	}
	if strings.Contains(j.Repo, ":") && len(j.Filters) == 0 {
		parts := strings.SplitN(j.Repo, ":", 2)
		j.Repo = parts[0]
		if strings.TrimSpace(parts[1]) != "" {
			j.Filters = splitComma(parts[1])
		}
	}
	if j.Repo == "" {
		return j, c, errors.New("missing REPO (owner/name). Pass as positional arg or --repo")
	}
	if !hfdownloader.IsValidModelName(j.Repo) {
		return j, c, fmt.Errorf("invalid repo id %q (expected owner/name)", j.Repo)
	}
	return j, c, nil
}

func applySettingsDefaults(cmd *cobra.Command, ro *rootOpts, dst *hfdownloader.Settings) error {
	path := ro.config
	if path == "" {
		home, _ := os.UserHomeDir()
		def := filepath.Join(home, ".config", "hfdownloader.json")
		if _, err := os.Stat(def); err == nil {
			path = def
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
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("invalid config file: %w", err)
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
			ro.token = fmt.Sprint(v)
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

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// Simple logger used when --json or --quiet is set
func cliProgress(ro *rootOpts, job hfdownloader.Job) hfdownloader.ProgressFunc {
	return func(ev hfdownloader.ProgressEvent) {
		switch ev.Event {
		case "scan_start":
			fmt.Printf("Scanning %s@%s ...\n", job.Repo, orDefault(job.Revision, "main"))
		case "retry":
			fmt.Printf("retry %s (attempt %d): %s\n", ev.Path, ev.Attempt, ev.Message)
		case "file_start":
			fmt.Printf("downloading: %s (%d bytes)\n", ev.Path, ev.Total)
		case "file_progress":
			// no-op in quiet
		case "file_done":
			fmt.Printf("done: %s\n", ev.Path)
		case "error":
			fmt.Fprintf(os.Stderr, "error: %s\n", ev.Message)
		case "done":
			fmt.Println(ev.Message)
		}
	}
}
