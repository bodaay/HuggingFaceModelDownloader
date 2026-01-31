// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

// Known adapter types and their descriptions.
var adapterTypeDescriptions = map[string]string{
	"lora":     "Low-Rank Adaptation for efficient fine-tuning",
	"qlora":    "Quantized LoRA with 4-bit base model",
	"ia3":      "Infused Adapter by Inhibiting and Amplifying Inner Activations",
	"adalora":  "Adaptive LoRA with dynamic rank allocation",
	"prefix":   "Prefix Tuning prepends trainable tokens",
	"prompt":   "Prompt Tuning learns soft prompts",
	"p_tuning": "P-Tuning learns continuous prompt embeddings",
	"lora_fa":  "LoRA with Frozen-A matrix",
	"vera":     "Vector-based Random Matrix Adaptation",
	"oft":      "Orthogonal Fine-Tuning",
	"boft":     "Block Orthogonal Fine-Tuning",
}

// analyzeLoRA analyzes LoRA/adapter repositories.
func analyzeLoRA(metadata map[string]interface{}) *LoRAInfo {
	info := &LoRAInfo{}

	// Parse adapter_config.json
	config, ok := metadata["adapter_config.json"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Get adapter type (peft_type)
	if peftType, ok := config["peft_type"].(string); ok {
		info.AdapterType = peftType
		if desc, exists := adapterTypeDescriptions[peftType]; exists {
			info.AdapterDescription = desc
		} else {
			info.AdapterDescription = "PEFT adapter"
		}
	}

	// Get base model
	if baseModel, ok := config["base_model_name_or_path"].(string); ok {
		info.BaseModel = baseModel
	}

	// Get LoRA-specific parameters
	if r, ok := config["r"].(float64); ok {
		info.Rank = int(r)
	}

	if alpha, ok := config["lora_alpha"].(float64); ok {
		info.Alpha = alpha
	}

	if dropout, ok := config["lora_dropout"].(float64); ok {
		info.Dropout = dropout
	}

	// Get target modules
	if modules, ok := config["target_modules"].([]interface{}); ok {
		for _, m := range modules {
			if s, ok := m.(string); ok {
				info.TargetModules = append(info.TargetModules, s)
			}
		}
	}

	// Handle target_modules as map (some configs use this format)
	if modules, ok := config["target_modules"].(map[string]interface{}); ok {
		for k := range modules {
			info.TargetModules = append(info.TargetModules, k)
		}
	}

	// Get bias setting
	if bias, ok := config["bias"].(string); ok {
		info.Bias = bias
	}

	// Get task type
	if taskType, ok := config["task_type"].(string); ok {
		info.TaskType = taskType
	}

	// Get additional PEFT settings
	if fanInFanOut, ok := config["fan_in_fan_out"].(bool); ok {
		info.FanInFanOut = fanInFanOut
	}

	if initLoraWeights, ok := config["init_lora_weights"].(bool); ok {
		info.InitLoraWeights = initLoraWeights
	}

	// QLoRA specific
	if quantType, ok := config["quant_type"].(string); ok {
		info.QuantType = quantType
	}

	return info
}

// IsQLoRA checks if the adapter is a QLoRA (quantized LoRA).
func IsQLoRA(info *LoRAInfo) bool {
	return info.AdapterType == "qlora" || info.QuantType != ""
}

// RequiresBaseModel returns true if the adapter requires a base model.
func RequiresBaseModel(info *LoRAInfo) bool {
	return info.BaseModel != ""
}

// GetEffectiveRank returns the effective rank considering alpha scaling.
func GetEffectiveRank(info *LoRAInfo) float64 {
	if info.Rank == 0 {
		return 0
	}
	if info.Alpha == 0 {
		return float64(info.Rank)
	}
	return info.Alpha / float64(info.Rank)
}

// EstimateAdapterSize estimates the adapter's parameter count.
// Formula: 2 * rank * hidden_dim * num_target_modules (approximate)
func EstimateAdapterSize(info *LoRAInfo, hiddenDim int) int64 {
	if info.Rank == 0 || hiddenDim == 0 {
		return 0
	}

	numModules := len(info.TargetModules)
	if numModules == 0 {
		numModules = 4 // Default assumption for typical LoRA configs
	}

	// Each LoRA adapter has A (rank x hidden) and B (hidden x rank) matrices
	paramsPerModule := int64(2 * info.Rank * hiddenDim)
	return paramsPerModule * int64(numModules)
}

// LoRAToRelatedDownloads creates RelatedDownload entries for LoRA adapters.
// This indicates the base model that is required for the adapter to work.
func LoRAToRelatedDownloads(info *LoRAInfo) []RelatedDownload {
	if info == nil || info.BaseModel == "" {
		return nil
	}

	return []RelatedDownload{
		{
			Type:        "base_model",
			Repo:        info.BaseModel,
			Label:       "Base Model (Required)",
			Description: "This adapter requires the base model to function",
			Required:    true,
		},
	}
}
