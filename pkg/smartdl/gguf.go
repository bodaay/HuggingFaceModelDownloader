// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/filtermatch"
)

// GGUF quantization quality ratings (1-5 stars).
//
// Entries prefixed/suffixed with UD-style modifiers (e.g. Q4_K_XL) are unsloth's
// "Unsloth Dynamic" quants — same bit-budget family as the corresponding K
// variant but with improved layer-by-layer quantization, rated at or above the
// closest standard K_M equivalent.
var quantQuality = map[string]int{
	// 1-bit (unsloth dynamic only)
	"IQ1_S": 1,
	"IQ1_M": 1,

	// 2-bit
	"Q2_K":    1,
	"Q2_K_S":  1,
	"Q2_K_L":  1,
	"Q2_K_XL": 2, // unsloth dynamic, better than plain Q2_K
	"IQ2_S":   1,
	"IQ2_M":   1,
	"IQ2_XS":  1,
	"IQ2_XXS": 1,

	// 3-bit
	"Q3_K_S":  2,
	"Q3_K_M":  2,
	"Q3_K_L":  2,
	"Q3_K_XL": 3, // unsloth dynamic
	"IQ3_S":   2,
	"IQ3_XS":  2,
	"IQ3_XXS": 2,
	"IQ3_M":   2,

	// 4-bit
	"Q4_0":    3,
	"Q4_1":    3,
	"Q4_K_S":  3,
	"Q4_K_M":  4,
	"Q4_K_XL": 4, // unsloth dynamic, comparable to Q4_K_M
	"IQ4_NL":  3,
	"IQ4_XS":  3,

	// 5-bit
	"Q5_0":    4,
	"Q5_1":    4,
	"Q5_K_S":  4,
	"Q5_K_M":  5,
	"Q5_K_XL": 5, // unsloth dynamic

	// 6-bit
	"Q6_K":    5,
	"Q6_K_XL": 5, // unsloth dynamic

	// 8-bit
	"Q8_0":    5,
	"Q8_K_XL": 5, // unsloth dynamic

	// Ternary, ARM-repacked and microscaling formats
	"TQ1_0":     1,
	"TQ2_0":     1,
	"Q4_0_4_4":  3,
	"Q4_0_4_8":  3,
	"Q4_0_8_8":  3,
	"MXFP4":     3,
	"MXFP4_MOE": 3,
	// Full / half precision
	"F16":  5,
	"F32":  5,
	"BF16": 5,
}

// quantDescriptions provides human-readable descriptions for quantization levels.
var quantDescriptions = map[string]string{
	// 1-bit
	"IQ1_S": "Unsloth dynamic 1-bit, small",
	"IQ1_M": "Unsloth dynamic 1-bit, medium",

	// 2-bit
	"Q2_K":    "Smallest, significant quality loss",
	"Q2_K_S":  "Smallest, significant quality loss",
	"Q2_K_L":  "Small 2-bit, noticeable quality loss",
	"Q2_K_XL": "Unsloth dynamic 2-bit, improved quality",
	"IQ2_S":   "Importance matrix 2-bit, small",
	"IQ2_M":   "Importance matrix 2-bit, medium",
	"IQ2_XS":  "Importance matrix 2-bit, extra small",
	"IQ2_XXS": "Importance matrix 2-bit, extra extra small",

	// 3-bit
	"Q3_K_S":  "Very small, noticeable quality loss",
	"Q3_K_M":  "Small, noticeable quality loss",
	"Q3_K_L":  "Small, noticeable quality loss",
	"Q3_K_XL": "Unsloth dynamic 3-bit, improved quality",
	"IQ3_S":   "Importance matrix 3-bit, small",
	"IQ3_XS":  "Importance matrix 3-bit, extra small",
	"IQ3_XXS": "Importance matrix 3-bit, extra extra small",
	"IQ3_M":   "Importance matrix 3-bit, medium",

	// 4-bit
	"Q4_0":    "Legacy 4-bit, good balance",
	"Q4_1":    "Legacy 4-bit with scales",
	"Q4_K_S":  "Small 4-bit, good quality",
	"Q4_K_M":  "Medium 4-bit, recommended",
	"Q4_K_XL": "Unsloth dynamic 4-bit, recommended",
	"IQ4_NL":  "Importance matrix 4-bit, non-linear",
	"IQ4_XS":  "Importance matrix 4-bit, extra small",

	// 5-bit
	"Q5_0":    "Legacy 5-bit, very good quality",
	"Q5_1":    "Legacy 5-bit with scales",
	"Q5_K_S":  "Small 5-bit, excellent quality",
	"Q5_K_M":  "Medium 5-bit, excellent quality",
	"Q5_K_XL": "Unsloth dynamic 5-bit, excellent quality",

	// 6-bit
	"Q6_K":    "6-bit, near-lossless",
	"Q6_K_XL": "Unsloth dynamic 6-bit, near-lossless",

	// 8-bit
	"Q8_0":    "8-bit, minimal loss",
	"Q8_K_XL": "Unsloth dynamic 8-bit, minimal loss",

	// Ternary, ARM-repacked and microscaling formats
	"TQ1_0":     "Ternary ~1.7-bit, extreme compression",
	"TQ2_0":     "Ternary 2-bit, extreme compression",
	"Q4_0_4_4":  "4-bit repacked for ARM CPUs",
	"Q4_0_4_8":  "4-bit repacked for ARM CPUs (i8mm)",
	"Q4_0_8_8":  "4-bit repacked for ARM CPUs (SVE)",
	"MXFP4":     "Microscaling FP4",
	"MXFP4_MOE": "Microscaling FP4 for MoE experts",
	// Full / half precision
	"F16":  "Half precision, full quality",
	"F32":  "Full precision, original quality",
	"BF16": "Brain float 16, full quality",
}

// Regex patterns for parsing GGUF filenames.
//
// quantPattern captures the quantization type (group 1). Alternatives are
// ordered so the longest form wins (Q4_0_4_4 before Q4_0, XXL before XL
// before L). An unsloth "UD-" (dynamic) prefix is detected by quantLabel.
var (
	quantPattern = regexp.MustCompile(`(?i)(IQ[1-4]_(?:XXS|XS|S|M|NL)|TQ[12]_0|MXFP4(?:_MOE)?|Q[2-8]_(?:0_[48]_[48]|[01]|K(?:_(?:XXL|XL|L|M|S))?)|F(?:16|32)|BF16)`)

	// bitsPattern pulls a bit width out of non-standard labels such as
	// "Q3_LynnStyle" or "Q8-MTP" for a rough quality rating.
	bitsPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])I?Q([1-8])(?:[^0-9]|$)`)

	// Match parameter count: 7B, 13B, 70B, 1.5B, etc.
	paramPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)[Bb]`)

	// Match model name from filename (before quant type).
	modelNamePattern = regexp.MustCompile(`^(.+?)[-._](?:UD[-_])?(?:IQ|TQ|Q|F|BF|MXFP)\d`)
)

// quantLabel finds the quantization type in s and returns it with any
// unsloth "UD-" prefix kept in the label ("UD-Q4_K_XL", "Q4_K_XL").
func quantLabel(s string) (label, quantType string, ok bool) {
	m := quantPattern.FindStringSubmatchIndex(s)
	if m == nil {
		return "", "", false
	}
	quantType = strings.ToUpper(s[m[2]:m[3]])
	label = quantType
	if m[2] >= 3 && strings.EqualFold(s[m[2]-3:m[2]], "UD-") {
		label = "UD-" + quantType
	}
	return label, quantType, true
}

// qualityByBits rates labels with no known quant type by their bit width.
var qualityByBits = map[string]int{"1": 1, "2": 1, "3": 2, "4": 3, "5": 4, "6": 5, "8": 5}

// isMMProjFile reports whether a GGUF filename is a multimodal projector
// (vision encoder) file. These files live alongside LLM quantizations in
// multimodal GGUF repos and must be downloaded as a companion to the chosen
// LLM quant — they are not user-selectable quantizations themselves.
func isMMProjFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return strings.HasPrefix(base, "mmproj") || strings.Contains(base, "-mmproj")
}

// isMTPDraftFile reports whether a GGUF file is a multi-token-prediction
// draft model (github issue #86): "mtp-<model>.gguf", "...-MTP-draft.gguf",
// or anything in an "MTP/" folder. A main model with MTP layers built in
// (e.g. "Q8-MTP-00001-of-00005.gguf") is not a draft.
func isMTPDraftFile(p string) bool {
	lower := strings.ToLower(filepath.ToSlash(p))
	base := path.Base(lower)
	if strings.HasPrefix(base, "mtp-") || strings.HasPrefix(base, "mtp_") {
		return true
	}
	if strings.Contains(base, "mtp") && strings.Contains(base, "draft") {
		return true
	}
	for _, dir := range strings.Split(path.Dir(lower), "/") {
		if dir == "mtp" {
			return true
		}
	}
	return false
}

// isImatrixFile reports whether a GGUF file is importance-matrix calibration
// data rather than a model.
func isImatrixFile(name string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(name)), "imatrix")
}

// analyzeGGUF analyzes GGUF files and extracts quantization information.
//
// Model files are grouped into one quantization each: the shards of a split
// model ("-00001-of-00003") form one entry with their combined size, and a
// quant is named from its file name, else its folder (unsloth/bartowski put
// big quants in "Q4_K_M/" folders; custom quants like "Q3_LynnStyle/" keep
// the folder name — github issue #89). Multimodal projectors and MTP draft
// models are kept as companions (github issues #76, #86); imatrix
// calibration files are left out.
func analyzeGGUF(files []FileInfo) *GGUFInfo {
	info := &GGUFInfo{}

	var ggufFiles, modelFiles []FileInfo
	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".gguf") {
			continue
		}
		ggufFiles = append(ggufFiles, f)
		switch {
		case isMMProjFile(f.Name):
			info.MMProjFiles = append(info.MMProjFiles, f)
		case isMTPDraftFile(f.Path):
			info.MTPFiles = append(info.MTPFiles, f)
		case isImatrixFile(f.Name):
			// calibration data, not a model
		default:
			modelFiles = append(modelFiles, f)
		}
	}

	if len(modelFiles) == 0 && len(info.MMProjFiles) == 0 {
		return nil
	}

	// Extract model name and parameter count from a model file (or the
	// first mmproj file if the repo is mmproj-only, which is rare).
	var nameSource string
	if len(modelFiles) > 0 {
		nameSource = filtermatch.StripShard(modelFiles[0].Name)
	} else {
		nameSource = info.MMProjFiles[0].Name
	}
	if matches := modelNamePattern.FindStringSubmatch(nameSource); len(matches) > 1 {
		info.ModelName = strings.ReplaceAll(matches[1], "-", " ")
		info.ModelName = strings.ReplaceAll(info.ModelName, "_", " ")
	}
	if matches := paramPattern.FindStringSubmatch(nameSource); len(matches) > 1 {
		info.ParameterCount = matches[1] + "B"
	}

	// Group shards: same folder + same name once the shard suffix is removed.
	type group struct{ files []FileInfo }
	var order []string
	groups := map[string]*group{}
	for _, f := range modelFiles {
		key := strings.ToLower(path.Join(filepath.ToSlash(f.Directory), filtermatch.StripShard(f.Name)))
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
			order = append(order, key)
		}
		g.files = append(g.files, f)
	}

	labels := map[string]int{}
	for _, key := range order {
		g := groups[key]
		sort.Slice(g.files, func(i, j int) bool { return g.files[i].Path < g.files[j].Path })
		q := parseGGUFGroup(g.files)
		q.Filter = ggufGroupFilter(q, g.files, ggufFiles)
		labels[q.Name]++
		info.Quantizations = append(info.Quantizations, q)
	}

	// Disambiguate identical labels (same quant in two folders).
	for i := range info.Quantizations {
		q := &info.Quantizations[i]
		if labels[q.Name] > 1 && q.File.Directory != "" && q.File.Directory != "." {
			q.Name = q.Name + " (" + q.File.Directory + ")"
		}
	}

	// Sort by quality (descending) then by size (ascending)
	sort.Slice(info.Quantizations, func(i, j int) bool {
		if info.Quantizations[i].Quality != info.Quantizations[j].Quality {
			return info.Quantizations[i].Quality > info.Quantizations[j].Quality
		}
		return info.Quantizations[i].Size < info.Quantizations[j].Size
	})

	return info
}

// parseGGUFQuantization extracts quantization info from a single GGUF file.
func parseGGUFQuantization(f FileInfo) *GGUFQuantization {
	q := parseGGUFGroup([]FileInfo{f})
	return &q
}

// parseGGUFGroup builds the quantization entry for one model's files (one
// file, or the shards of a split model).
func parseGGUFGroup(files []FileInfo) GGUFQuantization {
	first := files[0]
	var total int64
	for _, f := range files {
		total += f.Size
	}

	stem := filtermatch.StripShard(filepath.Base(first.Name))
	dir := filepath.ToSlash(first.Directory)
	folder := ""
	if dir != "" && dir != "." {
		folder = path.Base(dir)
	}

	// Name from the file, else the folder, else the folder/file name itself.
	label, quantType, ok := quantLabel(stem)
	if !ok {
		label, quantType, ok = quantLabel(folder)
	}
	if !ok && folder != "" {
		label = folder
	} else if !ok {
		label = stem
	}

	quality := quantQuality[quantType]
	if quality == 0 {
		quality = 3
		if m := bitsPattern.FindStringSubmatch(label); m != nil {
			quality = qualityByBits[m[1]]
		}
	}

	desc := quantDescriptions[quantType]
	switch {
	case quantType == "":
		desc = "Custom quantization"
	case desc == "":
		desc = "Quantized model"
	}
	if strings.HasPrefix(label, "UD-") {
		desc = "Unsloth Dynamic: " + desc
	}
	if len(files) > 1 {
		desc += fmt.Sprintf(" (%d-part split)", len(files))
	}

	ram := estimateRAM(total)
	return GGUFQuantization{
		Name:              label,
		File:              first,
		Files:             files,
		Size:              total,
		SizeHuman:         humanSize(total),
		Quality:           quality,
		QualityStars:      qualityToStars(quality),
		EstimatedRAM:      ram,
		EstimatedRAMHuman: humanSize(ram),
		Description:       desc,
	}
}

// ggufGroupFilter returns the shortest filter that, with --exact, selects
// exactly this quantization's files among all GGUF files in the repo. A
// plain quant name ("q4_k_m") is preferred; when it would also catch other
// files (a draft in the same folder, an mmproj sharing the quant tag, a
// custom quant without a standard name) the file name without its shard
// suffix is used, which matches all shards and nothing else.
func ggufGroupFilter(q GGUFQuantization, own, all []FileInfo) string {
	stem := strings.ToLower(filtermatch.StripShard(filepath.Base(q.File.Name)))
	var candidates []string
	if _, quantType, ok := quantLabel(q.Name); ok {
		candidates = append(candidates, strings.ToLower(quantType))
	}
	candidates = append(candidates, stem)
	if dir := filepath.ToSlash(q.File.Directory); dir != "" && dir != "." {
		candidates = append(candidates, strings.ToLower(dir)+"/")
	}

	ownSet := map[string]bool{}
	for _, f := range own {
		ownSet[f.Path] = true
	}
	for _, c := range candidates {
		ok := true
		for _, f := range all {
			if filtermatch.Match(strings.ToLower(filepath.ToSlash(f.Path)), c, true) != ownSet[f.Path] {
				ok = false
				break
			}
		}
		if ok {
			return c
		}
	}
	return stem
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
		return recommended[i].Size < recommended[j].Size
	})

	return recommended
}

// preferredMMProj picks the preferred multimodal projector file from a set.
// Preference order by precision: F16 > BF16 > F32 > first file. Returns the
// chosen FileInfo and a narrow filter string (lowercased basename without
// .gguf extension) that matches only that file under the downloader's
// case-insensitive substring filter semantics.
func preferredMMProj(files []FileInfo) (FileInfo, string) {
	precedence := []string{"-f16", "-bf16", "-f32"}
	for _, p := range precedence {
		for i := range files {
			name := strings.ToLower(filepath.Base(files[i].Name))
			if strings.Contains(name, p) {
				return files[i], strings.TrimSuffix(name, ".gguf")
			}
		}
	}
	// Fallback: pick the first file and build a filter from its full basename
	// so the filter still matches exactly one file.
	name := strings.ToLower(filepath.Base(files[0].Name))
	return files[0], strings.TrimSuffix(name, ".gguf")
}

// GGUFToSelectableItems converts GGUF quantizations to SelectableItems.
// This provides a unified interface for the web UI and CLI.
//
// Each quantization is one item covering all of its files (every shard of a
// split model). Companions follow: the preferred mmproj vision encoder
// (Category="vision_encoder", Recommended so the recommended command
// bundles it — github issue #76) and MTP draft models for speculative
// decoding (Category="mtp_draft", optional — github issue #86). Filter
// values are meant for --exact matching.
func GGUFToSelectableItems(info *GGUFInfo) []SelectableItem {
	if info == nil {
		return nil
	}
	if len(info.Quantizations) == 0 && len(info.MMProjFiles) == 0 {
		return nil
	}

	items := make([]SelectableItem, 0, len(info.Quantizations)+1+len(info.MTPFiles))

	// Track if we have a Q4_K_M (common recommended default)
	hasQ4KM := false
	for _, q := range info.Quantizations {
		if q.Name == "Q4_K_M" {
			hasQ4KM = true
			break
		}
	}

	recommendedOne := false
	fallback := -1 // used when no quant fits the rule above
	for i, q := range info.Quantizations {
		if fallback < 0 || (q.Quality >= 4 && (info.Quantizations[fallback].Quality < 4 || q.Size < info.Quantizations[fallback].Size)) {
			fallback = i
		}
	}
	for _, q := range info.Quantizations {
		// Recommend Q4_K_M when present, otherwise the best-rated quant
		// under 10 GiB — one quant, not every one that qualifies.
		recommended := false
		if hasQ4KM && q.Name == "Q4_K_M" {
			recommended = true
		} else if !hasQ4KM && !recommendedOne && q.Quality >= 4 && q.Size < 10*1024*1024*1024 {
			recommended = true
		}
		recommendedOne = recommendedOne || recommended

		paths := make([]string, 0, len(q.Files))
		for _, f := range q.Files {
			paths = append(paths, f.Path)
		}
		if len(paths) == 0 {
			paths = []string{q.File.Path}
		}
		filter := q.Filter
		if filter == "" {
			filter = strings.ToLower(q.Name)
		}
		items = append(items, SelectableItem{
			ID:           strings.ToLower(q.Name),
			Label:        q.Name,
			Description:  q.Description,
			Size:         q.Size,
			SizeHuman:    q.SizeHuman,
			Quality:      q.Quality,
			QualityStars: q.QualityStars,
			Recommended:  recommended,
			Category:     "quantization",
			FilterValue:  filter,
			Files:        paths,
			RAM:          q.EstimatedRAM,
			RAMHuman:     q.EstimatedRAMHuman,
		})
	}

	// Nothing fit (no Q4_K_M, nothing 4-star under 10 GiB — e.g. a repo of
	// big custom quants): recommend the smallest 4-star-or-better quant, or
	// the best-rated one, so "Recommended" never means "only the mmproj".
	if !recommendedOne && fallback >= 0 {
		items[fallback].Recommended = true
	}

	// Append a vision-encoder companion item when mmproj files are present.
	// This is what makes multimodal GGUF downloads actually work end-to-end
	// (github issue #76) — the user picks a quant and the mmproj tags along
	// via the comma-separated -F filter list.
	if len(info.MMProjFiles) > 0 {
		chosen, filter := preferredMMProj(info.MMProjFiles)
		items = append(items, SelectableItem{
			ID:          "mmproj",
			Label:       filepath.Base(chosen.Name),
			Description: "Multimodal projector; required alongside the LLM quant for vision/multimodal models",
			Size:        chosen.Size,
			SizeHuman:   chosen.SizeHuman,
			Recommended: true,
			Category:    "vision_encoder",
			FilterValue: filter,
			Files:       []string{chosen.Path},
		})
	}

	// MTP draft models: optional companions for speculative decoding.
	for _, f := range info.MTPFiles {
		stem := strings.ToLower(filtermatch.StripShard(filepath.Base(f.Name)))
		items = append(items, SelectableItem{
			ID:          "mtp:" + strings.ToLower(f.Path),
			Label:       filepath.Base(f.Name),
			Description: "Multi-token-prediction draft model for speculative decoding (optional; needs llama.cpp MTP support)",
			Size:        f.Size,
			SizeHuman:   f.SizeHuman,
			Category:    "mtp_draft",
			FilterValue: stem,
			Files:       []string{f.Path},
		})
	}

	return items
}
