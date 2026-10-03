// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Known diffusers pipeline types and their descriptions.
var pipelineDescriptions = map[string]string{
	"StableDiffusionPipeline":          "Stable Diffusion v1.x text-to-image",
	"StableDiffusionImg2ImgPipeline":   "Stable Diffusion image-to-image",
	"StableDiffusionInpaintPipeline":   "Stable Diffusion inpainting",
	"StableDiffusionXLPipeline":        "Stable Diffusion XL text-to-image",
	"StableDiffusionXLImg2ImgPipeline": "SDXL image-to-image",
	"StableDiffusionXLInpaintPipeline": "SDXL inpainting",
	"FluxPipeline":                     "Flux text-to-image",
	"FluxImg2ImgPipeline":              "Flux image-to-image",
	"FluxControlNetPipeline":           "Flux with ControlNet",
	"KandinskyPipeline":                "Kandinsky v2 text-to-image",
	"KandinskyV22Pipeline":             "Kandinsky v2.2 text-to-image",
	"StableVideoDiffusionPipeline":     "Stable Video Diffusion",
	"PixArtAlphaPipeline":              "PixArt-α text-to-image",
	"HunyuanDiTPipeline":               "Hunyuan-DiT text-to-image",
	"WuerstchenPipeline":               "Würstchen text-to-image",
	"AnimateDiffPipeline":              "AnimateDiff animation",
	"LatentConsistencyModelPipeline":   "LCM fast inference",
}

// Component requirements for known pipelines.
var requiredComponents = map[string][]string{
	"StableDiffusionPipeline": {"unet", "vae", "text_encoder", "tokenizer", "scheduler"},
	"StableDiffusionXLPipeline": {"unet", "vae", "text_encoder", "text_encoder_2", "tokenizer", "tokenizer_2", "scheduler"},
	"FluxPipeline": {"transformer", "vae", "text_encoder", "text_encoder_2", "tokenizer", "tokenizer_2", "scheduler"},
}

// analyzeDiffusers analyzes a diffusers repository.
func analyzeDiffusers(files []FileInfo, metadata map[string]interface{}) *DiffusersInfo {
	info := &DiffusersInfo{}

	// Parse model_index.json
	if modelIndex, ok := metadata["model_index.json"].(map[string]interface{}); ok {
		// Get pipeline type
		if className, ok := modelIndex["_class_name"].(string); ok {
			info.PipelineType = className
			if desc, exists := pipelineDescriptions[className]; exists {
				info.PipelineDescription = desc
			} else {
				info.PipelineDescription = "Diffusers pipeline"
			}
		}

		// Get diffusers version
		if version, ok := modelIndex["_diffusers_version"].(string); ok {
			info.DiffusersVersion = version
		}

		// Parse components
		for key, value := range modelIndex {
			if strings.HasPrefix(key, "_") {
				continue // Skip metadata fields
			}

			comp := DiffusersComponent{
				Name: key,
			}

			// Parse component config
			if arr, ok := value.([]interface{}); ok && len(arr) >= 2 {
				if lib, ok := arr[0].(string); ok {
					comp.Library = lib
				}
				if cls, ok := arr[1].(string); ok {
					comp.ClassName = cls
				}
			}

			// Calculate component size from files
			comp.Size = calculateComponentSize(files, key)
			comp.SizeHuman = humanSize(comp.Size)
			for _, f := range chooseComponentWeights(files, key) {
				comp.WeightFiles = append(comp.WeightFiles, f.Path)
				comp.WeightSize += f.Size
			}

			// Determine if required
			if required, ok := requiredComponents[info.PipelineType]; ok {
				for _, r := range required {
					if r == key {
						comp.Required = true
						break
					}
				}
			}

			info.Components = append(info.Components, comp)
		}
	}

	// Detect available variants (fp16, fp32, bf16)
	info.Variants = detectVariants(files)

	// Detect available precisions
	info.Precisions = detectPrecisions(files)

	return info
}

// weightFormatRank orders weight formats by preference (lower is better).
var weightFormatRank = map[string]int{".safetensors": 0, ".bin": 1, ".pt": 2, ".pth": 2, ".ckpt": 3}

// variantRank orders weight variants by preference: fp16 halves the
// download and VRAM with no practical quality loss for inference, which is
// what the old fp16-by-default recommendation intended.
var variantRank = map[string]int{"fp16": 0, "": 1, "bf16": 2}

// chooseComponentWeights picks the weight files to download for one pipeline
// component: the best (format, variant) pair present — fp16 safetensors,
// then default safetensors, then .bin — including every shard of it. Flax
// (.msgpack), ONNX and OpenVINO exports are never chosen. Repos commonly ship
// several of these side by side; downloading the whole folder pulled them
// all (SDXL base: ~39 GiB instead of ~7).
func chooseComponentWeights(files []FileInfo, component string) []FileInfo {
	type key struct {
		format, variant string
	}
	groups := map[key][]FileInfo{}
	prefix := component + "/"
	for _, f := range files {
		if !strings.HasPrefix(f.Path, prefix) || !f.IsLFS {
			continue
		}
		lower := strings.ToLower(f.Path)
		if strings.Contains(lower, "onnx") || strings.Contains(lower, "openvino") || strings.Contains(lower, "flax") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if _, ok := weightFormatRank[ext]; !ok {
			continue
		}
		// "diffusion_pytorch_model[-0000N-of-0000M].fp16.safetensors" → "fp16"
		variant := ""
		if parts := strings.Split(strings.TrimSuffix(strings.ToLower(f.Name), ext), "."); len(parts) > 1 {
			variant = parts[len(parts)-1]
		}
		k := key{ext, variant}
		groups[k] = append(groups[k], f)
	}

	var best *key
	rank := func(k key) int {
		v, ok := variantRank[k.variant]
		if !ok {
			v = 3
		}
		return weightFormatRank[k.format]*10 + v
	}
	for k := range groups {
		k := k
		if best == nil || rank(k) < rank(*best) || (rank(k) == rank(*best) && k.variant < best.variant) {
			best = &k
		}
	}
	if best == nil {
		return nil
	}
	chosen := groups[*best]
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].Path < chosen[j].Path })
	return chosen
}

// calculateComponentSize calculates total size of files in a component directory.
func calculateComponentSize(files []FileInfo, componentName string) int64 {
	var total int64
	prefix := componentName + "/"

	for _, f := range files {
		if strings.HasPrefix(f.Path, prefix) {
			total += f.Size
		}
	}

	return total
}

// detectVariants finds available variants (fp16, fp32, bf16) in files.
func detectVariants(files []FileInfo) []string {
	variants := make(map[string]bool)

	for _, f := range files {
		name := strings.ToLower(f.Name)
		dir := strings.ToLower(f.Directory)

		// Check for variant in filename
		if strings.Contains(name, ".fp16.") || strings.Contains(name, "_fp16.") {
			variants["fp16"] = true
		}
		if strings.Contains(name, ".fp32.") || strings.Contains(name, "_fp32.") {
			variants["fp32"] = true
		}
		if strings.Contains(name, ".bf16.") || strings.Contains(name, "_bf16.") {
			variants["bf16"] = true
		}

		// Check for variant directories
		if strings.Contains(dir, "/fp16") || dir == "fp16" {
			variants["fp16"] = true
		}
		if strings.Contains(dir, "/fp32") || dir == "fp32" {
			variants["fp32"] = true
		}
		if strings.Contains(dir, "/bf16") || dir == "bf16" {
			variants["bf16"] = true
		}
	}

	var result []string
	for v := range variants {
		result = append(result, v)
	}
	return result
}

// detectPrecisions finds available precisions from safetensors/bin files.
func detectPrecisions(files []FileInfo) []string {
	precisions := make(map[string]bool)

	for _, f := range files {
		name := strings.ToLower(f.Name)

		// Standard model files indicate fp32 by default
		if strings.HasSuffix(name, ".safetensors") || strings.HasSuffix(name, ".bin") {
			if !strings.Contains(name, "fp16") && !strings.Contains(name, "bf16") {
				precisions["fp32"] = true
			}
		}
	}

	var result []string
	for p := range precisions {
		result = append(result, p)
	}
	return result
}

// GetComponentFiles returns all files belonging to a component.
func GetComponentFiles(files []FileInfo, componentName string) []FileInfo {
	var result []FileInfo
	prefix := componentName + "/"

	for _, f := range files {
		// Files directly in component directory
		if strings.HasPrefix(f.Path, prefix) {
			result = append(result, f)
			continue
		}

		// Root-level files with component prefix
		if filepath.Dir(f.Path) == "." && strings.HasPrefix(f.Name, componentName) {
			result = append(result, f)
		}
	}

	return result
}

// CalculateDownloadSize calculates total size for selected components and variant.
func CalculateDownloadSize(info *DiffusersInfo, files []FileInfo, selectedComponents []string, variant string) int64 {
	var total int64
	selected := make(map[string]bool)
	for _, c := range selectedComponents {
		selected[c] = true
	}

	for _, f := range files {
		// Check if file belongs to a selected component
		dir := strings.Split(f.Path, "/")[0]
		if !selected[dir] && len(selected) > 0 {
			continue
		}

		// Check variant match
		name := strings.ToLower(f.Name)
		if variant != "" {
			// Skip files that are different variant
			for _, v := range []string{"fp16", "fp32", "bf16"} {
				if v != variant && (strings.Contains(name, "."+v+".") || strings.Contains(name, "_"+v+".")) {
					continue
				}
			}
		}

		total += f.Size
	}

	return total
}

// DiffusersToSelectableItems converts Diffusers components to SelectableItems.
//
// Each component item selects exactly the weight files chosen for it (see
// chooseComponentWeights) — with --exact, a comma-separated list of their
// paths — plus, automatically, the small config files in its folder.
// Weightless components (tokenizer, scheduler) select their folder.
// Variants are not separate items: filters combine with OR, so "-F fp16,unet"
// meant "anything fp16 plus everything in unet/".
func DiffusersToSelectableItems(info *DiffusersInfo) []SelectableItem {
	if info == nil {
		return nil
	}

	_, knownPipeline := requiredComponents[info.PipelineType]
	var items []SelectableItem
	for _, comp := range info.Components {
		desc := comp.ClassName
		if desc == "" {
			desc = comp.Library + " component"
		}
		if comp.Library == "" && comp.ClassName == "" {
			continue // null entry in model_index.json (e.g. no safety checker)
		}

		filter := comp.Name + "/"
		size := comp.Size
		if len(comp.WeightFiles) > 0 {
			filter = strings.Join(comp.WeightFiles, ",")
			size = comp.WeightSize
			desc += " (" + weightFilesLabel(comp.WeightFiles) + ")"
		}

		recommended := comp.Required
		if !knownPipeline {
			// Unknown pipeline: everything except the optional safety checker.
			recommended = comp.Name != "safety_checker"
		}

		items = append(items, SelectableItem{
			ID:          comp.Name,
			Label:       comp.Name,
			Description: desc,
			Size:        size,
			SizeHuman:   humanSize(size),
			Recommended: recommended,
			Category:    "component",
			FilterValue: filter,
			Files:       comp.WeightFiles,
		})
	}

	return items
}

// weightFilesLabel describes chosen weight files, e.g. "fp16 safetensors".
func weightFilesLabel(paths []string) string {
	name := strings.ToLower(filepath.Base(paths[0]))
	ext := filepath.Ext(name)
	label := strings.TrimPrefix(ext, ".")
	if parts := strings.Split(strings.TrimSuffix(name, ext), "."); len(parts) > 1 {
		label = parts[len(parts)-1] + " " + label
	}
	if len(paths) > 1 {
		label += fmt.Sprintf(", %d shards", len(paths))
	}
	return label
}
