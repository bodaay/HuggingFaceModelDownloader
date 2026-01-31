// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"testing"
)

func TestAdapterTypeDescriptions(t *testing.T) {
	tests := []struct {
		adapterType string
		contains    string
	}{
		{"lora", "Low-Rank"},
		{"qlora", "Quantized"},
		{"ia3", "Inhibiting"},
		{"adalora", "Adaptive"},
		{"prefix", "Prefix"},
		{"vera", "Vector"},
	}

	for _, tt := range tests {
		t.Run(tt.adapterType, func(t *testing.T) {
			desc, ok := adapterTypeDescriptions[tt.adapterType]
			if !ok {
				t.Errorf("missing description for %q", tt.adapterType)
				return
			}
			if desc == "" {
				t.Errorf("empty description for %q", tt.adapterType)
			}
		})
	}
}

func TestAnalyzeLoRA(t *testing.T) {
	t.Run("standard LoRA config", func(t *testing.T) {
		metadata := map[string]interface{}{
			"adapter_config.json": map[string]interface{}{
				"peft_type":              "lora",
				"base_model_name_or_path": "meta-llama/Llama-2-7b-hf",
				"r":                       float64(16),
				"lora_alpha":              float64(32),
				"lora_dropout":            float64(0.05),
				"target_modules":          []interface{}{"q_proj", "v_proj", "k_proj", "o_proj"},
				"bias":                    "none",
				"task_type":               "CAUSAL_LM",
			},
		}

		info := analyzeLoRA(metadata)
		if info == nil {
			t.Fatal("expected non-nil result")
		}

		if info.AdapterType != "lora" {
			t.Errorf("AdapterType = %q", info.AdapterType)
		}
		if info.BaseModel != "meta-llama/Llama-2-7b-hf" {
			t.Errorf("BaseModel = %q", info.BaseModel)
		}
		if info.Rank != 16 {
			t.Errorf("Rank = %d", info.Rank)
		}
		if info.Alpha != 32 {
			t.Errorf("Alpha = %f", info.Alpha)
		}
		if info.Dropout != 0.05 {
			t.Errorf("Dropout = %f", info.Dropout)
		}
		if len(info.TargetModules) != 4 {
			t.Errorf("TargetModules count = %d", len(info.TargetModules))
		}
		if info.Bias != "none" {
			t.Errorf("Bias = %q", info.Bias)
		}
		if info.TaskType != "CAUSAL_LM" {
			t.Errorf("TaskType = %q", info.TaskType)
		}
	})

	t.Run("QLoRA config", func(t *testing.T) {
		metadata := map[string]interface{}{
			"adapter_config.json": map[string]interface{}{
				"peft_type":  "qlora",
				"quant_type": "nf4",
				"r":          float64(64),
			},
		}

		info := analyzeLoRA(metadata)
		if info == nil {
			t.Fatal("expected non-nil result")
		}

		if info.AdapterType != "qlora" {
			t.Errorf("AdapterType = %q", info.AdapterType)
		}
		if info.QuantType != "nf4" {
			t.Errorf("QuantType = %q", info.QuantType)
		}
	})

	t.Run("target_modules as map", func(t *testing.T) {
		metadata := map[string]interface{}{
			"adapter_config.json": map[string]interface{}{
				"peft_type": "lora",
				"target_modules": map[string]interface{}{
					"q_proj": true,
					"v_proj": true,
				},
			},
		}

		info := analyzeLoRA(metadata)
		if info == nil {
			t.Fatal("expected non-nil result")
		}

		if len(info.TargetModules) != 2 {
			t.Errorf("TargetModules count = %d, want 2", len(info.TargetModules))
		}
	})

	t.Run("missing adapter_config.json", func(t *testing.T) {
		metadata := map[string]interface{}{}
		info := analyzeLoRA(metadata)
		if info != nil {
			t.Error("expected nil for missing config")
		}
	})

	t.Run("boolean fields", func(t *testing.T) {
		metadata := map[string]interface{}{
			"adapter_config.json": map[string]interface{}{
				"peft_type":         "lora",
				"fan_in_fan_out":    true,
				"init_lora_weights": true,
			},
		}

		info := analyzeLoRA(metadata)
		if !info.FanInFanOut {
			t.Error("FanInFanOut should be true")
		}
		if !info.InitLoraWeights {
			t.Error("InitLoraWeights should be true")
		}
	})

	t.Run("description for known type", func(t *testing.T) {
		metadata := map[string]interface{}{
			"adapter_config.json": map[string]interface{}{
				"peft_type": "lora",
			},
		}

		info := analyzeLoRA(metadata)
		if info.AdapterDescription == "" {
			t.Error("AdapterDescription should not be empty")
		}
	})

	t.Run("description for unknown type", func(t *testing.T) {
		metadata := map[string]interface{}{
			"adapter_config.json": map[string]interface{}{
				"peft_type": "unknown_type",
			},
		}

		info := analyzeLoRA(metadata)
		if info.AdapterDescription != "PEFT adapter" {
			t.Errorf("AdapterDescription = %q, want 'PEFT adapter'", info.AdapterDescription)
		}
	})
}

func TestIsQLoRA(t *testing.T) {
	tests := []struct {
		name     string
		info     *LoRAInfo
		expected bool
	}{
		{
			name:     "qlora type",
			info:     &LoRAInfo{AdapterType: "qlora"},
			expected: true,
		},
		{
			name:     "lora with quant_type",
			info:     &LoRAInfo{AdapterType: "lora", QuantType: "nf4"},
			expected: true,
		},
		{
			name:     "standard lora",
			info:     &LoRAInfo{AdapterType: "lora"},
			expected: false,
		},
		{
			name:     "ia3",
			info:     &LoRAInfo{AdapterType: "ia3"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsQLoRA(tt.info)
			if result != tt.expected {
				t.Errorf("IsQLoRA() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRequiresBaseModel(t *testing.T) {
	tests := []struct {
		name     string
		info     *LoRAInfo
		expected bool
	}{
		{
			name:     "with base model",
			info:     &LoRAInfo{BaseModel: "meta-llama/Llama-2-7b"},
			expected: true,
		},
		{
			name:     "without base model",
			info:     &LoRAInfo{BaseModel: ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RequiresBaseModel(tt.info)
			if result != tt.expected {
				t.Errorf("RequiresBaseModel() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveRank(t *testing.T) {
	tests := []struct {
		name     string
		info     *LoRAInfo
		expected float64
	}{
		{
			name:     "zero rank",
			info:     &LoRAInfo{Rank: 0, Alpha: 32},
			expected: 0,
		},
		{
			name:     "zero alpha",
			info:     &LoRAInfo{Rank: 16, Alpha: 0},
			expected: 16,
		},
		{
			name:     "standard values",
			info:     &LoRAInfo{Rank: 16, Alpha: 32},
			expected: 2, // 32/16
		},
		{
			name:     "alpha equals rank",
			info:     &LoRAInfo{Rank: 16, Alpha: 16},
			expected: 1,
		},
		{
			name:     "high rank",
			info:     &LoRAInfo{Rank: 64, Alpha: 128},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEffectiveRank(tt.info)
			if result != tt.expected {
				t.Errorf("GetEffectiveRank() = %f, want %f", result, tt.expected)
			}
		})
	}
}

func TestEstimateAdapterSize(t *testing.T) {
	tests := []struct {
		name      string
		info      *LoRAInfo
		hiddenDim int
		minSize   int64
	}{
		{
			name:      "zero rank",
			info:      &LoRAInfo{Rank: 0},
			hiddenDim: 4096,
			minSize:   0,
		},
		{
			name:      "zero hidden dim",
			info:      &LoRAInfo{Rank: 16},
			hiddenDim: 0,
			minSize:   0,
		},
		{
			name:      "standard LoRA",
			info:      &LoRAInfo{Rank: 16, TargetModules: []string{"q_proj", "v_proj"}},
			hiddenDim: 4096,
			minSize:   100000, // Should be non-trivial
		},
		{
			name:      "no target modules (uses default)",
			info:      &LoRAInfo{Rank: 16},
			hiddenDim: 4096,
			minSize:   100000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateAdapterSize(tt.info, tt.hiddenDim)
			if result < tt.minSize {
				t.Errorf("EstimateAdapterSize() = %d, want >= %d", result, tt.minSize)
			}
		})
	}
}

func TestLoRAToRelatedDownloads(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		result := LoRAToRelatedDownloads(nil)
		if result != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("no base model", func(t *testing.T) {
		info := &LoRAInfo{AdapterType: "lora"}
		result := LoRAToRelatedDownloads(info)
		if result != nil {
			t.Error("expected nil when no base model")
		}
	})

	t.Run("with base model", func(t *testing.T) {
		info := &LoRAInfo{
			AdapterType: "lora",
			BaseModel:   "meta-llama/Llama-2-7b-hf",
		}

		result := LoRAToRelatedDownloads(info)
		if len(result) != 1 {
			t.Fatalf("expected 1 related download, got %d", len(result))
		}

		related := result[0]
		if related.Type != "base_model" {
			t.Errorf("Type = %q", related.Type)
		}
		if related.Repo != "meta-llama/Llama-2-7b-hf" {
			t.Errorf("Repo = %q", related.Repo)
		}
		if !related.Required {
			t.Error("should be required")
		}
	})
}
