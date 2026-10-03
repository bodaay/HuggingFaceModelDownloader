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
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/server"
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
	root.AddCommand(newRebuildCmd(ro))
	root.AddCommand(newListCmd(ro))
	root.AddCommand(newInfoCmd(ro))
	root.AddCommand(newMirrorCmd(ro))
	root.AddCommand(newExportCmd(ro))
	root.AddCommand(newAnalyzeCmd(ctx, ro))
	root.AddCommand(newProxyCmd(ro))

	// Use download as the default action, but on a bare invocation with no
	// arguments — e.g. double-clicking the .exe on Windows — show help instead
	// of failing with "missing REPO" (github issue #79). Repos are downloaded
	// via the explicit "download" subcommand (see README).
	downloadRunE := downloadCmd.RunE
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return downloadRunE(cmd, args)
	}
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
	var legacy bool
	var legacyOutput string
	var localDir string

	// Proxy settings
	var proxyURL string
	var proxyUser string
	var proxyPass string
	var noEnvProxy bool

	cmd := &cobra.Command{
		Use:   "download [REPO]",
		Short: "Download a model or dataset from the Hugging Face Hub",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return applySettingsDefaults(cmd, ro, cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			finalJob, finalCfg, err := finalize(cmd, ro, args, job, cfg, legacy, legacyOutput, localDir, proxyURL, proxyUser, proxyPass, noEnvProxy)
			if err != nil {
				return err
			}

			// Plan-only mode
			if dryRun {
				p, err := hfdownloader.PlanRepo(ctx, finalJob, finalCfg)
				if err != nil {
					return err
				}
				if w := hfdownloader.UnmatchedFiltersWarning(finalJob, p); w != "" {
					fmt.Fprintf(os.Stderr, "warning: %s\n", w)
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

			if err := confirmCopyOnlyCache(cmd, ro, &finalCfg); err != nil {
				return err
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
	cmd.Flags().StringSliceVarP(&job.Excludes, "exclude", "E", nil, "Comma-separated patterns to exclude (e.g. .md,fp16)")
	cmd.Flags().BoolVar(&job.AppendFilterSubdir, "append-filter-subdir", false, "Append each filter as a subdirectory")
	cmd.Flags().StringVar(&job.Shards, "shards", "", "Only these shards of split files, e.g. 1-100 or 1-50,120-185 (download a large model in batches)")
	cmd.Flags().BoolVar(&job.ExactMatch, "exact", false, "Match filters against whole name segments instead of substrings (e.g. -F q6_k matches Q6_K but not Q6_K_XL)")

	// Settings flags
	cmd.Flags().StringVar(&cfg.CacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface or HF_HOME)")
	cmd.Flags().StringVar(&cfg.StaleTimeout, "stale-timeout", "5m", "Timeout for stale incomplete downloads")
	cmd.Flags().IntVarP(&cfg.Concurrency, "connections", "c", 8, "Per-file concurrent connections for LFS range requests")
	cmd.Flags().IntVar(&cfg.MaxActiveDownloads, "max-active", 3, "Maximum number of files downloading at once")
	cmd.Flags().StringVar(&cfg.MultipartThreshold, "multipart-threshold", "32MiB", "Use multipart/range downloads only for files >= this size")
	cmd.Flags().StringVar(&cfg.Verify, "verify", "size", "Verification for non-LFS files: none|size|etag|sha256")
	cmd.Flags().IntVar(&cfg.Retries, "retries", 4, "Max retry attempts per HTTP request/part")
	cmd.Flags().StringVar(&cfg.BackoffInitial, "backoff-initial", "400ms", "Initial retry backoff duration")
	cmd.Flags().StringVar(&cfg.BackoffMax, "backoff-max", "10s", "Maximum retry backoff duration")
	cmd.Flags().StringVar(&cfg.StallTimeout, "stall-timeout", "60s", "Retry a transfer that receives no data for this long (0 disables)")
	cmd.Flags().StringVar(&cfg.LinkMode, "link-mode", "auto", "How cache entries refer to downloaded data: auto (symlink, else hardlink, else copy), symlink, hardlink, copy")
	cmd.Flags().StringVar(&cfg.Endpoint, "endpoint", "", "Custom HuggingFace endpoint URL (e.g. https://hf-mirror.com)")
	cmd.Flags().BoolVar(&cfg.NoManifest, "no-manifest", false, "Do not write hfd.yaml manifest file after download")
	cmd.Flags().BoolVar(&cfg.NoFriendlyView, "no-friendly", false, "Do not create friendly view symlinks (models/, datasets/)")
	cmd.Flags().BoolVar(&legacy, "legacy", false, "Use flat directory structure (v2.x behavior); pair with -o to choose the directory")
	cmd.Flags().StringVarP(&legacyOutput, "output", "o", "", "Output directory for --legacy mode (default: Models/ or Datasets/)")
	cmd.Flags().StringVar(&localDir, "local-dir", "", "Download real files (not HF cache symlinks) into this directory, like `huggingface-cli download --local-dir`")

	// Proxy flags
	cmd.Flags().StringVarP(&proxyURL, "proxy", "x", "", "Proxy URL (http://, https://, or socks5://)")
	cmd.Flags().StringVar(&proxyUser, "proxy-user", "", "Proxy authentication username")
	cmd.Flags().StringVar(&proxyPass, "proxy-pass", "", "Proxy authentication password")
	cmd.Flags().BoolVar(&noEnvProxy, "no-env-proxy", false, "Ignore HTTP_PROXY/HTTPS_PROXY environment variables")

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

func finalize(cmd *cobra.Command, ro *RootOpts, args []string, job *hfdownloader.Job, cfg *hfdownloader.Settings, legacy bool, legacyOutput, localDir string, proxyURL, proxyUser, proxyPass string, noEnvProxy bool) (hfdownloader.Job, hfdownloader.Settings, error) {
	j := *job
	c := *cfg

	// Token
	tok := strings.TrimSpace(ro.Token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("HF_TOKEN"))
	}
	c.Token = tok

	// Proxy configuration
	if proxyURL != "" || noEnvProxy {
		if c.Proxy == nil {
			c.Proxy = &hfdownloader.ProxyConfig{}
		}
		if proxyURL != "" {
			c.Proxy.URL = proxyURL
		}
		if proxyUser != "" {
			c.Proxy.Username = proxyUser
		}
		if proxyPass != "" {
			c.Proxy.Password = proxyPass
		}
		c.Proxy.NoEnvProxy = noEnvProxy
	}

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

	// Directory resolution: by default (no flags) we download into the HF
	// cache structure. --local-dir and --legacy -o both opt into flat
	// directory output with real files at a user-specified path; they are
	// two names for the same underlying v2.x-compatible behavior (the
	// huggingface-cli-style --local-dir is the preferred name).
	switch {
	case localDir != "" && legacyOutput != "":
		return j, c, fmt.Errorf("--local-dir and --output are mutually exclusive; pick one")
	case localDir != "":
		c.OutputDir = localDir
		c.CacheDir = "" // force flat-file mode
	case legacy:
		if legacyOutput != "" {
			c.OutputDir = legacyOutput
		} else if j.IsDataset {
			c.OutputDir = "Datasets"
		} else {
			c.OutputDir = "Models"
		}
		c.CacheDir = ""
	case legacyOutput != "":
		return j, c, fmt.Errorf("--output requires --legacy flag (v2.x compatibility mode); consider --local-dir instead")
	}

	// Build the CLI command string for the manifest (token stripped)
	c.Command = buildCommandString(cmd, j, c)

	return j, c, nil
}

// buildCommandString reconstructs the CLI command from flags, excluding sensitive data.
func buildCommandString(cmd *cobra.Command, job hfdownloader.Job, cfg hfdownloader.Settings) string {
	var parts []string
	parts = append(parts, "hfdownloader", "download", job.Repo)

	if job.IsDataset {
		parts = append(parts, "--dataset")
	}
	if job.Revision != "" && job.Revision != "main" {
		parts = append(parts, "-b", job.Revision)
	}
	for _, f := range job.Filters {
		parts = append(parts, "-F", f)
	}
	for _, e := range job.Excludes {
		parts = append(parts, "-E", e)
	}
	if job.AppendFilterSubdir {
		parts = append(parts, "--append-filter-subdir")
	}
	if job.ExactMatch {
		parts = append(parts, "--exact")
	}
	if cfg.CacheDir != "" {
		parts = append(parts, "--cache-dir", cfg.CacheDir)
	}
	// Flat-file mode output directory. The rebuild command keeps using the
	// older --legacy -o form so manifests generated by this version stay
	// runnable on older releases that don't know --local-dir.
	if cfg.OutputDir != "" && cfg.OutputDir != "Models" && cfg.OutputDir != "Datasets" {
		parts = append(parts, "--legacy", "-o", cfg.OutputDir)
	} else if cfg.OutputDir == "Models" || cfg.OutputDir == "Datasets" {
		parts = append(parts, "--legacy")
	}
	if cfg.Concurrency != 8 {
		parts = append(parts, "-c", fmt.Sprintf("%d", cfg.Concurrency))
	}
	if cfg.MaxActiveDownloads != 3 {
		parts = append(parts, "--max-active", fmt.Sprintf("%d", cfg.MaxActiveDownloads))
	}
	if cfg.Verify != "size" && cfg.Verify != "" {
		parts = append(parts, "--verify", cfg.Verify)
	}
	// Proxy (URL only, credentials intentionally omitted for security)
	if cfg.Proxy != nil && cfg.Proxy.URL != "" {
		parts = append(parts, "--proxy", cfg.Proxy.URL)
	}
	// Note: Token and proxy credentials are intentionally omitted for security

	return strings.Join(parts, " ")
}

// loadConfigMap loads the config file and returns it as a map.
// Returns nil if no config file exists.
func loadConfigMap() map[string]any {
	home, _ := os.UserHomeDir()
	// Try JSON first, then YAML
	jsonPath := filepath.Join(home, ".config", "hfdownloader.json")
	yamlPath := filepath.Join(home, ".config", "hfdownloader.yaml")
	ymlPath := filepath.Join(home, ".config", "hfdownloader.yml")

	var path string
	if _, err := os.Stat(jsonPath); err == nil {
		path = jsonPath
	} else if _, err := os.Stat(yamlPath); err == nil {
		path = yamlPath
	} else if _, err := os.Stat(ymlPath); err == nil {
		path = ymlPath
	}
	if path == "" {
		return nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var cfg map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return nil
		}
	default:
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil
		}
	}
	return cfg
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

	// Load cache-dir from config (v3 replaces the old "output" setting)
	setStr("cache-dir", func(v string) { dst.CacheDir = v })
	setInt("connections", func(v int) { dst.Concurrency = v })
	setInt("max-active", func(v int) { dst.MaxActiveDownloads = v })
	setStr("multipart-threshold", func(v string) { dst.MultipartThreshold = v })
	setStr("verify", func(v string) { dst.Verify = v })
	setInt("retries", func(v int) { dst.Retries = v })
	setStr("backoff-initial", func(v string) { dst.BackoffInitial = v })
	setStr("backoff-max", func(v string) { dst.BackoffMax = v })
	setStr("stall-timeout", func(v string) { dst.StallTimeout = v })
	setStr("link-mode", func(v string) { dst.LinkMode = v })
	setStr("endpoint", func(v string) { dst.Endpoint = v })

	if !cmd.Flags().Changed("token") && os.Getenv("HF_TOKEN") == "" {
		if v, ok := cfg["token"]; ok && v != nil {
			ro.Token = fmt.Sprint(v)
		}
	}

	// Load proxy configuration from config file
	if proxyMap, ok := cfg["proxy"].(map[string]any); ok {
		if dst.Proxy == nil {
			dst.Proxy = &hfdownloader.ProxyConfig{}
		}
		if !cmd.Flags().Changed("proxy") {
			if v, ok := proxyMap["url"]; ok && v != nil {
				dst.Proxy.URL = fmt.Sprint(v)
			}
		}
		if !cmd.Flags().Changed("proxy-user") {
			if v, ok := proxyMap["username"]; ok && v != nil {
				dst.Proxy.Username = fmt.Sprint(v)
			}
		}
		if !cmd.Flags().Changed("proxy-pass") {
			if v, ok := proxyMap["password"]; ok && v != nil {
				dst.Proxy.Password = fmt.Sprint(v)
			}
		}
		if !cmd.Flags().Changed("no-env-proxy") {
			if v, ok := proxyMap["no_env_proxy"]; ok {
				if b, ok := v.(bool); ok {
					dst.Proxy.NoEnvProxy = b
				}
			}
		}
		if v, ok := proxyMap["no_proxy"]; ok && v != nil {
			dst.Proxy.NoProxy = fmt.Sprint(v)
		}
		if v, ok := proxyMap["insecure_skip_verify"]; ok {
			if b, ok := v.(bool); ok {
				dst.Proxy.InsecureSkipVerify = b
			}
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
	var mu sync.Mutex
	announced := map[string]struct{}{}
	return func(ev hfdownloader.ProgressEvent) {
		mu.Lock()
		defer mu.Unlock()
		rev := job.Revision
		if rev == "" {
			rev = "main"
		}
		switch ev.Event {
		case "scan_start":
			fmt.Printf("Scanning %s@%s ...\n", job.Repo, rev)
		case "retry":
			fmt.Printf("retry %s (attempt %d): %s\n", ev.Path, ev.Attempt, ev.Message)
		case "file_assemble", "file_verify":
			// Announce each long post-download phase once per file.
			key := ev.Event + "\x00" + ev.Path
			if _, seen := announced[key]; !seen {
				announced[key] = struct{}{}
				verb := "assembling"
				if ev.Event == "file_verify" {
					verb = "verifying"
				}
				fmt.Printf("%s: %s\n", verb, ev.Path)
			}
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
		case "warning":
			fmt.Fprintf(os.Stderr, "warning: %s\n", ev.Message)
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


// confirmCopyOnlyCache asks, once, what to do when the cache drive supports
// neither symlinks nor hardlinks (e.g. FAT/exFAT): every cache entry would be
// a copy, doubling disk use. Only in an interactive terminal with link-mode
// left on auto; the answer is saved to the config file. Scripts, JSON output
// and the web server never block — they proceed with copies.
func confirmCopyOnlyCache(cmd *cobra.Command, ro *RootOpts, cfg *hfdownloader.Settings) error {
	if cfg.OutputDir != "" || ro.JSONOut || ro.Quiet || cmd.Flags().Changed("link-mode") {
		return nil // flat output, non-interactive output, or an explicit choice
	}
	if mode, _ := hfdownloader.ParseLinkMode(cfg.LinkMode); mode != hfdownloader.LinkAuto {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}
	root := cfg.CacheDir
	if root == "" {
		root = hfdownloader.DefaultCacheDir()
	}
	if sym, hard := hfdownloader.LinkSupport(root); sym || hard {
		return nil
	}

	fmt.Printf("The cache drive (%s) supports neither symlinks nor hardlinks,\n", root)
	fmt.Println("so every file would be stored twice (blob + copy).")
	fmt.Println("  [c] Copy into the HF cache anyway (Python/HF tools find the files)")
	fmt.Println("  [l] Cancel, and use --local-dir <folder> for plain files instead")
	fmt.Print("Choose [c/l]: ")
	var answer string
	fmt.Scanln(&answer)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "c") {
		return fmt.Errorf("cancelled: re-run with --local-dir <folder> to save plain files, or --link-mode copy")
	}
	cfg.LinkMode = string(hfdownloader.LinkCopy)
	if err := server.UpdateConfigFile(map[string]any{"link-mode": "copy"}); err == nil {
		fmt.Printf("Saved link-mode: copy to %s\n", server.ConfigPath())
	}
	return nil
}
