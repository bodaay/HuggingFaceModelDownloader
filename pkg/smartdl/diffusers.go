// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"path/filepath"
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

// DiffusersToSelectableItems converts Diffusers components and variants to SelectableItems.
func DiffusersToSelectableItems(info *DiffusersInfo) []SelectableItem {
	if info == nil {
		return nil
	}

	var items []SelectableItem

	// Add variants if available (fp16, fp32, bf16)
	variantDescriptions := map[string]string{
		"fp16": "Half precision - Recommended, uses less VRAM",
		"fp32": "Full precision - Maximum quality, more VRAM",
		"bf16": "Brain float - Good quality, efficient on modern GPUs",
	}

	for _, variant := range info.Variants {
		desc := variantDescriptions[variant]
		if desc == "" {
			desc = "Model variant"
		}

		item := SelectableItem{
			ID:          variant,
			Label:       strings.ToUpper(variant),
			Description: desc,
			Recommended: variant == "fp16",
			Category:    "variant",
			FilterValue: variant,
		}
		items = append(items, item)
	}

	// Add components
	for _, comp := range info.Components {
		desc := comp.ClassName
		if desc == "" {
			desc = comp.Library + " component"
		}

		item := SelectableItem{
			ID:          comp.Name,
			Label:       comp.Name,
			Description: desc,
			Size:        comp.Size,
			SizeHuman:   comp.SizeHuman,
			Recommended: comp.Required,
			Category:    "component",
			FilterValue: comp.Name,
		}
		items = append(items, item)
	}

	return items
}
