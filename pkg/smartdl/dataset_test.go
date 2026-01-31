// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"strings"
	"testing"
)

func TestAnalyzeDataset(t *testing.T) {
	t.Run("basic dataset with splits", func(t *testing.T) {
		files := []FileInfo{
			{Name: "train-00000.parquet", Path: "data/train-00000.parquet", Size: 1024 * 1024},
			{Name: "train-00001.parquet", Path: "data/train-00001.parquet", Size: 1024 * 1024},
			{Name: "test-00000.parquet", Path: "data/test-00000.parquet", Size: 512 * 1024},
			{Name: "validation.parquet", Path: "data/validation.parquet", Size: 256 * 1024},
		}

		info := analyzeDataset(files)
		if info == nil {
			t.Fatal("analyzeDataset returned nil")
		}

		if len(info.Splits) != 3 {
			t.Errorf("Splits length = %d, want 3", len(info.Splits))
		}

		// Check primary format
		if info.PrimaryFormat != "parquet" {
			t.Errorf("PrimaryFormat = %q, want parquet", info.PrimaryFormat)
		}
	})

	t.Run("split ordering", func(t *testing.T) {
		files := []FileInfo{
			{Name: "test.parquet", Path: "test.parquet", Size: 1024},
			{Name: "validation.parquet", Path: "validation.parquet", Size: 1024},
			{Name: "train.parquet", Path: "train.parquet", Size: 1024},
		}

		info := analyzeDataset(files)
		if len(info.Splits) != 3 {
			t.Fatalf("Splits length = %d, want 3", len(info.Splits))
		}

		// Train should be first due to sorting by priority
		if info.Splits[0].Name != "train" {
			t.Errorf("first split = %q, want train", info.Splits[0].Name)
		}
		if info.Splits[1].Name != "validation" {
			t.Errorf("second split = %q, want validation", info.Splits[1].Name)
		}
		if info.Splits[2].Name != "test" {
			t.Errorf("third split = %q, want test", info.Splits[2].Name)
		}
	})

	t.Run("multiple formats", func(t *testing.T) {
		files := []FileInfo{
			{Name: "train.parquet", Path: "train.parquet", Size: 1024},
			{Name: "train.json", Path: "train.json", Size: 1024},
			{Name: "train.csv", Path: "train.csv", Size: 1024},
		}

		info := analyzeDataset(files)
		if len(info.Formats) != 3 {
			t.Errorf("Formats length = %d, want 3", len(info.Formats))
		}
		// Parquet should be primary
		if info.PrimaryFormat != "parquet" {
			t.Errorf("PrimaryFormat = %q, want parquet", info.PrimaryFormat)
		}
	})

	t.Run("dataset with configs", func(t *testing.T) {
		files := []FileInfo{
			{Name: "train.parquet", Path: "data/en/train.parquet", Size: 1024},
			{Name: "train.parquet", Path: "data/fr/train.parquet", Size: 1024},
			{Name: "train.parquet", Path: "data/de/train.parquet", Size: 1024},
		}

		info := analyzeDataset(files)
		if len(info.Configs) != 3 {
			t.Errorf("Configs length = %d, want 3 (en, fr, de)", len(info.Configs))
		}
	})

	t.Run("empty dataset", func(t *testing.T) {
		files := []FileInfo{}
		info := analyzeDataset(files)
		if len(info.Splits) != 0 {
			t.Errorf("Splits should be empty, got %d", len(info.Splits))
		}
	})

	t.Run("no data files", func(t *testing.T) {
		files := []FileInfo{
			{Name: "README.md", Path: "README.md", Size: 1024},
			{Name: "config.yaml", Path: "config.yaml", Size: 512},
			{Name: "model.safetensors", Path: "model.safetensors", Size: 1024},
		}

		info := analyzeDataset(files)
		if len(info.Splits) != 0 {
			t.Errorf("Splits should be empty for non-data files, got %d", len(info.Splits))
		}
	})

	t.Run("tar.gz handling", func(t *testing.T) {
		files := []FileInfo{
			{Name: "train-00000.tar.gz", Path: "train-00000.tar.gz", Size: 1024 * 1024},
		}

		info := analyzeDataset(files)
		if len(info.Splits) != 1 {
			t.Fatalf("Splits length = %d, want 1", len(info.Splits))
		}

		hasGz := false
		for _, f := range info.Formats {
			if f == "tar.gz" {
				hasGz = true
			}
		}
		if !hasGz {
			t.Error("should detect tar.gz format")
		}
	})

	t.Run("size aggregation per split", func(t *testing.T) {
		files := []FileInfo{
			{Name: "train-00000.parquet", Path: "train-00000.parquet", Size: 1000},
			{Name: "train-00001.parquet", Path: "train-00001.parquet", Size: 2000},
			{Name: "train-00002.parquet", Path: "train-00002.parquet", Size: 3000},
		}

		info := analyzeDataset(files)
		if len(info.Splits) != 1 {
			t.Fatalf("Splits length = %d, want 1", len(info.Splits))
		}

		if info.Splits[0].Size != 6000 {
			t.Errorf("Split size = %d, want 6000", info.Splits[0].Size)
		}
		if info.Splits[0].FileCount != 3 {
			t.Errorf("FileCount = %d, want 3", info.Splits[0].FileCount)
		}
	})
}

func TestIsDataFileExtension(t *testing.T) {
	validExts := []string{
		"parquet", "arrow", "json", "jsonl", "csv", "tsv",
		"txt", "tar", "tar.gz", "zip", "gz", "zst",
	}

	for _, ext := range validExts {
		t.Run(ext+"_valid", func(t *testing.T) {
			if !isDataFileExtension(ext) {
				t.Errorf("%q should be a valid data file extension", ext)
			}
		})
	}

	invalidExts := []string{"md", "py", "safetensors", "bin", "yaml", "toml", ""}

	for _, ext := range invalidExts {
		t.Run(ext+"_invalid", func(t *testing.T) {
			if isDataFileExtension(ext) {
				t.Errorf("%q should not be a valid data file extension", ext)
			}
		})
	}
}

func TestDetectSplit(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		filename string
		expected string
	}{
		{"train in path", "train/data.parquet", "data.parquet", "train"},
		{"test in path", "test/data.parquet", "data.parquet", "test"},
		{"validation in path", "validation/data.parquet", "data.parquet", "validation"},
		{"dev in path", "dev/data.parquet", "data.parquet", "dev"},
		{"eval in path", "eval/data.parquet", "data.parquet", "eval"},
		{"train prefix in filename", "data/train-00000.parquet", "train-00000.parquet", "train"},
		{"test prefix in filename", "data/test-00000.parquet", "test-00000.parquet", "test"},
		{"train underscore", "data/train_data.parquet", "train_data.parquet", "train"},
		{"train dot prefix", "data/train.parquet", "train.parquet", "train"},
		{"data prefix with split name in filename", "data/english/train.parquet", "train.parquet", "train"},
		{"no split detected", "other/data.parquet", "data.parquet", ""},
		{"path starts with train/", "train/shard-0000.parquet", "shard-0000.parquet", "train"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectSplit(tt.path, tt.filename)
			if result != tt.expected {
				t.Errorf("detectSplit(%q, %q) = %q, want %q", tt.path, tt.filename, result, tt.expected)
			}
		})
	}
}

func TestDetectConfig(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"data with config", "data/en/train.parquet", "en"},
		{"data with config fr", "data/fr/test.parquet", "fr"},
		{"split path not config", "data/train/file.parquet", ""},
		{"config at root", "english/train.parquet", "english"},
		{"no config", "train.parquet", ""},
		{"train at root not config", "train/file.parquet", ""},
		{"test at root not config", "test/file.parquet", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectConfig(tt.path)
			if result != tt.expected {
				t.Errorf("detectConfig(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestSplitPriority(t *testing.T) {
	tests := []struct {
		split    string
		priority int
	}{
		{"train", 0},
		{"validation", 1},
		{"dev", 2},
		{"test", 3},
		{"eval", 4},
		{"default", 5},
		{"unknown", 100},
		{"custom", 100},
	}

	for _, tt := range tests {
		t.Run(tt.split, func(t *testing.T) {
			result := splitPriority(tt.split)
			if result != tt.priority {
				t.Errorf("splitPriority(%q) = %d, want %d", tt.split, result, tt.priority)
			}
		})
	}

	// Verify ordering
	t.Run("train before validation", func(t *testing.T) {
		if splitPriority("train") >= splitPriority("validation") {
			t.Error("train should come before validation")
		}
	})

	t.Run("test before unknown", func(t *testing.T) {
		if splitPriority("test") >= splitPriority("unknown") {
			t.Error("test should come before unknown")
		}
	})
}

func TestSelectPrimaryFormat(t *testing.T) {
	tests := []struct {
		name     string
		formats  []string
		expected string
	}{
		{"parquet preferred", []string{"json", "parquet", "csv"}, "parquet"},
		{"arrow second", []string{"json", "arrow", "csv"}, "arrow"},
		{"jsonl third", []string{"csv", "jsonl"}, "jsonl"},
		{"json over csv", []string{"csv", "json"}, "json"},
		{"csv over txt", []string{"txt", "csv"}, "csv"},
		{"first if unknown", []string{"custom", "other"}, "custom"},
		{"empty list", []string{}, ""},
		{"single format", []string{"parquet"}, "parquet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectPrimaryFormat(tt.formats)
			if result != tt.expected {
				t.Errorf("selectPrimaryFormat(%v) = %q, want %q", tt.formats, result, tt.expected)
			}
		})
	}
}

func TestGetSplitByName(t *testing.T) {
	info := &DatasetInfo{
		Splits: []DatasetSplit{
			{Name: "train", Size: 1000},
			{Name: "test", Size: 500},
			{Name: "validation", Size: 200},
		},
	}

	t.Run("find existing split", func(t *testing.T) {
		split := GetSplitByName(info, "train")
		if split == nil {
			t.Fatal("expected to find train split")
		}
		if split.Size != 1000 {
			t.Errorf("Size = %d, want 1000", split.Size)
		}
	})

	t.Run("find another split", func(t *testing.T) {
		split := GetSplitByName(info, "test")
		if split == nil {
			t.Fatal("expected to find test split")
		}
		if split.Size != 500 {
			t.Errorf("Size = %d, want 500", split.Size)
		}
	})

	t.Run("not found", func(t *testing.T) {
		split := GetSplitByName(info, "unknown")
		if split != nil {
			t.Error("should return nil for unknown split")
		}
	})

	t.Run("empty info", func(t *testing.T) {
		emptyInfo := &DatasetInfo{}
		split := GetSplitByName(emptyInfo, "train")
		if split != nil {
			t.Error("should return nil for empty info")
		}
	})
}

func TestCalculateSelectedSize(t *testing.T) {
	info := &DatasetInfo{
		Splits: []DatasetSplit{
			{Name: "train", Size: 1000},
			{Name: "test", Size: 500},
			{Name: "validation", Size: 200},
		},
	}

	t.Run("all splits when empty selection", func(t *testing.T) {
		size := CalculateSelectedSize(info, []string{})
		if size != 1700 {
			t.Errorf("Size = %d, want 1700", size)
		}
	})

	t.Run("nil selection same as all", func(t *testing.T) {
		size := CalculateSelectedSize(info, nil)
		if size != 1700 {
			t.Errorf("Size = %d, want 1700", size)
		}
	})

	t.Run("single selection", func(t *testing.T) {
		size := CalculateSelectedSize(info, []string{"train"})
		if size != 1000 {
			t.Errorf("Size = %d, want 1000", size)
		}
	})

	t.Run("multiple selections", func(t *testing.T) {
		size := CalculateSelectedSize(info, []string{"train", "test"})
		if size != 1500 {
			t.Errorf("Size = %d, want 1500", size)
		}
	})

	t.Run("selection with unknown split", func(t *testing.T) {
		size := CalculateSelectedSize(info, []string{"train", "unknown"})
		if size != 1000 {
			t.Errorf("Size = %d, want 1000 (unknown should be ignored)", size)
		}
	})

	t.Run("all selected explicitly", func(t *testing.T) {
		size := CalculateSelectedSize(info, []string{"train", "test", "validation"})
		if size != 1700 {
			t.Errorf("Size = %d, want 1700", size)
		}
	})
}

func TestGetFormatDescription(t *testing.T) {
	tests := []struct {
		format      string
		shouldMatch string
	}{
		{"parquet", "Parquet"},
		{"arrow", "Arrow"},
		{"json", "JSON"},
		{"jsonl", "JSON"},
		{"csv", "Comma"},
		{"txt", "Plain"},
		{"tar", "WebDataset"},
		{"tar.gz", "WebDataset"},
		{"zip", "Compressed"},
		{"unknown", "Data format"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			desc := GetFormatDescription(tt.format)
			if desc == "" {
				t.Error("description should not be empty")
			}
			// Check that known formats have specific descriptions
			if tt.format != "unknown" && desc == "Data format" {
				t.Errorf("expected specific description for %q, got generic", tt.format)
			}
		})
	}
}

func TestHasMultipleConfigs(t *testing.T) {
	t.Run("no configs", func(t *testing.T) {
		info := &DatasetInfo{Configs: []string{}}
		if HasMultipleConfigs(info) {
			t.Error("should return false for no configs")
		}
	})

	t.Run("single config", func(t *testing.T) {
		info := &DatasetInfo{Configs: []string{"default"}}
		if HasMultipleConfigs(info) {
			t.Error("should return false for single config")
		}
	})

	t.Run("multiple configs", func(t *testing.T) {
		info := &DatasetInfo{Configs: []string{"en", "fr", "de"}}
		if !HasMultipleConfigs(info) {
			t.Error("should return true for multiple configs")
		}
	})
}

func TestHasMultipleFormats(t *testing.T) {
	t.Run("no formats", func(t *testing.T) {
		info := &DatasetInfo{Formats: []string{}}
		if HasMultipleFormats(info) {
			t.Error("should return false for no formats")
		}
	})

	t.Run("single format", func(t *testing.T) {
		info := &DatasetInfo{Formats: []string{"parquet"}}
		if HasMultipleFormats(info) {
			t.Error("should return false for single format")
		}
	})

	t.Run("multiple formats", func(t *testing.T) {
		info := &DatasetInfo{Formats: []string{"parquet", "json", "csv"}}
		if !HasMultipleFormats(info) {
			t.Error("should return true for multiple formats")
		}
	})
}

func TestDatasetToSelectableItems(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		items := DatasetToSelectableItems(nil)
		if items != nil {
			t.Error("expected nil for nil info")
		}
	})

	t.Run("empty splits", func(t *testing.T) {
		info := &DatasetInfo{Splits: []DatasetSplit{}}
		items := DatasetToSelectableItems(info)
		if items != nil {
			t.Error("expected nil for empty splits")
		}
	})

	t.Run("basic splits", func(t *testing.T) {
		info := &DatasetInfo{
			Splits: []DatasetSplit{
				{Name: "train", Size: 1000, FileCount: 5, SizeHuman: "1.0 KiB"},
				{Name: "test", Size: 500, FileCount: 2, SizeHuman: "500 B"},
			},
		}

		items := DatasetToSelectableItems(info)
		if len(items) != 2 {
			t.Fatalf("items length = %d, want 2", len(items))
		}

		// Train should be recommended
		trainItem := items[0]
		if trainItem.ID != "train" {
			t.Errorf("first item ID = %q, want train", trainItem.ID)
		}
		if !trainItem.Recommended {
			t.Error("train should be recommended")
		}
		if trainItem.Category != "split" {
			t.Errorf("Category = %q, want split", trainItem.Category)
		}

		// Test should not be recommended
		testItem := items[1]
		if testItem.Recommended {
			t.Error("test should not be recommended")
		}
	})

	t.Run("descriptions for known splits", func(t *testing.T) {
		splits := []string{"train", "validation", "dev", "test", "eval", "custom"}

		for _, split := range splits {
			info := &DatasetInfo{
				Splits: []DatasetSplit{
					{Name: split, Size: 1000, FileCount: 1},
				},
			}

			items := DatasetToSelectableItems(info)
			if len(items) != 1 {
				t.Fatalf("items length = %d for %q", len(items), split)
			}

			if items[0].Description == "" {
				t.Errorf("description should not be empty for %q", split)
			}
		}
	})

	t.Run("file count in description", func(t *testing.T) {
		info := &DatasetInfo{
			Splits: []DatasetSplit{
				{Name: "train", Size: 1000, FileCount: 10},
			},
		}

		items := DatasetToSelectableItems(info)
		if len(items) != 1 {
			t.Fatalf("items length = %d", len(items))
		}

		// Description should mention file count
		desc := items[0].Description
		if desc == "" {
			t.Error("description should not be empty")
		}
		// Description should contain "10 files" since FileCount is 10
		if !strings.Contains(desc, "10 files") {
			t.Errorf("description should mention file count, got: %q", desc)
		}
	})

	t.Run("filter values", func(t *testing.T) {
		info := &DatasetInfo{
			Splits: []DatasetSplit{
				{Name: "train", Size: 1000},
				{Name: "validation", Size: 500},
			},
		}

		items := DatasetToSelectableItems(info)
		for _, item := range items {
			if item.FilterValue == "" {
				t.Errorf("FilterValue should not be empty for %q", item.ID)
			}
			if item.FilterValue != item.ID {
				t.Errorf("FilterValue = %q, should match ID %q", item.FilterValue, item.ID)
			}
		}
	})

	t.Run("size fields populated", func(t *testing.T) {
		info := &DatasetInfo{
			Splits: []DatasetSplit{
				{Name: "train", Size: 1024 * 1024, SizeHuman: "1.0 MiB"},
			},
		}

		items := DatasetToSelectableItems(info)
		if items[0].Size != 1024*1024 {
			t.Errorf("Size = %d, want %d", items[0].Size, 1024*1024)
		}
		if items[0].SizeHuman != "1.0 MiB" {
			t.Errorf("SizeHuman = %q, want 1.0 MiB", items[0].SizeHuman)
		}
	})
}

func TestFormatDescriptions(t *testing.T) {
	expectedFormats := []string{"parquet", "arrow", "json", "jsonl", "csv", "txt", "tar", "tar.gz", "zip"}

	for _, format := range expectedFormats {
		t.Run(format, func(t *testing.T) {
			desc, ok := formatDescriptions[format]
			if !ok {
				t.Errorf("missing description for %q", format)
			}
			if desc == "" {
				t.Errorf("empty description for %q", format)
			}
		})
	}
}

func TestStandardSplits(t *testing.T) {
	expectedSplits := []string{"train", "test", "validation", "dev", "eval"}

	if len(standardSplits) != len(expectedSplits) {
		t.Errorf("standardSplits length = %d, want %d", len(standardSplits), len(expectedSplits))
	}

	for i, split := range expectedSplits {
		if standardSplits[i] != split {
			t.Errorf("standardSplits[%d] = %q, want %q", i, standardSplits[i], split)
		}
	}
}
