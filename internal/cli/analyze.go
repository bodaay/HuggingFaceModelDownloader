// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/tui"
	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
	"github.com/bodaay/HuggingFaceModelDownloader/pkg/smartdl"
)

func newAnalyzeCmd(ctx context.Context, ro *RootOpts) *cobra.Command {
	var (
		isDataset   bool
		endpoint    string
		formatOut   string
		revision    string
		interactive bool
		cacheDir    string
	)

	cmd := &cobra.Command{
		Use:   "analyze <repo>",
		Short: "Analyze a HuggingFace repository to determine its type and structure",
		Long: `Analyze a HuggingFace repository to detect its type (GGUF, Transformers,
Diffusers, LoRA, etc.) and provide detailed information about available options.

This command fetches the repository file tree and metadata without downloading
any files, then presents intelligent analysis based on the detected type.

Examples:
  # Analyze a GGUF model
  hfdownloader analyze TheBloke/Mistral-7B-Instruct-v0.2-GGUF

  # Interactive mode - select files with TUI
  hfdownloader analyze TheBloke/Mistral-7B-Instruct-v0.2-GGUF -i

  # Analyze a diffusers model
  hfdownloader analyze stabilityai/stable-diffusion-xl-base-1.0

  # Analyze a specific branch
  hfdownloader analyze TheBloke/Llama-2-7B-Chat-GPTQ -b gptq-4bit-32g-actorder_True

  # Analyze a dataset
  hfdownloader analyze --dataset HuggingFaceFW/fineweb

  # Get JSON output for scripting
  hfdownloader analyze --format json TheBloke/Mistral-7B-Instruct-v0.2-GGUF`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := args[0]

			// Validate repo format
			if !hfdownloader.IsValidModelName(repo) {
				return fmt.Errorf("invalid repo id %q (expected owner/name)", repo)
			}

			// Get token
			token := strings.TrimSpace(ro.Token)
			if token == "" {
				token = strings.TrimSpace(os.Getenv("HF_TOKEN"))
			}

			// Create analyzer
			opts := smartdl.AnalyzerOptions{
				Token:    token,
				Endpoint: endpoint,
			}
			analyzer := smartdl.NewAnalyzer(opts)

			// Analyze with revision
			info, err := analyzer.AnalyzeWithRevision(ctx, repo, isDataset, revision)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			// Output
			if formatOut == "json" || ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}

			// Interactive mode - launch TUI selector
			if interactive {
				// Check if there are multiple refs and user didn't specify a revision
				if len(info.Refs) > 1 && revision == "main" {
					// Show branch picker
					branchResult, err := tui.RunBranchPicker(repo, info.Refs)
					if err != nil {
						return fmt.Errorf("branch picker failed: %w", err)
					}

					if branchResult.Cancelled {
						fmt.Println("Cancelled.")
						return nil
					}

					// If user selected a different branch, re-analyze
					if branchResult.Selected != "" && branchResult.Selected != info.Branch {
						fmt.Printf("Analyzing %s (%s)...\n", repo, branchResult.Selected)
						info, err = analyzer.AnalyzeWithRevision(ctx, repo, isDataset, branchResult.Selected)
						if err != nil {
							return fmt.Errorf("analysis failed: %w", err)
						}
					}
				}

				return runInteractiveSelector(ctx, info, ro, cacheDir)
			}

			// Human-readable output
			printAnalysis(info)
			return nil
		},
	}

	cmd.Flags().BoolVar(&isDataset, "dataset", false, "Analyze as a dataset repository")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Custom HuggingFace endpoint URL (e.g. https://hf-mirror.com)")
	cmd.Flags().StringVar(&formatOut, "format", "text", "Output format: text, json")
	cmd.Flags().StringVarP(&revision, "revision", "b", "main", "Branch or revision to analyze (e.g. main, gptq-4bit-32g-actorder_True)")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Launch interactive TUI for selecting files to download")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "HuggingFace cache directory (default: ~/.cache/huggingface or HF_HOME)")

	return cmd
}

// runInteractiveSelector launches the TUI selector and handles the result.
func runInteractiveSelector(ctx context.Context, info *smartdl.RepoInfo, ro *RootOpts, cacheDir string) error {
	// Check if there are selectable items
	if len(info.SelectableItems) == 0 {
		fmt.Println("No selectable items found for this repository.")
		fmt.Println("Falling back to standard output...")
		fmt.Println()
		printAnalysis(info)
		return nil
	}

	// Run the TUI selector
	result, err := tui.RunSelector(info)
	if err != nil {
		return fmt.Errorf("selector failed: %w", err)
	}

	// Handle the result
	switch result.Action {
	case "download":
		if len(result.SelectedFilters) == 0 {
			fmt.Println("\nNo items selected. Aborting download.")
			return nil
		}

		fmt.Printf("\nStarting download with filters: %v\n", result.SelectedFilters)
		fmt.Printf("Command: %s\n\n", result.CLICommand)

		// Build job and settings for download
		job := hfdownloader.Job{
			Repo:      info.Repo,
			IsDataset: info.IsDataset,
			Revision:  info.Branch,
			Filters:   result.SelectedFilters,
		}
		if job.Revision == "" {
			job.Revision = "main"
		}

		// Get token
		token := strings.TrimSpace(ro.Token)
		if token == "" {
			token = strings.TrimSpace(os.Getenv("HF_TOKEN"))
		}

		// Build settings - use defaults, then apply config file
		cfg := hfdownloader.Settings{
			Token:              token,
			CacheDir:           cacheDir,
			Concurrency:        8,
			MaxActiveDownloads: 3,
			MultipartThreshold: "32MiB",
			Verify:             "size",
			Retries:            4,
			BackoffInitial:     "400ms",
			BackoffMax:         "10s",
		}

		// Load settings from config file (respects cache-dir, connections, etc.)
		if cfgFile := loadConfigMap(); cfgFile != nil {
			if cacheDir == "" {
				if v, ok := cfgFile["cache-dir"].(string); ok && v != "" {
					cfg.CacheDir = v
				}
			}
			if v, ok := cfgFile["connections"].(float64); ok && v > 0 {
				cfg.Concurrency = int(v)
			}
			if v, ok := cfgFile["max-active"].(float64); ok && v > 0 {
				cfg.MaxActiveDownloads = int(v)
			}
			if v, ok := cfgFile["multipart-threshold"].(string); ok && v != "" {
				cfg.MultipartThreshold = v
			}
			if v, ok := cfgFile["verify"].(string); ok && v != "" {
				cfg.Verify = v
			}
			if v, ok := cfgFile["retries"].(float64); ok && v > 0 {
				cfg.Retries = int(v)
			}
		}

		// Use live TUI for download progress
		ui := tui.NewLiveRenderer(job, cfg)
		defer ui.Close()
		progress := ui.Handler()

		return hfdownloader.Download(ctx, job, cfg, progress)

	case "copy":
		fmt.Printf("\nCommand copied to clipboard:\n  %s\n", result.CLICommand)

	case "cancel":
		fmt.Println("\nCancelled.")
	}

	return nil
}

func printAnalysis(info *smartdl.RepoInfo) {
	fmt.Printf("Repository: %s\n", info.Repo)
	if info.Branch != "" && info.Branch != "main" {
		fmt.Printf("Branch:     %s\n", info.Branch)
	}
	fmt.Printf("Type:       %s (%s)\n", info.Type, info.TypeDescription)
	fmt.Printf("Files:      %d\n", info.FileCount)
	fmt.Printf("Total Size: %s\n", info.TotalSizeHuman)
	fmt.Println()

	switch info.Type {
	case smartdl.TypeGGUF:
		printGGUFAnalysis(info)
	case smartdl.TypeTransformers:
		printTransformersAnalysis(info)
	case smartdl.TypeDiffusers:
		printDiffusersAnalysis(info)
	case smartdl.TypeLoRA:
		printLoRAAnalysis(info)
	case smartdl.TypeGPTQ, smartdl.TypeAWQ:
		printQuantizedAnalysis(info)
	case smartdl.TypeDataset:
		printDatasetAnalysis(info)
	case smartdl.TypeAudio:
		printAudioAnalysis(info)
	case smartdl.TypeVision:
		printVisionAnalysis(info)
	case smartdl.TypeMultimodal:
		printMultimodalAnalysis(info)
	case smartdl.TypeONNX:
		printONNXAnalysis(info)
	default:
		printGenericAnalysis(info)
	}

	// Print selectable items (unified across all types)
	printSelectableItems(info)

	// Print related downloads (e.g., base model for LoRA)
	printRelatedDownloads(info)

	// Print CLI commands at the end
	printCLICommands(info)
}

// printSelectableItems displays available download options
func printSelectableItems(info *smartdl.RepoInfo) {
	if len(info.SelectableItems) == 0 {
		return
	}

	// Group items by category
	categories := make(map[string][]smartdl.SelectableItem)
	for _, item := range info.SelectableItems {
		cat := item.Category
		if cat == "" {
			cat = "options"
		}
		categories[cat] = append(categories[cat], item)
	}

	categoryTitles := map[string]string{
		"quantization": "Available Quantizations",
		"variant":      "Available Variants",
		"component":    "Available Components",
		"split":        "Available Splits",
		"format":       "Weight Formats",
		"precision":    "Precision Options",
		"options":      "Available Options",
	}

	for cat, items := range categories {
		title := categoryTitles[cat]
		if title == "" {
			title = "Available " + strings.Title(cat)
		}

		fmt.Println()
		fmt.Printf("%s:\n", title)

		// Determine columns based on category
		switch cat {
		case "quantization":
			fmt.Printf("  %-12s  %12s  %12s  %s  %s\n", "OPTION", "SIZE", "RAM", "QUALITY", "FILTER")
			fmt.Printf("  %-12s  %12s  %12s  %s  %s\n", "------------", "------------", "------------", "-------", "------")
			for _, item := range items {
				stars := ""
				if item.Quality > 0 {
					stars = strings.Repeat("★", item.Quality) + strings.Repeat("☆", 5-item.Quality)
				}
				rec := ""
				if item.Recommended {
					rec = " *"
				}
				fmt.Printf("  %-12s  %12s  %12s  %s  -F %s%s\n",
					item.Label,
					item.SizeHuman,
					item.RAMHuman,
					stars,
					item.FilterValue,
					rec,
				)
			}
		case "component", "split":
			fmt.Printf("  %-20s  %12s  %10s  %s\n", "OPTION", "SIZE", "REC", "FILTER")
			fmt.Printf("  %-20s  %12s  %10s  %s\n", "--------------------", "------------", "----------", "------")
			for _, item := range items {
				rec := ""
				if item.Recommended {
					rec = "yes"
				}
				fmt.Printf("  %-20s  %12s  %10s  -F %s\n",
					item.Label,
					item.SizeHuman,
					rec,
					item.FilterValue,
				)
			}
		default:
			fmt.Printf("  %-15s  %12s  %10s  %s\n", "OPTION", "SIZE", "REC", "FILTER")
			fmt.Printf("  %-15s  %12s  %10s  %s\n", "---------------", "------------", "----------", "------")
			for _, item := range items {
				rec := ""
				if item.Recommended {
					rec = "yes"
				}
				sizeStr := item.SizeHuman
				if sizeStr == "" {
					sizeStr = "-"
				}
				fmt.Printf("  %-15s  %12s  %10s  -F %s\n",
					item.Label,
					sizeStr,
					rec,
					item.FilterValue,
				)
			}
		}
	}

	// Note about recommended items
	hasRecommended := false
	for _, item := range info.SelectableItems {
		if item.Recommended {
			hasRecommended = true
			break
		}
	}
	if hasRecommended {
		fmt.Println()
		fmt.Println("  * = recommended")
	}
}

// printRelatedDownloads displays related downloads like base model for LoRA
func printRelatedDownloads(info *smartdl.RepoInfo) {
	if len(info.RelatedDownloads) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("Related Downloads:")
	for _, dl := range info.RelatedDownloads {
		required := ""
		if dl.Required {
			required = " (required)"
		}
		fmt.Printf("  %s%s\n", dl.Label, required)
		fmt.Printf("    Repo: %s\n", dl.Repo)
		if dl.Description != "" {
			fmt.Printf("    %s\n", dl.Description)
		}
		if dl.SizeHuman != "" {
			fmt.Printf("    Size: %s\n", dl.SizeHuman)
		}
	}
}

// printCLICommands displays the generated CLI commands
func printCLICommands(info *smartdl.RepoInfo) {
	fmt.Println()
	fmt.Println("Download Commands:")
	if info.CLICommand != "" {
		fmt.Printf("  Full:        %s\n", info.CLICommand)
	}
	if info.CLICommandFull != "" && info.CLICommandFull != info.CLICommand {
		fmt.Printf("  Recommended: %s\n", info.CLICommandFull)
	}
}

func printGGUFAnalysis(info *smartdl.RepoInfo) {
	if info.GGUF == nil {
		return
	}

	gguf := info.GGUF
	if gguf.ModelName != "" {
		fmt.Printf("Model:      %s\n", gguf.ModelName)
	}
	if gguf.ParameterCount != "" {
		fmt.Printf("Parameters: %s\n", gguf.ParameterCount)
	}
	// Quantizations are displayed through unified SelectableItems section
}

func printTransformersAnalysis(info *smartdl.RepoInfo) {
	if info.Transformers == nil {
		printGenericAnalysis(info)
		return
	}

	t := info.Transformers

	// Architecture
	if t.Architecture != "" {
		fmt.Printf("Architecture: %s\n", t.Architecture)
		if t.ArchitectureDescription != "" {
			fmt.Printf("              %s\n", t.ArchitectureDescription)
		}
	}

	// Task
	if t.Task != "" {
		fmt.Printf("Task:         %s\n", t.Task)
		if t.TaskDescription != "" {
			fmt.Printf("              %s\n", t.TaskDescription)
		}
	}
	fmt.Println()

	// Model specs
	fmt.Println("Model Configuration:")
	if t.EstimatedParameters != "" {
		fmt.Printf("  Parameters:      ~%s\n", t.EstimatedParameters)
	}
	if t.HiddenSize > 0 {
		fmt.Printf("  Hidden Size:     %d\n", t.HiddenSize)
	}
	if t.NumHiddenLayers > 0 {
		fmt.Printf("  Layers:          %d\n", t.NumHiddenLayers)
	}
	if t.NumAttentionHeads > 0 {
		fmt.Printf("  Attention Heads: %d\n", t.NumAttentionHeads)
	}
	if t.VocabSize > 0 {
		fmt.Printf("  Vocab Size:      %d\n", t.VocabSize)
	}
	if t.ContextLength > 0 {
		fmt.Printf("  Context Length:  %d tokens\n", t.ContextLength)
	}
	if t.Precision != "" {
		fmt.Printf("  Precision:       %s\n", t.Precision)
	}
	fmt.Println()

	// Sharding info
	if t.IsSharded {
		fmt.Printf("Sharding:     %d shards\n", t.ShardCount)
	}

	// Weight files
	if len(t.WeightFiles) > 0 {
		fmt.Println("Weight Files:")
		fmt.Printf("  %-45s  %12s  %s\n", "NAME", "SIZE", "FORMAT")
		fmt.Printf("  %-45s  %12s  %s\n", strings.Repeat("-", 45), "------------", "------")

		limit := 10
		for i, wf := range t.WeightFiles {
			if i >= limit {
				fmt.Printf("  ... and %d more files\n", len(t.WeightFiles)-limit)
				break
			}
			name := wf.Name
			if len(name) > 45 {
				name = "..." + name[len(name)-42:]
			}
			fmt.Printf("  %-45s  %12s  %s\n", name, wf.SizeHuman, wf.Format)
		}
		fmt.Println()
	}

	// Tokenizer
	if t.Tokenizer != nil && t.Tokenizer.Type != "" {
		fmt.Println("Tokenizer:")
		fmt.Printf("  Type:        %s\n", t.Tokenizer.Type)
		if t.Tokenizer.VocabSize > 0 {
			fmt.Printf("  Vocab Size:  %d\n", t.Tokenizer.VocabSize)
		}
		if t.Tokenizer.ModelMaxLength > 0 {
			fmt.Printf("  Max Length:  %d\n", t.Tokenizer.ModelMaxLength)
		}
		if t.Tokenizer.HasChatTemplate {
			fmt.Printf("  Chat:        yes (has chat template)\n")
		}
		fmt.Println()
	}

	// Compatible backends
	if len(t.Backends) > 0 {
		fmt.Printf("Backends:     %s\n", strings.Join(t.Backends, ", "))
	}
	// Download commands are displayed through unified CLI commands section
}

func printDiffusersAnalysis(info *smartdl.RepoInfo) {
	if info.Diffusers == nil {
		return
	}

	diff := info.Diffusers
	fmt.Printf("Pipeline:   %s\n", diff.PipelineType)
	if diff.PipelineDescription != "" {
		fmt.Printf("            %s\n", diff.PipelineDescription)
	}
	if diff.DiffusersVersion != "" {
		fmt.Printf("Version:    diffusers %s\n", diff.DiffusersVersion)
	}
	fmt.Println()

	if len(diff.Variants) > 0 {
		fmt.Printf("Variants:   %s\n", strings.Join(diff.Variants, ", "))
		fmt.Println()
	}

	fmt.Println("Components:")
	fmt.Printf("  %-20s  %12s  %10s  %s\n", "NAME", "SIZE", "REQUIRED", "CLASS")
	fmt.Printf("  %-20s  %12s  %10s  %s\n", "--------------------", "------------", "----------", "-----")

	for _, c := range diff.Components {
		required := ""
		if c.Required {
			required = "yes"
		}
		fmt.Printf("  %-20s  %12s  %10s  %s\n", c.Name, c.SizeHuman, required, c.ClassName)
	}

	// Download commands are displayed through unified CLI commands section
}

func printLoRAAnalysis(info *smartdl.RepoInfo) {
	if info.LoRA == nil {
		return
	}

	lora := info.LoRA
	fmt.Printf("Adapter:    %s\n", lora.AdapterType)
	if lora.AdapterDescription != "" {
		fmt.Printf("            %s\n", lora.AdapterDescription)
	}
	fmt.Println()

	if lora.BaseModel != "" {
		fmt.Printf("Base Model: %s\n", lora.BaseModel)
		fmt.Println()
		fmt.Println("⚠️  This adapter requires the base model to be downloaded separately.")
	}

	if lora.Rank > 0 {
		fmt.Printf("Rank (r):   %d\n", lora.Rank)
	}
	if lora.Alpha > 0 {
		fmt.Printf("Alpha:      %.1f\n", lora.Alpha)
	}
	if len(lora.TargetModules) > 0 {
		fmt.Printf("Targets:    %s\n", strings.Join(lora.TargetModules, ", "))
	}

	// Download commands are displayed through unified CLI commands section
}

func printQuantizedAnalysis(info *smartdl.RepoInfo) {
	if info.Quantized == nil {
		return
	}

	q := info.Quantized
	fmt.Printf("Method:     %s", strings.ToUpper(q.Method))
	if q.MethodDescription != "" {
		fmt.Printf(" - %s", q.MethodDescription)
	}
	fmt.Println()

	fmt.Printf("Bits:       %d-bit\n", q.Bits)
	if q.GroupSize > 0 {
		fmt.Printf("Group Size: %d\n", q.GroupSize)
	}
	if q.EstimatedVRAM > 0 {
		fmt.Printf("Est. VRAM:  %s\n", humanSize(q.EstimatedVRAM))
	}
	fmt.Println()

	if len(q.Backends) > 0 {
		fmt.Printf("Backends:   %s\n", strings.Join(q.Backends, ", "))
	}

	if q.DescAct {
		fmt.Println("Note:       desc_act=True (may be slower but more accurate)")
	}
	// Download commands are displayed through unified CLI commands section
}

func printDatasetAnalysis(info *smartdl.RepoInfo) {
	if info.Dataset == nil {
		return
	}

	ds := info.Dataset
	if len(ds.Formats) > 0 {
		fmt.Printf("Formats:    %s\n", strings.Join(ds.Formats, ", "))
	}
	if ds.PrimaryFormat != "" {
		fmt.Printf("Recommended: %s\n", ds.PrimaryFormat)
	}
	if len(ds.Configs) > 0 {
		fmt.Printf("Configs:    %s\n", strings.Join(ds.Configs, ", "))
	}
	fmt.Println()

	fmt.Println("Splits:")
	fmt.Printf("  %-20s  %10s  %12s\n", "NAME", "FILES", "SIZE")
	fmt.Printf("  %-20s  %10s  %12s\n", "--------------------", "----------", "------------")

	for _, s := range ds.Splits {
		fmt.Printf("  %-20s  %10d  %12s\n", s.Name, s.FileCount, s.SizeHuman)
	}
	// Download commands are displayed through unified CLI commands section
}

func printGenericAnalysis(info *smartdl.RepoInfo) {
	fmt.Println("Files:")
	fmt.Printf("  %-50s  %12s  %s\n", "NAME", "SIZE", "LFS")
	fmt.Printf("  %-50s  %12s  %s\n", strings.Repeat("-", 50), "------------", "---")

	// Show up to 20 files
	limit := 20
	for i, f := range info.Files {
		if i >= limit {
			fmt.Printf("  ... and %d more files\n", len(info.Files)-limit)
			break
		}
		name := f.Path
		if len(name) > 50 {
			name = "..." + name[len(name)-47:]
		}
		lfs := ""
		if f.IsLFS {
			lfs = "yes"
		}
		fmt.Printf("  %-50s  %12s  %s\n", name, f.SizeHuman, lfs)
	}
	// Download commands are displayed through unified CLI commands section
}

func printAudioAnalysis(info *smartdl.RepoInfo) {
	if info.Audio == nil {
		printGenericAnalysis(info)
		return
	}

	audio := info.Audio
	fmt.Printf("Task:       %s\n", audio.Task)
	if audio.TaskDescription != "" {
		fmt.Printf("            %s\n", audio.TaskDescription)
	}
	fmt.Println()

	if audio.FeatureExtractorType != "" {
		fmt.Printf("Feature Extractor: %s\n", audio.FeatureExtractorType)
	}
	if audio.SampleRate > 0 {
		fmt.Printf("Sample Rate:       %d Hz\n", audio.SampleRate)
	}
	if audio.NumMelBins > 0 {
		fmt.Printf("Mel Bins:          %d\n", audio.NumMelBins)
	}
	if len(audio.Languages) > 0 {
		fmt.Printf("Languages:         %s\n", strings.Join(audio.Languages, ", "))
	}
	if audio.Framework != "" {
		fmt.Printf("Framework:         %s\n", audio.Framework)
	}
	// Download commands are displayed through unified CLI commands section
}

func printVisionAnalysis(info *smartdl.RepoInfo) {
	if info.Vision == nil {
		printGenericAnalysis(info)
		return
	}

	vision := info.Vision
	fmt.Printf("Task:       %s\n", vision.Task)
	if vision.TaskDescription != "" {
		fmt.Printf("            %s\n", vision.TaskDescription)
	}
	fmt.Println()

	if vision.ImageProcessorType != "" {
		fmt.Printf("Image Processor: %s\n", vision.ImageProcessorType)
	}
	if vision.ImageSize.Height > 0 && vision.ImageSize.Width > 0 {
		fmt.Printf("Input Size:      %dx%d\n", vision.ImageSize.Width, vision.ImageSize.Height)
	}
	if vision.NumChannels > 0 {
		fmt.Printf("Channels:        %d\n", vision.NumChannels)
	}
	if vision.NumLabels > 0 {
		fmt.Printf("Classes:         %d\n", vision.NumLabels)
	}
	if vision.Normalization != nil && len(vision.Normalization.Mean) > 0 {
		fmt.Printf("Normalization:   mean=%v, std=%v\n", vision.Normalization.Mean, vision.Normalization.Std)
	}
	if vision.Framework != "" {
		fmt.Printf("Framework:       %s\n", vision.Framework)
	}
	// Download commands are displayed through unified CLI commands section
}

func printMultimodalAnalysis(info *smartdl.RepoInfo) {
	if info.Multimodal == nil {
		printGenericAnalysis(info)
		return
	}

	mm := info.Multimodal
	fmt.Printf("Task:       %s\n", mm.Task)
	if mm.TaskDescription != "" {
		fmt.Printf("            %s\n", mm.TaskDescription)
	}
	fmt.Println()

	if len(mm.Modalities) > 0 {
		fmt.Printf("Modalities:    %s\n", strings.Join(mm.Modalities, ", "))
	}
	if mm.ProcessorType != "" {
		fmt.Printf("Processor:     %s\n", mm.ProcessorType)
	}
	if mm.VisionEncoder != "" {
		fmt.Printf("Vision Enc:    %s\n", mm.VisionEncoder)
	}
	if mm.TextEncoder != "" {
		fmt.Printf("Text Enc:      %s\n", mm.TextEncoder)
	}
	if mm.ImageSize.Height > 0 && mm.ImageSize.Width > 0 {
		fmt.Printf("Image Size:    %dx%d\n", mm.ImageSize.Width, mm.ImageSize.Height)
	}
	if mm.MaxTextLength > 0 {
		fmt.Printf("Max Text Len:  %d tokens\n", mm.MaxTextLength)
	}
	if mm.Framework != "" {
		fmt.Printf("Framework:     %s\n", mm.Framework)
	}
	// Download commands are displayed through unified CLI commands section
}

func printONNXAnalysis(info *smartdl.RepoInfo) {
	if info.ONNX == nil {
		printGenericAnalysis(info)
		return
	}

	onnx := info.ONNX
	if onnx.Optimized {
		fmt.Println("Optimized:  Yes (optimized models available)")
	}
	if onnx.Quantized {
		fmt.Println("Quantized:  Yes (quantized models available)")
	}
	if len(onnx.Runtimes) > 0 {
		fmt.Printf("Runtimes:   %s\n", strings.Join(onnx.Runtimes, ", "))
	}
	fmt.Println()

	fmt.Println("ONNX Models:")
	fmt.Printf("  %-40s  %12s  %s\n", "NAME", "SIZE", "VARIANT")
	fmt.Printf("  %-40s  %12s  %s\n", strings.Repeat("-", 40), "------------", "-------")

	for _, m := range onnx.Models {
		name := m.Name
		if len(name) > 40 {
			name = "..." + name[len(name)-37:]
		}
		variant := m.Variant
		if m.Optimized {
			variant += " (opt)"
		}
		fmt.Printf("  %-40s  %12s  %s\n", name, m.SizeHuman, variant)
	}
	// Download commands are displayed through unified CLI commands section
}
