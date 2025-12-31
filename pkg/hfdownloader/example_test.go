// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader_test

import (
	"context"
	"fmt"
	"os"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func ExampleDownload() {
	job := hfdownloader.Job{
		Repo:     "hf-internal-testing/tiny-random-gpt2",
		Revision: "main",
	}

	cfg := hfdownloader.Settings{
		OutputDir:          "./example_output",
		Concurrency:        4,
		MaxActiveDownloads: 2,
	}

	// Progress callback
	progress := func(e hfdownloader.ProgressEvent) {
		switch e.Event {
		case "scan_start":
			fmt.Println("Scanning repository...")
		case "file_done":
			fmt.Printf("Downloaded: %s\n", e.Path)
		case "done":
			fmt.Println("Complete!")
		}
	}

	ctx := context.Background()
	err := hfdownloader.Download(ctx, job, cfg, progress)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Cleanup
	os.RemoveAll("./example_output")
}

func ExampleDownload_withFilters() {
	// Download only specific quantizations
	job := hfdownloader.Job{
		Repo:    "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
		Filters: []string{"q4_k_m", "q5_k_m"}, // Case-insensitive matching
	}

	cfg := hfdownloader.Settings{
		OutputDir: "./Models",
	}

	err := hfdownloader.Download(context.Background(), job, cfg, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func ExampleDownload_dataset() {
	// Download a dataset instead of a model
	job := hfdownloader.Job{
		Repo:      "facebook/flores",
		IsDataset: true,
	}

	cfg := hfdownloader.Settings{
		OutputDir: "./Datasets",
	}

	err := hfdownloader.Download(context.Background(), job, cfg, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func ExamplePlanRepo() {
	job := hfdownloader.Job{
		Repo: "hf-internal-testing/tiny-random-gpt2",
	}

	cfg := hfdownloader.Settings{}

	plan, err := hfdownloader.PlanRepo(context.Background(), job, cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %d files:\n", len(plan.Items))
	for _, item := range plan.Items {
		lfsTag := ""
		if item.LFS {
			lfsTag = " [LFS]"
		}
		fmt.Printf("  %s (%d bytes)%s\n", item.RelativePath, item.Size, lfsTag)
	}
}

func ExampleIsValidModelName() {
	// Valid names
	fmt.Println(hfdownloader.IsValidModelName("TheBloke/Mistral-7B-GGUF"))     // true
	fmt.Println(hfdownloader.IsValidModelName("facebook/opt-1.3b"))            // true
	fmt.Println(hfdownloader.IsValidModelName("hf-internal-testing/tiny-gpt")) // true

	// Invalid names
	fmt.Println(hfdownloader.IsValidModelName("Mistral-7B-GGUF")) // false (no owner)
	fmt.Println(hfdownloader.IsValidModelName(""))                // false (empty)
	fmt.Println(hfdownloader.IsValidModelName("/model"))          // false (empty owner)

	// Output:
	// true
	// true
	// true
	// false
	// false
	// false
}

func ExampleJob_filterSubdirs() {
	// Organize downloaded files by filter match
	job := hfdownloader.Job{
		Repo:               "TheBloke/Llama-2-7B-GGUF",
		Filters:            []string{"q4_0", "q5_0"},
		AppendFilterSubdir: true, // Creates separate subdirectories
	}

	// This will create:
	// ./Models/TheBloke/Llama-2-7B-GGUF/q4_0/llama-2-7b.Q4_0.gguf
	// ./Models/TheBloke/Llama-2-7B-GGUF/q5_0/llama-2-7b.Q5_0.gguf

	cfg := hfdownloader.Settings{
		OutputDir: "./Models",
	}

	_ = hfdownloader.Download(context.Background(), job, cfg, nil)
}

func ExampleSettings_withAuth() {
	// For private or gated repositories
	cfg := hfdownloader.Settings{
		OutputDir: "./Models",
		Token:     os.Getenv("HF_TOKEN"), // Use environment variable
	}

	job := hfdownloader.Job{
		Repo: "meta-llama/Llama-2-7b", // Requires authentication
	}

	err := hfdownloader.Download(context.Background(), job, cfg, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func ExampleSettings_performance() {
	// High-performance settings for fast networks
	cfg := hfdownloader.Settings{
		OutputDir:          "./Models",
		Concurrency:        16,                // 16 parallel connections per file
		MaxActiveDownloads: 4,                 // 4 files at once
		MultipartThreshold: "16MiB",           // Use multipart for files >= 16MiB
		Retries:            6,                 // More retries for unstable connections
		BackoffInitial:     "200ms",           // Faster retry
		BackoffMax:         "30s",             // Longer max for rate limiting
		Verify:             "sha256",          // Full verification
	}

	_ = cfg // Use in Download()
}

