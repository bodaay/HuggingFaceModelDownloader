// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import "strings"

// Quantization method descriptions.
var quantMethodDescriptions = map[string]string{
	"gptq":        "GPTQ - GPU-accelerated post-training quantization",
	"awq":         "AWQ - Activation-aware Weight Quantization",
	"exl2":        "EXL2 - ExLlamaV2 mixed-precision quantization",
	"bitsandbytes": "bitsandbytes INT8/INT4 quantization",
	"bnb":         "bitsandbytes INT8/INT4 quantization",
	"hqq":         "HQQ - Half-Quadratic Quantization",
	"eetq":        "EETQ - Easy and Efficient Quantization",
}

// analyzeQuantized analyzes GPTQ/AWQ/EXL2 quantized models.
func analyzeQuantized(metadata map[string]interface{}) *QuantizedInfo {
	info := &QuantizedInfo{}

	// Parse quantize_config.json
	config, ok := metadata["quantize_config.json"].(map[string]interface{})
	if !ok {
		// Try config.json for some quantized models
		config, ok = metadata["config.json"].(map[string]interface{})
		if !ok {
			return nil
		}
	}

	// Detect quantization method
	if method, ok := config["quant_method"].(string); ok {
		info.Method = method
		if desc, exists := quantMethodDescriptions[method]; exists {
			info.MethodDescription = desc
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

		items = append(items, SelectableItem{
			ID:           "pytorch",
			Label:        "PyTorch (.bin)",
			Description:  "Legacy format",
			Size:         pytorchBinSize,
			SizeHuman:    humanSize(pytorchBinSize),
			Quality:      3,
			QualityStars: "★★★☆☆",
			Recommended:  false,
			Category:     "format",
			FilterValue:  ".bin",
		})
	}

	return items
}
