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

// analyzeDataset analyzes a dataset repository.
func analyzeDataset(files []FileInfo) *DatasetInfo {
	info := &DatasetInfo{}

	// Collect splits and formats
	splitMap := make(map[string]*DatasetSplit)
	formatSet := make(map[string]bool)
	configMap := make(map[string]*DatasetConfig)

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
		if name, dir := detectConfigDir(f.Path); name != "" {
			c := configMap[dir]
			if c == nil {
				c = &DatasetConfig{Name: name, Path: dir}
				configMap[dir] = c
			}
			c.FileCount++
			c.Size += f.Size
			if split != "default" && !containsString(c.Splits, split) {
				c.Splits = append(c.Splits, split)
			}
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
	for _, c := range configMap {
		c.SizeHuman = humanSize(c.Size)
		info.ConfigDetails = append(info.ConfigDetails, *c)
	}
	sort.Slice(info.ConfigDetails, func(i, j int) bool { return info.ConfigDetails[i].Path < info.ConfigDetails[j].Path })
	for _, c := range info.ConfigDetails {
		info.Configs = append(info.Configs, c.Name)
	}

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

// standardSplits are the common split names, in display priority order.
var standardSplits = []string{"train", "test", "validation", "dev", "eval"}

// splitNames are dataset split names recognized as path segments.
var splitNames = map[string]bool{"train": true, "test": true, "validation": true, "valid": true, "val": true, "dev": true, "eval": true}

// splitSegments splits a lowercased path the way --exact filters do.
func splitSegments(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' || r == '.' || r == ' ' })
}

// detectSplit returns the split a data file belongs to: a path segment that
// is a split name ("c4-train.00000-of-01024.json.gz", "clean/train.360/x",
// "data/validation-0000.parquet") or a split name with a suffix
// ("validation_matched"). File-name segments win over folder segments.
// Segments are the same as --exact matching uses, so the split name selects
// exactly these files.
func detectSplit(path, name string) string {
	check := func(segs []string) string {
		for _, seg := range segs {
			if splitNames[seg] {
				return seg
			}
			if i := strings.Index(seg, "_"); i > 0 && splitNames[seg[:i]] {
				return seg
			}
		}
		return ""
	}
	if s := check(splitSegments(strings.ToLower(name))); s != "" {
		return s
	}
	return check(splitSegments(strings.ToLower(filepath.ToSlash(filepath.Dir(path)))))
}

// detectConfig returns the dataset config (subset) a file belongs to, or "".
func detectConfig(path string) string {
	name, _ := detectConfigDir(path)
	return name
}

// detectConfigDir returns a file's config name and the folder holding the
// config's files: the first folder ("cola/train-0000.parquet" → cola), or
// the folder under data/ ("data/en/x.json" → en, data/en). Folders named
// like a split are not configs.
func detectConfigDir(path string) (name, dir string) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 2 {
		return "", ""
	}
	if parts[0] == "data" {
		if len(parts) < 3 {
			return "", ""
		}
		name, dir = parts[1], "data/"+parts[1]
	} else {
		name, dir = parts[0], parts[0]
	}
	if splitNames[strings.ToLower(name)] {
		return "", ""
	}
	return name, dir
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

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// DatasetToSelectableItems converts dataset configs and splits to
// SelectableItems. Filters combine with OR, so a config item selects all of
// that config's splits and a split item selects that split in every config.
// The catch-all "default" split (files with no recognizable split) is not
// offered: no filter selects exactly those files. When configs exist one is
// recommended ("default" if present, else the first) rather than "train",
// which would pull the train split of every config (allenai/c4: terabytes).
func DatasetToSelectableItems(info *DatasetInfo) []SelectableItem {
	if info == nil || (len(info.Splits) == 0 && len(info.ConfigDetails) == 0) {
		return nil
	}

	var items []SelectableItem

	// Recommend "default", else the first config with a train split (glue's
	// first config alphabetically, "ax", is a test-only diagnostic set).
	recommendedConfig := ""
	if len(info.ConfigDetails) > 1 {
		recommendedConfig = info.ConfigDetails[0].Path
		withTrain := ""
		for _, c := range info.ConfigDetails {
			if strings.EqualFold(c.Name, "default") {
				withTrain = c.Path
				break
			}
			if withTrain == "" && containsString(c.Splits, "train") {
				withTrain = c.Path
			}
		}
		if withTrain != "" {
			recommendedConfig = withTrain
		}
	}
	if len(info.ConfigDetails) > 1 {
		for _, c := range info.ConfigDetails {
			items = append(items, SelectableItem{
				ID:          "config:" + c.Path,
				Label:       c.Name,
				Description: fmt.Sprintf("Dataset config (%d files)", c.FileCount),
				Size:        c.Size,
				SizeHuman:   c.SizeHuman,
				Recommended: c.Path == recommendedConfig,
				Category:    "config",
				FilterValue: c.Path + "/",
			})
		}
	}

	// Add splits
	splitDescriptions := map[string]string{
		"train":      "Primary training data",
		"validation": "Validation/evaluation set",
		"valid":      "Validation/evaluation set",
		"val":        "Validation/evaluation set",
		"dev":        "Development set",
		"test":       "Held-out test set",
		"eval":       "Evaluation set",
	}

	for _, split := range info.Splits {
		if split.Name == "default" {
			continue
		}
		desc := splitDescriptions[split.Name]
		if desc == "" {
			desc = "Dataset split"
		}

		// Add file count to description
		if split.FileCount > 0 {
			desc = fmt.Sprintf("%s (%d files)", desc, split.FileCount)
		}

		items = append(items, SelectableItem{
			ID:          split.Name,
			Label:       split.Name,
			Description: desc,
			Size:        split.Size,
			SizeHuman:   split.SizeHuman,
			Recommended: recommendedConfig == "" && split.Name == "train",
			Category:    "split",
			FilterValue: split.Name,
		})
	}

	return items
}
