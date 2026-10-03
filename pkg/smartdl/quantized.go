// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"fmt"
	"strings"
)

// Quantization method descriptions.
var quantMethodDescriptions = map[string]string{
	"gptq":               "GPTQ - GPU-accelerated post-training quantization",
	"awq":                "AWQ - Activation-aware Weight Quantization",
	"exl2":               "EXL2 - ExLlamaV2 mixed-precision quantization",
	"exl3":               "EXL3 - ExLlamaV3 trellis quantization",
	"bitsandbytes":       "bitsandbytes INT8/INT4 quantization",
	"bnb":                "bitsandbytes INT8/INT4 quantization",
	"hqq":                "HQQ - Half-Quadratic Quantization",
	"eetq":               "EETQ - Easy and Efficient Quantization",
	"fp8":                "FP8 - 8-bit floating point weights",
	"compressed-tensors": "compressed-tensors (llm-compressor) quantization",
	"mlx":                "MLX - Apple silicon quantization (mlx-lm)",
}

// quantizedRepoType maps a quantization method to its repo type.
func quantizedRepoType(method string) RepoType {
	switch method {
	case "gptq":
		return TypeGPTQ
	case "awq":
		return TypeAWQ
	}
	return TypeQuantized
}

// quantizedTypeDescription describes a quantized repo, e.g.
// "bitsandbytes quantized model (4-bit)".
func quantizedTypeDescription(q *QuantizedInfo) string {
	name := map[string]string{
		"gptq": "GPTQ", "awq": "AWQ", "exl2": "EXL2", "exl3": "EXL3", "bitsandbytes": "bitsandbytes",
		"bnb": "bitsandbytes", "hqq": "HQQ", "eetq": "EETQ", "fp8": "FP8", "compressed-tensors": "compressed-tensors", "mlx": "MLX",
	}[q.Method]
	if name == "" {
		name = q.Method
	}
	desc := name + " quantized model"
	switch {
	case q.BitsPerWeight > 0:
		desc += fmt.Sprintf(" (%.2f bpw)", q.BitsPerWeight)
	case q.Bits > 0:
		desc += fmt.Sprintf(" (%d-bit)", q.Bits)
	}
	return desc
}

// analyzeQuantized analyzes quantized models. The quantization settings come
// from, in order: quantize_config.json (older AutoGPTQ/AutoAWQ repos),
// quantization_config.json (EXL3),
// config.json's "quantization_config" (transformers-native GPTQ, AWQ,
// bitsandbytes, FP8, compressed-tensors, EXL3, ...), or config.json's
// "quantization" block written by mlx-lm. Returns nil when none is present.
func analyzeQuantized(metadata map[string]interface{}) *QuantizedInfo {
	info := &QuantizedInfo{}

	cfgJSON, _ := metadata["config.json"].(map[string]interface{})
	config, fromQuantizeConfig := metadata["quantize_config.json"].(map[string]interface{})
	exlConfig, _ := metadata["quantization_config.json"].(map[string]interface{})
	switch {
	case fromQuantizeConfig:
	case exlConfig != nil && exlConfig["quant_method"] != nil:
		config = exlConfig // EXL3 writes a standalone quantization_config.json
	case cfgJSON != nil && cfgJSON["quantization_config"] != nil:
		config, _ = cfgJSON["quantization_config"].(map[string]interface{})
	case cfgJSON != nil && cfgJSON["quant_method"] != nil:
		config = cfgJSON // flat form, quant_method at the top level
	case cfgJSON != nil && cfgJSON["quantization"] != nil:
		config, _ = cfgJSON["quantization"].(map[string]interface{})
		if config != nil {
			info.Method = "mlx"
		}
	}
	if config == nil {
		return nil
	}

	// Detect quantization method
	if method, ok := config["quant_method"].(string); ok {
		info.Method = strings.ToLower(method)
	} else if info.Method == "" && config["load_in_4bit"] != nil {
		info.Method = "bitsandbytes"
	} else if info.Method == "" && fromQuantizeConfig {
		info.Method = "gptq" // AutoGPTQ's quantize_config.json predates quant_method
	}
	if desc, exists := quantMethodDescriptions[info.Method]; exists {
		info.MethodDescription = desc
	}
	if info.Method == "bitsandbytes" && info.Bits == 0 {
		if b, _ := config["load_in_4bit"].(bool); b {
			info.Bits = 4
		} else if b, _ := config["load_in_8bit"].(bool); b {
			info.Bits = 8
		}
	}

	// GPTQ specific fields
	if bits, ok := config["bits"].(float64); ok {
		info.Bits = int(bits)
	}

	if groupSize, ok := config["group_size"].(float64); ok {
		info.GroupSize = int(groupSize)
	}

	if descAct, ok := config["desc_act"].(bool); ok {
		info.DescAct = descAct
	}

	if symm, ok := config["sym"].(bool); ok {
		info.Symmetric = symm
	}

	// AWQ specific fields
	if zeroPoint, ok := config["zero_point"].(bool); ok {
		info.ZeroPoint = zeroPoint
	}

	if version, ok := config["version"].(string); ok {
		info.Version = version
	}

	// EXL2 specific
	if bpw, ok := config["bits_per_weight"].(float64); ok {
		info.BitsPerWeight = bpw
	}
	// EXL3 stores fractional bits (e.g. 2.51) in "bits"
	if info.Method == "exl3" {
		if bits, ok := config["bits"].(float64); ok && bits != float64(int(bits)) {
			info.BitsPerWeight = bits
		}
		if hb, ok := config["head_bits"].(float64); ok {
			info.HeadBits = int(hb)
		}
	}

	// Module quantization info
	if modules, ok := config["modules_to_not_convert"].([]interface{}); ok {
		for _, m := range modules {
			if s, ok := m.(string); ok {
				info.ExcludedModules = append(info.ExcludedModules, s)
			}
		}
	}

	// Backend compatibility
	info.Backends = detectBackends(info)

	// Get model architecture from config.json if available
	if configJson, ok := metadata["config.json"].(map[string]interface{}); ok {
		if arch, ok := configJson["architectures"].([]interface{}); ok && len(arch) > 0 {
			if s, ok := arch[0].(string); ok {
				info.ModelArchitecture = s
			}
		}

		// Model size for VRAM estimation
		if hiddenSize, ok := configJson["hidden_size"].(float64); ok {
			if numLayers, ok := configJson["num_hidden_layers"].(float64); ok {
				info.EstimatedVRAM = estimateVRAM(int(hiddenSize), int(numLayers), info.Bits)
			}
		}
	}

	return info
}

// detectBackends returns compatible inference backends.
func detectBackends(info *QuantizedInfo) []string {
	var backends []string

	switch info.Method {
	case "gptq":
		backends = append(backends, "auto-gptq", "exllamav2", "transformers")
		if info.GroupSize == 128 && !info.DescAct {
			backends = append(backends, "vllm")
		}
	case "awq":
		backends = append(backends, "autoawq", "vllm", "transformers")
	case "exl2":
		backends = append(backends, "exllamav2")
	case "bitsandbytes", "bnb":
		backends = append(backends, "transformers", "bitsandbytes")
	case "hqq":
		backends = append(backends, "hqq", "transformers")
	case "eetq":
		backends = append(backends, "eetq", "transformers")
	case "exl3":
		backends = append(backends, "exllamav3", "tabbyAPI")
	case "fp8", "compressed-tensors":
		backends = append(backends, "vllm", "sglang", "transformers")
	case "mlx":
		backends = append(backends, "mlx-lm")
	}

	return backends
}

// estimateVRAM estimates GPU VRAM requirements for quantized models.
// This is a rough estimate based on model dimensions and bit width.
func estimateVRAM(hiddenSize, numLayers, bits int) int64 {
	if bits == 0 {
		bits = 4 // Default assumption
	}

	// Rough parameter count estimation for transformer models
	// params ≈ 12 * hidden_size^2 * num_layers (simplified)
	paramsApprox := int64(12) * int64(hiddenSize) * int64(hiddenSize) * int64(numLayers)

	// Bytes per parameter based on bit width
	bytesPerParam := float64(bits) / 8.0

	// Add 20% overhead for KV cache and activations
	vram := int64(float64(paramsApprox) * bytesPerParam * 1.2)

	return vram
}

// IsGPTQ checks if the model uses GPTQ quantization.
func IsGPTQ(info *QuantizedInfo) bool {
	return info.Method == "gptq"
}

// IsAWQ checks if the model uses AWQ quantization.
func IsAWQ(info *QuantizedInfo) bool {
	return info.Method == "awq"
}

// IsEXL2 checks if the model uses EXL2 quantization.
func IsEXL2(info *QuantizedInfo) bool {
	return info.Method == "exl2"
}

// VRAMHuman returns VRAM estimate as human-readable string.
func VRAMHuman(info *QuantizedInfo) string {
	return humanSize(info.EstimatedVRAM)
}

// SupportsBackend checks if a specific backend is compatible.
func SupportsBackend(info *QuantizedInfo, backend string) bool {
	for _, b := range info.Backends {
		if b == backend {
			return true
		}
	}
	return false
}

// QuantizedToSelectableItems converts quantized model info to SelectableItems.
// For GPTQ/AWQ, there's typically only one version, so this shows informational items.
func QuantizedToSelectableItems(info *QuantizedInfo, files []FileInfo) []SelectableItem {
	if info == nil {
		return nil
	}

	var items []SelectableItem

	// Check for safetensors vs bin files
	hasSafetensors := false
	hasPytorchBin := false
	var safetensorsSize, pytorchBinSize int64

	for _, f := range files {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".safetensors") {
			hasSafetensors = true
			safetensorsSize += f.Size
		} else if strings.HasSuffix(lower, ".bin") && !strings.Contains(lower, "tokenizer") {
			hasPytorchBin = true
			pytorchBinSize += f.Size
		}
	}

	// Add format options if both are available
	if hasSafetensors && hasPytorchBin {
		items = append(items, SelectableItem{
			ID:           "safetensors",
			Label:        "SafeTensors",
			Description:  "Faster loading, recommended",
			Size:         safetensorsSize,
			SizeHuman:    humanSize(safetensorsSize),
			Quality:      5,
			QualityStars: "★★★★★",
			Recommended:  true,
			Category:     "format",
			FilterValue:  "safetensors",
		})

		binFilter, binSize := pytorchBinSelection(files)
		if binSize == 0 {
			binSize = pytorchBinSize
		}
		items = append(items, SelectableItem{
			ID:           "pytorch",
			Label:        "PyTorch (.bin)",
			Description:  "Legacy format",
			Size:         binSize,
			SizeHuman:    humanSize(binSize),
			Quality:      3,
			QualityStars: "★★★☆☆",
			Recommended:  false,
			Category:     "format",
			FilterValue:  binFilter,
		})
	}

	return items
}
