// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"testing"
)

func TestAnalyzeQuantized(t *testing.T) {
	t.Run("GPTQ model with quantize_config.json", func(t *testing.T) {
		metadata := map[string]interface{}{
			"quantize_config.json": map[string]interface{}{
				"quant_method": "gptq",
				"bits":         float64(4),
				"group_size":   float64(128),
				"desc_act":     true,
				"sym":          true,
			},
			"config.json": map[string]interface{}{
				"architectures":    []interface{}{"LlamaForCausalLM"},
				"hidden_size":      float64(4096),
				"num_hidden_layers": float64(32),
			},
		}

		info := analyzeQuantized(metadata)
		if info == nil {
			t.Fatal("analyzeQuantized returned nil")
		}

		if info.Method != "gptq" {
			t.Errorf("Method = %q, want gptq", info.Method)
		}
		if info.Bits != 4 {
			t.Errorf("Bits = %d, want 4", info.Bits)
		}
		if info.GroupSize != 128 {
			t.Errorf("GroupSize = %d, want 128", info.GroupSize)
		}
		if !info.DescAct {
			t.Error("DescAct should be true")
		}
		if !info.Symmetric {
			t.Error("Symmetric should be true")
		}
		if info.ModelArchitecture != "LlamaForCausalLM" {
			t.Errorf("ModelArchitecture = %q, want LlamaForCausalLM", info.ModelArchitecture)
		}
	})

	t.Run("AWQ model", func(t *testing.T) {
		metadata := map[string]interface{}{
			"quantize_config.json": map[string]interface{}{
				"quant_method": "awq",
				"bits":         float64(4),
				"group_size":   float64(128),
				"zero_point":   true,
				"version":      "gemm",
			},
		}

		info := analyzeQuantized(metadata)
		if info == nil {
			t.Fatal("analyzeQuantized returned nil")
		}

		if info.Method != "awq" {
			t.Errorf("Method = %q, want awq", info.Method)
		}
		if !info.ZeroPoint {
			t.Error("ZeroPoint should be true")
		}
		if info.Version != "gemm" {
			t.Errorf("Version = %q, want gemm", info.Version)
		}
	})

	t.Run("EXL2 model", func(t *testing.T) {
		metadata := map[string]interface{}{
			"quantize_config.json": map[string]interface{}{
				"quant_method":    "exl2",
				"bits_per_weight": float64(4.5),
			},
		}

		info := analyzeQuantized(metadata)
		if info == nil {
			t.Fatal("analyzeQuantized returned nil")
		}

		if info.Method != "exl2" {
			t.Errorf("Method = %q, want exl2", info.Method)
		}
		if info.BitsPerWeight != 4.5 {
			t.Errorf("BitsPerWeight = %f, want 4.5", info.BitsPerWeight)
		}
	})

	t.Run("model with excluded modules", func(t *testing.T) {
		metadata := map[string]interface{}{
			"quantize_config.json": map[string]interface{}{
				"quant_method":          "gptq",
				"modules_to_not_convert": []interface{}{"lm_head", "embed_tokens"},
			},
		}

		info := analyzeQuantized(metadata)
		if info == nil {
			t.Fatal("analyzeQuantized returned nil")
		}

		if len(info.ExcludedModules) != 2 {
			t.Errorf("ExcludedModules length = %d, want 2", len(info.ExcludedModules))
		}
		if info.ExcludedModules[0] != "lm_head" {
			t.Errorf("ExcludedModules[0] = %q, want lm_head", info.ExcludedModules[0])
		}
	})

	t.Run("fallback to config.json", func(t *testing.T) {
		metadata := map[string]interface{}{
			"config.json": map[string]interface{}{
				"quant_method":     "bitsandbytes",
				"bits":             float64(8),
				"architectures":    []interface{}{"MistralForCausalLM"},
				"hidden_size":      float64(4096),
				"num_hidden_layers": float64(32),
			},
		}

		info := analyzeQuantized(metadata)
		if info == nil {
			t.Fatal("analyzeQuantized returned nil")
		}

		if info.Method != "bitsandbytes" {
			t.Errorf("Method = %q, want bitsandbytes", info.Method)
		}
	})

	t.Run("no config files", func(t *testing.T) {
		metadata := map[string]interface{}{}
		info := analyzeQuantized(metadata)
		if info != nil {
			t.Error("expected nil for no config files")
		}
	})

	t.Run("invalid config type", func(t *testing.T) {
		metadata := map[string]interface{}{
			"quantize_config.json": "not a map",
		}
		info := analyzeQuantized(metadata)
		if info != nil {
			t.Error("expected nil for invalid config type")
		}
	})
}

func TestDetectBackends(t *testing.T) {
	tests := []struct {
		name      string
		info      *QuantizedInfo
		expected  []string
		mustHave  string
		mustAvoid string
	}{
		{
			name: "GPTQ standard",
			info: &QuantizedInfo{
				Method:    "gptq",
				GroupSize: 64,
				DescAct:   true,
			},
			mustHave:  "auto-gptq",
			mustAvoid: "vllm",
		},
		{
			name: "GPTQ vllm compatible",
			info: &QuantizedInfo{
				Method:    "gptq",
				GroupSize: 128,
				DescAct:   false,
			},
			mustHave: "vllm",
		},
		{
			name: "AWQ",
			info: &QuantizedInfo{
				Method: "awq",
			},
			mustHave: "autoawq",
		},
		{
			name: "EXL2",
			info: &QuantizedInfo{
				Method: "exl2",
			},
			expected: []string{"exllamav2"},
		},
		{
			name: "bitsandbytes",
			info: &QuantizedInfo{
				Method: "bitsandbytes",
			},
			mustHave: "bitsandbytes",
		},
		{
			name: "bnb alias",
			info: &QuantizedInfo{
				Method: "bnb",
			},
			mustHave: "bitsandbytes",
		},
		{
			name: "HQQ",
			info: &QuantizedInfo{
				Method: "hqq",
			},
			mustHave: "hqq",
		},
		{
			name: "EETQ",
			info: &QuantizedInfo{
				Method: "eetq",
			},
			mustHave: "eetq",
		},
		{
			name: "unknown method",
			info: &QuantizedInfo{
				Method: "unknown",
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backends := detectBackends(tt.info)

			if tt.expected != nil {
				if len(backends) != len(tt.expected) {
					t.Errorf("backends = %v, want %v", backends, tt.expected)
				}
			}

			if tt.mustHave != "" {
				found := false
				for _, b := range backends {
					if b == tt.mustHave {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("backends %v missing %q", backends, tt.mustHave)
				}
			}

			if tt.mustAvoid != "" {
				for _, b := range backends {
					if b == tt.mustAvoid {
						t.Errorf("backends should not contain %q", tt.mustAvoid)
					}
				}
			}
		})
	}
}

func TestEstimateVRAM(t *testing.T) {
	tests := []struct {
		name       string
		hiddenSize int
		numLayers  int
		bits       int
		minVRAM    int64
		maxVRAM    int64
	}{
		{
			name:       "7B model 4-bit",
			hiddenSize: 4096,
			numLayers:  32,
			bits:       4,
			minVRAM:    1 * 1024 * 1024 * 1024,  // At least 1GB
			maxVRAM:    20 * 1024 * 1024 * 1024, // Less than 20GB
		},
		{
			name:       "7B model 8-bit",
			hiddenSize: 4096,
			numLayers:  32,
			bits:       8,
			minVRAM:    2 * 1024 * 1024 * 1024,  // At least 2GB
			maxVRAM:    40 * 1024 * 1024 * 1024, // Less than 40GB
		},
		{
			name:       "zero bits uses default",
			hiddenSize: 4096,
			numLayers:  32,
			bits:       0,
			minVRAM:    1 * 1024 * 1024 * 1024, // Should use 4-bit default
			maxVRAM:    20 * 1024 * 1024 * 1024,
		},
		{
			name:       "small model",
			hiddenSize: 768,
			numLayers:  12,
			bits:       4,
			minVRAM:    10 * 1024 * 1024, // At least 10MB
			maxVRAM:    2 * 1024 * 1024 * 1024,
		},
		{
			name:       "large model",
			hiddenSize: 8192,
			numLayers:  80,
			bits:       4,
			minVRAM:    10 * 1024 * 1024 * 1024, // At least 10GB
			maxVRAM:    200 * 1024 * 1024 * 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vram := estimateVRAM(tt.hiddenSize, tt.numLayers, tt.bits)
			if vram < tt.minVRAM || vram > tt.maxVRAM {
				t.Errorf("vram = %d, want between %d and %d", vram, tt.minVRAM, tt.maxVRAM)
			}
		})
	}

	t.Run("larger model uses more VRAM", func(t *testing.T) {
		smallVRAM := estimateVRAM(2048, 24, 4)
		largeVRAM := estimateVRAM(4096, 32, 4)
		if largeVRAM <= smallVRAM {
			t.Errorf("large model VRAM (%d) should be > small model (%d)", largeVRAM, smallVRAM)
		}
	})

	t.Run("higher bits uses more VRAM", func(t *testing.T) {
		vram4bit := estimateVRAM(4096, 32, 4)
		vram8bit := estimateVRAM(4096, 32, 8)
		if vram8bit <= vram4bit {
			t.Errorf("8-bit VRAM (%d) should be > 4-bit (%d)", vram8bit, vram4bit)
		}
	})
}

func TestIsGPTQ(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
	}{
		{"gptq", true},
		{"awq", false},
		{"exl2", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			info := &QuantizedInfo{Method: tt.method}
			if got := IsGPTQ(info); got != tt.expected {
				t.Errorf("IsGPTQ() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsAWQ(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
	}{
		{"awq", true},
		{"gptq", false},
		{"exl2", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			info := &QuantizedInfo{Method: tt.method}
			if got := IsAWQ(info); got != tt.expected {
				t.Errorf("IsAWQ() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsEXL2(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
	}{
		{"exl2", true},
		{"gptq", false},
		{"awq", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			info := &QuantizedInfo{Method: tt.method}
			if got := IsEXL2(info); got != tt.expected {
				t.Errorf("IsEXL2() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestVRAMHuman(t *testing.T) {
	tests := []struct {
		name     string
		vram     int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"kilobytes", 1536, "1.5 KiB"},
		{"megabytes", 256 * 1024 * 1024, "256.0 MiB"},
		{"gigabytes", 8 * 1024 * 1024 * 1024, "8.0 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &QuantizedInfo{EstimatedVRAM: tt.vram}
			result := VRAMHuman(info)
			if result != tt.expected {
				t.Errorf("VRAMHuman() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSupportsBackend(t *testing.T) {
	info := &QuantizedInfo{
		Backends: []string{"auto-gptq", "exllamav2", "transformers"},
	}

	t.Run("supported backend", func(t *testing.T) {
		if !SupportsBackend(info, "auto-gptq") {
			t.Error("should support auto-gptq")
		}
		if !SupportsBackend(info, "exllamav2") {
			t.Error("should support exllamav2")
		}
	})

	t.Run("unsupported backend", func(t *testing.T) {
		if SupportsBackend(info, "vllm") {
			t.Error("should not support vllm")
		}
		if SupportsBackend(info, "unknown") {
			t.Error("should not support unknown")
		}
	})

	t.Run("empty backends", func(t *testing.T) {
		emptyInfo := &QuantizedInfo{Backends: []string{}}
		if SupportsBackend(emptyInfo, "any") {
			t.Error("should not support any backend with empty list")
		}
	})

	t.Run("nil backends", func(t *testing.T) {
		nilInfo := &QuantizedInfo{}
		if SupportsBackend(nilInfo, "any") {
			t.Error("should not support any backend with nil list")
		}
	})
}

func TestQuantizedToSelectableItems(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		items := QuantizedToSelectableItems(nil, nil)
		if items != nil {
			t.Error("expected nil for nil info")
		}
	})

	t.Run("both formats available", func(t *testing.T) {
		info := &QuantizedInfo{Method: "gptq"}
		files := []FileInfo{
			{Name: "model.safetensors", Size: 4 * 1024 * 1024 * 1024},
			{Name: "model.safetensors.index.json", Size: 1024},
			{Name: "pytorch_model.bin", Size: 4 * 1024 * 1024 * 1024},
			{Name: "tokenizer.bin", Size: 1024}, // Should be skipped
		}

		items := QuantizedToSelectableItems(info, files)
		if len(items) != 2 {
			t.Fatalf("items length = %d, want 2", len(items))
		}

		// SafeTensors should be recommended
		safetensorsItem := items[0]
		if safetensorsItem.ID != "safetensors" {
			t.Errorf("first item ID = %q, want safetensors", safetensorsItem.ID)
		}
		if !safetensorsItem.Recommended {
			t.Error("safetensors should be recommended")
		}
		if safetensorsItem.Quality != 5 {
			t.Errorf("safetensors quality = %d, want 5", safetensorsItem.Quality)
		}

		// PyTorch should not be recommended
		pytorchItem := items[1]
		if pytorchItem.ID != "pytorch" {
			t.Errorf("second item ID = %q, want pytorch", pytorchItem.ID)
		}
		if pytorchItem.Recommended {
			t.Error("pytorch should not be recommended")
		}
	})

	t.Run("only safetensors", func(t *testing.T) {
		info := &QuantizedInfo{Method: "awq"}
		files := []FileInfo{
			{Name: "model.safetensors", Size: 4 * 1024 * 1024 * 1024},
		}

		items := QuantizedToSelectableItems(info, files)
		if len(items) != 0 {
			t.Errorf("items length = %d, want 0 (no choice needed)", len(items))
		}
	})

	t.Run("only pytorch bin", func(t *testing.T) {
		info := &QuantizedInfo{Method: "gptq"}
		files := []FileInfo{
			{Name: "pytorch_model.bin", Size: 4 * 1024 * 1024 * 1024},
		}

		items := QuantizedToSelectableItems(info, files)
		if len(items) != 0 {
			t.Errorf("items length = %d, want 0 (no choice needed)", len(items))
		}
	})

	t.Run("size aggregation", func(t *testing.T) {
		info := &QuantizedInfo{Method: "gptq"}
		files := []FileInfo{
			{Name: "model-00001-of-00002.safetensors", Size: 2 * 1024 * 1024 * 1024},
			{Name: "model-00002-of-00002.safetensors", Size: 2 * 1024 * 1024 * 1024},
			{Name: "pytorch_model-00001-of-00002.bin", Size: 2 * 1024 * 1024 * 1024},
			{Name: "pytorch_model-00002-of-00002.bin", Size: 2 * 1024 * 1024 * 1024},
		}

		items := QuantizedToSelectableItems(info, files)
		if len(items) != 2 {
			t.Fatalf("items length = %d, want 2", len(items))
		}

		expectedSize := int64(4 * 1024 * 1024 * 1024)
		if items[0].Size != expectedSize {
			t.Errorf("safetensors size = %d, want %d", items[0].Size, expectedSize)
		}
		if items[1].Size != expectedSize {
			t.Errorf("pytorch size = %d, want %d", items[1].Size, expectedSize)
		}
	})

	t.Run("category and filter values", func(t *testing.T) {
		info := &QuantizedInfo{Method: "gptq"}
		files := []FileInfo{
			{Name: "model.safetensors", Size: 1024},
			{Name: "model.bin", Size: 1024},
		}

		items := QuantizedToSelectableItems(info, files)
		for _, item := range items {
			if item.Category != "format" {
				t.Errorf("item %q category = %q, want format", item.ID, item.Category)
			}
			if item.FilterValue == "" {
				t.Errorf("item %q FilterValue should not be empty", item.ID)
			}
		}
	})
}

func TestQuantMethodDescriptions(t *testing.T) {
	methods := []string{"gptq", "awq", "exl2", "bitsandbytes", "bnb", "hqq", "eetq"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			desc, ok := quantMethodDescriptions[method]
			if !ok {
				t.Errorf("missing description for %q", method)
			}
			if desc == "" {
				t.Errorf("empty description for %q", method)
			}
		})
	}
}
