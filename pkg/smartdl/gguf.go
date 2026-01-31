// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GGUF quantization quality ratings (1-5 stars)
var quantQuality = map[string]int{
	"Q2_K":   1,
	"Q2_K_S": 1,
	"IQ2_S":  1,
	"IQ2_XS": 1,
	"IQ2_XXS": 1,
	"Q3_K_S": 2,
	"Q3_K_M": 2,
	"Q3_K_L": 2,
	"IQ3_S":  2,
	"IQ3_XS": 2,
	"IQ3_M":  2,
	"Q4_0":   3,
	"Q4_1":   3,
	"Q4_K_S": 3,
	"Q4_K_M": 4,
	"IQ4_NL": 3,
	"IQ4_XS": 3,
	"Q5_0":   4,
	"Q5_1":   4,
	"Q5_K_S": 4,
	"Q5_K_M": 5,
	"Q6_K":   5,
	"Q8_0":   5,
	"F16":    5,
	"F32":    5,
	"BF16":   5,
}

// quantDescriptions provides human-readable descriptions for quantization levels.
var quantDescriptions = map[string]string{
	"Q2_K":   "Smallest, significant quality loss",
	"Q2_K_S": "Smallest, significant quality loss",
	"IQ2_S":  "Importance matrix 2-bit, small",
	"IQ2_XS": "Importance matrix 2-bit, extra small",
	"IQ2_XXS": "Importance matrix 2-bit, extra extra small",
	"Q3_K_S": "Very small, noticeable quality loss",
	"Q3_K_M": "Small, noticeable quality loss",
	"Q3_K_L": "Small, noticeable quality loss",
	"IQ3_S":  "Importance matrix 3-bit, small",
	"IQ3_XS": "Importance matrix 3-bit, extra small",
	"IQ3_M":  "Importance matrix 3-bit, medium",
	"Q4_0":   "Legacy 4-bit, good balance",
	"Q4_1":   "Legacy 4-bit with scales",
	"Q4_K_S": "Small 4-bit, good quality",
	"Q4_K_M": "Medium 4-bit, recommended",
	"IQ4_NL": "Importance matrix 4-bit, non-linear",
	"IQ4_XS": "Importance matrix 4-bit, extra small",
	"Q5_0":   "Legacy 5-bit, very good quality",
	"Q5_1":   "Legacy 5-bit with scales",
	"Q5_K_S": "Small 5-bit, excellent quality",
	"Q5_K_M": "Medium 5-bit, excellent quality",
	"Q6_K":   "6-bit, near-lossless",
	"Q8_0":   "8-bit, minimal loss",
	"F16":    "Half precision, full quality",
	"F32":    "Full precision, original quality",
	"BF16":   "Brain float 16, full quality",
}

// Regex patterns for parsing GGUF filenames
var (
	// Match quantization type: Q4_K_M, IQ2_XS, F16, etc.
	quantPattern = regexp.MustCompile(`(?i)(IQ[234]_(?:XXS|XS|S|M|NL)|Q[2-8]_[01KL](?:_[SML])?|Q[2-8]_K(?:_[SML])?|F(?:16|32)|BF16)`)

	// Match parameter count: 7B, 13B, 70B, 1.5B, etc.
	paramPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)[Bb]`)

	// Match model name from filename (before quant type)
	modelNamePattern = regexp.MustCompile(`^(.+?)[-._](?:IQ|Q|F|BF)\d`)
)

// analyzeGGUF analyzes GGUF files and extracts quantization information.
func analyzeGGUF(files []FileInfo) *GGUFInfo {
	info := &GGUFInfo{}

	// Find all GGUF files
	var ggufFiles []FileInfo
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.Name), ".gguf") {
			ggufFiles = append(ggufFiles, f)
		}
	}

	if len(ggufFiles) == 0 {
		return nil
	}

	// Extract model name and parameter count from first file
	if len(ggufFiles) > 0 {
		firstFile := ggufFiles[0].Name

		// Extract model name
		if matches := modelNamePattern.FindStringSubmatch(firstFile); len(matches) > 1 {
			info.ModelName = strings.ReplaceAll(matches[1], "-", " ")
			info.ModelName = strings.ReplaceAll(info.ModelName, "_", " ")
		}

		// Extract parameter count
		if matches := paramPattern.FindStringSubmatch(firstFile); len(matches) > 1 {
			info.ParameterCount = matches[1] + "B"
		}
	}

	// Parse each GGUF file
	for _, f := range ggufFiles {
		quant := parseGGUFQuantization(f)
		if quant != nil {
			info.Quantizations = append(info.Quantizations, *quant)
		}
	}

	// Sort by quality (descending) then by size (ascending)
	sort.Slice(info.Quantizations, func(i, j int) bool {
		if info.Quantizations[i].Quality != info.Quantizations[j].Quality {
			return info.Quantizations[i].Quality > info.Quantizations[j].Quality
		}
		return info.Quantizations[i].File.Size < info.Quantizations[j].File.Size
	})

	return info
}

// parseGGUFQuantization extracts quantization info from a GGUF file.
func parseGGUFQuantization(f FileInfo) *GGUFQuantization {
	name := strings.ToUpper(filepath.Base(f.Name))

	// Find quantization type
	matches := quantPattern.FindStringSubmatch(name)
	if len(matches) < 2 {
		// No recognized quantization, might be a split file or unknown format
		ram := estimateRAM(f.Size)
		return &GGUFQuantization{
			Name:             "Unknown",
			File:             f,
			Quality:          3,
			QualityStars:     qualityToStars(3),
			EstimatedRAM:     ram,
			EstimatedRAMHuman: humanSize(ram),
			Description:      "Unknown quantization format",
		}
	}

	quantType := strings.ToUpper(matches[1])
	quality := quantQuality[quantType]
	if quality == 0 {
		quality = 3 // Default to medium if not found
	}

	desc := quantDescriptions[quantType]
	if desc == "" {
		desc = "Quantized model"
	}

	ram := estimateRAM(f.Size)
	return &GGUFQuantization{
		Name:             quantType,
		File:             f,
		Quality:          quality,
		QualityStars:     qualityToStars(quality),
		EstimatedRAM:     ram,
		EstimatedRAMHuman: humanSize(ram),
		Description:      desc,
	}
}

// qualityToStars converts a 1-5 quality rating to star representation.
func qualityToStars(quality int) string {
	filled := quality
	empty := 5 - quality
	return strings.Repeat("★", filled) + strings.Repeat("☆", empty)
}

// estimateRAM estimates RAM usage for a GGUF file.
// Formula: file_size * 1.1 + 500MB overhead
func estimateRAM(fileSize int64) int64 {
	const overhead = 500 * 1024 * 1024 // 500 MiB
	return int64(float64(fileSize)*1.1) + overhead
}

// RecommendGGUF recommends quantizations based on available RAM.
func RecommendGGUF(info *GGUFInfo, availableRAM int64) []GGUFQuantization {
	var recommended []GGUFQuantization

	for _, q := range info.Quantizations {
		if q.EstimatedRAM <= availableRAM {
			q.Recommended = true
			recommended = append(recommended, q)
		}
	}

	// Sort by quality (best that fits in RAM first)
	sort.Slice(recommended, func(i, j int) bool {
		if recommended[i].Quality != recommended[j].Quality {
			return recommended[i].Quality > recommended[j].Quality
		}
		return recommended[i].File.Size < recommended[j].File.Size
	})

	return recommended
}

// GGUFToSelectableItems converts GGUF quantizations to SelectableItems.
// This provides a unified interface for the web UI and CLI.
func GGUFToSelectableItems(info *GGUFInfo) []SelectableItem {
	if info == nil || len(info.Quantizations) == 0 {
		return nil
	}

	items := make([]SelectableItem, 0, len(info.Quantizations))

	// Track if we have a Q4_K_M (common recommended default)
	hasQ4KM := false
	for _, q := range info.Quantizations {
		if q.Name == "Q4_K_M" {
			hasQ4KM = true
			break
		}
	}

	for _, q := range info.Quantizations {
		// Determine if this should be recommended
		// Q4_K_M is a good default, otherwise highest quality in 4-bit range
		recommended := false
		if hasQ4KM && q.Name == "Q4_K_M" {
			recommended = true
		} else if !hasQ4KM && q.Quality >= 4 && q.File.Size < 10*1024*1024*1024 { // < 10 GiB
			recommended = true
		}

		item := SelectableItem{
			ID:           strings.ToLower(q.Name),
			Label:        q.Name,
			Description:  q.Description,
			Size:         q.File.Size,
			SizeHuman:    q.File.SizeHuman,
			Quality:      q.Quality,
			QualityStars: q.QualityStars,
			Recommended:  recommended,
			Category:     "quantization",
			FilterValue:  strings.ToLower(q.Name),
			Files:        []string{q.File.Path},
			RAM:          q.EstimatedRAM,
			RAMHuman:     q.EstimatedRAMHuman,
		}
		items = append(items, item)
	}

	return items
}
