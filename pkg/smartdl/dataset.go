// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Known dataset file formats and their descriptions.
var formatDescriptions = map[string]string{
	"parquet": "Apache Parquet columnar format (recommended)",
	"arrow":   "Apache Arrow IPC format",
	"json":    "JSON Lines format",
	"jsonl":   "JSON Lines format",
	"csv":     "Comma-separated values",
	"txt":     "Plain text",
	"tar":     "WebDataset tar archives",
	"tar.gz":  "Compressed WebDataset archives",
	"zip":     "Compressed archive",
}

// Standard split names.
var standardSplits = []string{"train", "test", "validation", "dev", "eval"}

// analyzeDataset analyzes a dataset repository.
func analyzeDataset(files []FileInfo) *DatasetInfo {
	info := &DatasetInfo{}

	// Collect splits and formats
	splitMap := make(map[string]*DatasetSplit)
	formatSet := make(map[string]bool)
	configSet := make(map[string]bool)

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext == "" {
			continue
		}
		ext = strings.TrimPrefix(ext, ".")

		// Handle compound extensions
		if strings.HasSuffix(f.Name, ".tar.gz") {
			ext = "tar.gz"
		}

		// Check if this is a data file
		if !isDataFileExtension(ext) {
			continue
		}

		formatSet[ext] = true

		// Detect split from path or filename
		split := detectSplit(f.Path, f.Name)
		if split == "" {
			split = "default"
		}

		// Detect config/subset from path
		config := detectConfig(f.Path)
		if config != "" {
			configSet[config] = true
		}

		// Add to split
		if _, exists := splitMap[split]; !exists {
			splitMap[split] = &DatasetSplit{
				Name: split,
			}
		}
		splitMap[split].Files = append(splitMap[split].Files, f)
		splitMap[split].Size += f.Size
	}

	// Convert splits map to slice
	for _, split := range splitMap {
		split.SizeHuman = humanSize(split.Size)
		split.FileCount = len(split.Files)
		info.Splits = append(info.Splits, *split)
	}

	// Sort splits by standard order
	sort.Slice(info.Splits, func(i, j int) bool {
		return splitPriority(info.Splits[i].Name) < splitPriority(info.Splits[j].Name)
	})

	// Collect formats
	for format := range formatSet {
		info.Formats = append(info.Formats, format)
	}
	sort.Strings(info.Formats)

	// Collect configs
	for config := range configSet {
		info.Configs = append(info.Configs, config)
	}
	sort.Strings(info.Configs)

	// Set primary format (prefer parquet > arrow > json)
	info.PrimaryFormat = selectPrimaryFormat(info.Formats)

	return info
}

// isDataFileExtension checks if the extension indicates a data file.
func isDataFileExtension(ext string) bool {
	dataExts := map[string]bool{
		"parquet": true,
		"arrow":   true,
		"json":    true,
		"jsonl":   true,
		"csv":     true,
		"tsv":     true,
		"txt":     true,
		"tar":     true,
		"tar.gz":  true,
		"zip":     true,
		"gz":      true,
		"zst":     true,
	}
	return dataExts[ext]
}

// detectSplit extracts split name from file path or name.
func detectSplit(path, name string) string {
	pathLower := strings.ToLower(path)
	nameLower := strings.ToLower(name)

	// Check for standard splits in path
	for _, split := range standardSplits {
		if strings.Contains(pathLower, "/"+split+"/") ||
			strings.HasPrefix(pathLower, split+"/") ||
			strings.Contains(nameLower, split+"-") ||
			strings.Contains(nameLower, split+"_") ||
			strings.HasPrefix(nameLower, split+".") ||
			strings.HasPrefix(nameLower, split+"-") {
			return split
		}
	}

	// Check for data/ prefix (common pattern)
	if strings.HasPrefix(pathLower, "data/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			// Check if second part is a split name
			candidate := strings.ToLower(parts[1])
			for _, split := range standardSplits {
				if candidate == split {
					return split
				}
			}
			// Return as config/split hybrid
			return candidate
		}
	}

	return ""
}

// detectConfig extracts configuration/subset name from path.
func detectConfig(path string) string {
	// Common patterns: data/<config>/<split>/ or <config>/train/
	parts := strings.Split(path, "/")

	if len(parts) >= 2 {
		first := parts[0]

		// Skip common non-config prefixes
		if first == "data" && len(parts) >= 3 {
			candidate := parts[1]
			// Check if it's not a standard split
			for _, split := range standardSplits {
				if strings.ToLower(candidate) == split {
					return ""
				}
			}
			return candidate
		}

		// Check if first part is not a standard split and not "data"
		if first != "data" {
			for _, split := range standardSplits {
				if strings.ToLower(first) == split {
					return ""
				}
			}
			// Could be a config
			if len(parts) >= 2 {
				return first
			}
		}
	}

	return ""
}

// splitPriority returns ordering priority for splits.
func splitPriority(split string) int {
	priorities := map[string]int{
		"train":      0,
		"validation": 1,
		"dev":        2,
		"test":       3,
		"eval":       4,
		"default":    5,
	}
	if p, ok := priorities[split]; ok {
		return p
	}
	return 100 // Unknown splits go last
}

// selectPrimaryFormat selects the best format from available options.
func selectPrimaryFormat(formats []string) string {
	priorities := []string{"parquet", "arrow", "jsonl", "json", "csv", "txt"}

	for _, pref := range priorities {
		for _, fmt := range formats {
			if fmt == pref {
				return fmt
			}
		}
	}

	if len(formats) > 0 {
		return formats[0]
	}
	return ""
}

// GetSplitByName finds a split by name.
func GetSplitByName(info *DatasetInfo, name string) *DatasetSplit {
	for i := range info.Splits {
		if info.Splits[i].Name == name {
			return &info.Splits[i]
		}
	}
	return nil
}

// CalculateSelectedSize calculates total size for selected splits.
func CalculateSelectedSize(info *DatasetInfo, selectedSplits []string) int64 {
	if len(selectedSplits) == 0 {
		// No selection = all splits
		var total int64
		for _, split := range info.Splits {
			total += split.Size
		}
		return total
	}

	selected := make(map[string]bool)
	for _, s := range selectedSplits {
		selected[s] = true
	}

	var total int64
	for _, split := range info.Splits {
		if selected[split.Name] {
			total += split.Size
		}
	}
	return total
}

// GetFormatDescription returns description for a format.
func GetFormatDescription(format string) string {
	if desc, ok := formatDescriptions[format]; ok {
		return desc
	}
	return "Data format"
}

// HasMultipleConfigs checks if dataset has multiple configurations.
func HasMultipleConfigs(info *DatasetInfo) bool {
	return len(info.Configs) > 1
}

// HasMultipleFormats checks if dataset has multiple formats available.
func HasMultipleFormats(info *DatasetInfo) bool {
	return len(info.Formats) > 1
}

// DatasetToSelectableItems converts dataset splits to SelectableItems.
func DatasetToSelectableItems(info *DatasetInfo) []SelectableItem {
	if info == nil || len(info.Splits) == 0 {
		return nil
	}

	var items []SelectableItem

	// Add splits
	splitDescriptions := map[string]string{
		"train":      "Primary training data",
		"validation": "Validation/evaluation set",
		"dev":        "Development set",
		"test":       "Held-out test set",
		"eval":       "Evaluation set",
		"default":    "Default dataset split",
	}

	for _, split := range info.Splits {
		desc := splitDescriptions[split.Name]
		if desc == "" {
			desc = "Dataset split"
		}

		// Add file count to description
		if split.FileCount > 0 {
			desc = fmt.Sprintf("%s (%d files)", desc, split.FileCount)
		}

		item := SelectableItem{
			ID:          split.Name,
			Label:       strings.Title(split.Name),
			Description: desc,
			Size:        split.Size,
			SizeHuman:   split.SizeHuman,
			Recommended: split.Name == "train",
			Category:    "split",
			FilterValue: split.Name,
		}
		items = append(items, item)
	}

	return items
}
