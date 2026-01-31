// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"strings"
	"testing"
)

func TestQuantQualityMappings(t *testing.T) {
	tests := []struct {
		quant           string
		expectedQuality int
	}{
		// Quality 1 (lowest)
		{"Q2_K", 1},
		{"Q2_K_S", 1},
		{"IQ2_S", 1},
		{"IQ2_XS", 1},
		{"IQ2_XXS", 1},

		// Quality 2
		{"Q3_K_S", 2},
		{"Q3_K_M", 2},
		{"Q3_K_L", 2},
		{"IQ3_S", 2},
		{"IQ3_XS", 2},
		{"IQ3_M", 2},

		// Quality 3
		{"Q4_0", 3},
		{"Q4_1", 3},
		{"Q4_K_S", 3},
		{"IQ4_NL", 3},
		{"IQ4_XS", 3},

		// Quality 4
		{"Q4_K_M", 4},
		{"Q5_0", 4},
		{"Q5_1", 4},
		{"Q5_K_S", 4},

		// Quality 5 (highest)
		{"Q5_K_M", 5},
		{"Q6_K", 5},
		{"Q8_0", 5},
		{"F16", 5},
		{"F32", 5},
		{"BF16", 5},
	}

	for _, tt := range tests {
		t.Run(tt.quant, func(t *testing.T) {
			quality, ok := quantQuality[tt.quant]
			if !ok {
				t.Errorf("quantQuality missing %q", tt.quant)
				return
			}
			if quality != tt.expectedQuality {
				t.Errorf("quantQuality[%q] = %d, want %d", tt.quant, quality, tt.expectedQuality)
			}
		})
	}
}

func TestQuantDescriptions(t *testing.T) {
	// Verify all quality entries have descriptions
	for quant := range quantQuality {
		t.Run(quant, func(t *testing.T) {
			desc, ok := quantDescriptions[quant]
			if !ok {
				t.Errorf("quantDescriptions missing %q", quant)
				return
			}
			if desc == "" {
				t.Errorf("quantDescriptions[%q] is empty", quant)
			}
		})
	}
}

func TestQuantPattern(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Standard quantizations
		{"model.Q4_K_M.gguf", "Q4_K_M"},
		{"mistral-7b.Q4_0.gguf", "Q4_0"},
		{"llama-Q5_K_S.gguf", "Q5_K_S"},
		{"model.Q8_0.gguf", "Q8_0"},
		{"test-Q6_K.gguf", "Q6_K"},

		// Importance matrix quantizations
		{"model-IQ2_XS.gguf", "IQ2_XS"},
		{"model.IQ3_M.gguf", "IQ3_M"},
		{"test.IQ4_NL.gguf", "IQ4_NL"},

		// Float precisions
		{"model-F16.gguf", "F16"},
		{"model-F32.gguf", "F32"},
		{"model-BF16.gguf", "BF16"},

		// Case insensitivity
		{"model.q4_k_m.gguf", "q4_k_m"},
		{"model-Q4_K_M.gguf", "Q4_K_M"},

		// With path separators
		{"path/to/model.Q4_K_M.gguf", "Q4_K_M"},

		// No match
		{"model.safetensors", ""},
		{"config.json", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := quantPattern.FindStringSubmatch(tt.input)
			got := ""
			if len(matches) >= 2 {
				got = matches[1]
			}
			if !strings.EqualFold(got, tt.expected) {
				t.Errorf("quantPattern.FindStringSubmatch(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParamPattern(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"mistral-7b-instruct.Q4_K_M.gguf", "7"},
		{"llama-13b.Q4_K_M.gguf", "13"},
		{"llama-70b-chat.gguf", "70"},
		{"phi-1.5b.gguf", "1.5"},
		{"model-3B-v1.gguf", "3"},
		{"mixtral-8x7b.gguf", "7"}, // Captures first match
		{"no-params.gguf", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := paramPattern.FindStringSubmatch(tt.input)
			got := ""
			if len(matches) >= 2 {
				got = matches[1]
			}
			if got != tt.expected {
				t.Errorf("paramPattern.FindStringSubmatch(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestQualityToStars(t *testing.T) {
	tests := []struct {
		quality  int
		expected string
	}{
		{1, "★☆☆☆☆"},
		{2, "★★☆☆☆"},
		{3, "★★★☆☆"},
		{4, "★★★★☆"},
		{5, "★★★★★"},
		{0, "☆☆☆☆☆"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := qualityToStars(tt.quality)
			if got != tt.expected {
				t.Errorf("qualityToStars(%d) = %q, want %q", tt.quality, got, tt.expected)
			}
		})
	}
}

func TestEstimateRAM(t *testing.T) {
	tests := []struct {
		name     string
		fileSize int64
		minRAM   int64
		maxRAM   int64
	}{
		// Small file - should be mostly overhead
		{"1KB file", 1024, 500 * 1024 * 1024, 600 * 1024 * 1024},

		// 1GB file
		{"1GB file", 1024 * 1024 * 1024, 1600 * 1024 * 1024, 1700 * 1024 * 1024},

		// 4GB file
		{"4GB file", 4 * 1024 * 1024 * 1024, 4900 * 1024 * 1024, 5100 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ram := estimateRAM(tt.fileSize)
			if ram < tt.minRAM || ram > tt.maxRAM {
				t.Errorf("estimateRAM(%d) = %d, expected between %d and %d", tt.fileSize, ram, tt.minRAM, tt.maxRAM)
			}
		})
	}
}

func TestAnalyzeGGUF(t *testing.T) {
	t.Run("no GGUF files", func(t *testing.T) {
		files := []FileInfo{
			{Name: "config.json", Path: "config.json", Size: 1000},
			{Name: "model.safetensors", Path: "model.safetensors", Size: 10000000},
		}

		result := analyzeGGUF(files)
		if result != nil {
			t.Error("expected nil for non-GGUF files")
		}
	})

	t.Run("single GGUF file", func(t *testing.T) {
		files := []FileInfo{
			{Name: "mistral-7b.Q4_K_M.gguf", Path: "mistral-7b.Q4_K_M.gguf", Size: 4000000000},
		}

		result := analyzeGGUF(files)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Quantizations) != 1 {
			t.Errorf("expected 1 quantization, got %d", len(result.Quantizations))
		}
		if result.Quantizations[0].Name != "Q4_K_M" {
			t.Errorf("Name = %q, want Q4_K_M", result.Quantizations[0].Name)
		}
		if result.Quantizations[0].Quality != 4 {
			t.Errorf("Quality = %d, want 4", result.Quantizations[0].Quality)
		}
	})

	t.Run("multiple GGUF files", func(t *testing.T) {
		files := []FileInfo{
			{Name: "model.Q2_K.gguf", Path: "model.Q2_K.gguf", Size: 1000000000},
			{Name: "model.Q4_K_M.gguf", Path: "model.Q4_K_M.gguf", Size: 4000000000},
			{Name: "model.Q8_0.gguf", Path: "model.Q8_0.gguf", Size: 8000000000},
		}

		result := analyzeGGUF(files)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Quantizations) != 3 {
			t.Errorf("expected 3 quantizations, got %d", len(result.Quantizations))
		}

		// Should be sorted by quality (descending)
		if result.Quantizations[0].Quality < result.Quantizations[1].Quality {
			t.Error("quantizations should be sorted by quality descending")
		}
	})

	t.Run("extracts parameter count", func(t *testing.T) {
		files := []FileInfo{
			{Name: "mistral-7b-instruct.Q4_K_M.gguf", Path: "mistral-7b-instruct.Q4_K_M.gguf", Size: 4000000000},
		}

		result := analyzeGGUF(files)
		if result.ParameterCount != "7B" {
			t.Errorf("ParameterCount = %q, want '7B'", result.ParameterCount)
		}
	})

	t.Run("unknown quantization format", func(t *testing.T) {
		files := []FileInfo{
			{Name: "model.gguf", Path: "model.gguf", Size: 4000000000}, // No quant type
		}

		result := analyzeGGUF(files)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Quantizations) != 1 {
			t.Fatalf("expected 1 quantization, got %d", len(result.Quantizations))
		}
		if result.Quantizations[0].Name != "Unknown" {
			t.Errorf("Name = %q, want 'Unknown'", result.Quantizations[0].Name)
		}
	})
}

func TestParseGGUFQuantization(t *testing.T) {
	t.Run("standard quantization", func(t *testing.T) {
		file := FileInfo{Name: "model.Q4_K_M.gguf", Path: "model.Q4_K_M.gguf", Size: 4000000000}
		quant := parseGGUFQuantization(file)

		if quant == nil {
			t.Fatal("expected non-nil")
		}
		if quant.Name != "Q4_K_M" {
			t.Errorf("Name = %q", quant.Name)
		}
		if quant.Quality != 4 {
			t.Errorf("Quality = %d", quant.Quality)
		}
		if quant.Description != "Medium 4-bit, recommended" {
			t.Errorf("Description = %q", quant.Description)
		}
		if quant.QualityStars != "★★★★☆" {
			t.Errorf("QualityStars = %q", quant.QualityStars)
		}
	})

	t.Run("importance matrix quantization", func(t *testing.T) {
		file := FileInfo{Name: "model.IQ2_XS.gguf", Path: "model.IQ2_XS.gguf", Size: 1000000000}
		quant := parseGGUFQuantization(file)

		if quant.Name != "IQ2_XS" {
			t.Errorf("Name = %q", quant.Name)
		}
		if quant.Quality != 1 {
			t.Errorf("Quality = %d", quant.Quality)
		}
	})

	t.Run("float precision", func(t *testing.T) {
		file := FileInfo{Name: "model-F16.gguf", Path: "model-F16.gguf", Size: 14000000000}
		quant := parseGGUFQuantization(file)

		if quant.Name != "F16" {
			t.Errorf("Name = %q", quant.Name)
		}
		if quant.Quality != 5 {
			t.Errorf("Quality = %d", quant.Quality)
		}
	})

	t.Run("RAM estimate included", func(t *testing.T) {
		file := FileInfo{Name: "model.Q4_K_M.gguf", Path: "model.Q4_K_M.gguf", Size: 4000000000}
		quant := parseGGUFQuantization(file)

		if quant.EstimatedRAM == 0 {
			t.Error("EstimatedRAM should be non-zero")
		}
		if quant.EstimatedRAMHuman == "" {
			t.Error("EstimatedRAMHuman should not be empty")
		}
	})
}

func TestRecommendGGUF(t *testing.T) {
	info := &GGUFInfo{
		Quantizations: []GGUFQuantization{
			{Name: "Q8_0", Quality: 5, EstimatedRAM: 10 * 1024 * 1024 * 1024}, // 10GB
			{Name: "Q5_K_M", Quality: 5, EstimatedRAM: 6 * 1024 * 1024 * 1024}, // 6GB
			{Name: "Q4_K_M", Quality: 4, EstimatedRAM: 5 * 1024 * 1024 * 1024}, // 5GB
			{Name: "Q2_K", Quality: 1, EstimatedRAM: 2 * 1024 * 1024 * 1024},   // 2GB
		},
	}

	t.Run("8GB available", func(t *testing.T) {
		available := int64(8 * 1024 * 1024 * 1024) // 8GB
		recommended := RecommendGGUF(info, available)

		// Should recommend Q5_K_M, Q4_K_M, Q2_K (not Q8_0)
		if len(recommended) != 3 {
			t.Errorf("expected 3 recommendations, got %d", len(recommended))
		}
		// First should be highest quality that fits
		if recommended[0].Name != "Q5_K_M" {
			t.Errorf("first recommendation = %q, want Q5_K_M", recommended[0].Name)
		}
		for _, r := range recommended {
			if !r.Recommended {
				t.Errorf("%s should have Recommended=true", r.Name)
			}
		}
	})

	t.Run("4GB available", func(t *testing.T) {
		available := int64(4 * 1024 * 1024 * 1024) // 4GB
		recommended := RecommendGGUF(info, available)

		// Should only recommend Q2_K
		if len(recommended) != 1 {
			t.Errorf("expected 1 recommendation, got %d", len(recommended))
		}
		if recommended[0].Name != "Q2_K" {
			t.Errorf("recommendation = %q, want Q2_K", recommended[0].Name)
		}
	})

	t.Run("16GB available", func(t *testing.T) {
		available := int64(16 * 1024 * 1024 * 1024) // 16GB
		recommended := RecommendGGUF(info, available)

		// Should recommend all
		if len(recommended) != 4 {
			t.Errorf("expected 4 recommendations, got %d", len(recommended))
		}
	})

	t.Run("1GB available", func(t *testing.T) {
		available := int64(1 * 1024 * 1024 * 1024) // 1GB
		recommended := RecommendGGUF(info, available)

		// Should recommend none
		if len(recommended) != 0 {
			t.Errorf("expected 0 recommendations, got %d", len(recommended))
		}
	})
}

func TestGGUFToSelectableItems(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		items := GGUFToSelectableItems(nil)
		if items != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("empty quantizations", func(t *testing.T) {
		info := &GGUFInfo{Quantizations: []GGUFQuantization{}}
		items := GGUFToSelectableItems(info)
		if items != nil {
			t.Error("expected nil for empty quantizations")
		}
	})

	t.Run("with quantizations", func(t *testing.T) {
		info := &GGUFInfo{
			Quantizations: []GGUFQuantization{
				{
					Name:              "Q4_K_M",
					Quality:           4,
					QualityStars:      "★★★★☆",
					Description:       "Medium 4-bit, recommended",
					EstimatedRAM:      5000000000,
					EstimatedRAMHuman: "4.7 GiB",
					File: FileInfo{
						Path:      "model.Q4_K_M.gguf",
						Size:      4000000000,
						SizeHuman: "3.7 GiB",
					},
				},
				{
					Name:              "Q8_0",
					Quality:           5,
					QualityStars:      "★★★★★",
					Description:       "8-bit, minimal loss",
					EstimatedRAM:      10000000000,
					EstimatedRAMHuman: "9.3 GiB",
					File: FileInfo{
						Path:      "model.Q8_0.gguf",
						Size:      8000000000,
						SizeHuman: "7.5 GiB",
					},
				},
			},
		}

		items := GGUFToSelectableItems(info)
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}

		// Verify first item (Q4_K_M)
		item := items[0]
		if item.ID != "q4_k_m" {
			t.Errorf("ID = %q, want 'q4_k_m'", item.ID)
		}
		if item.Label != "Q4_K_M" {
			t.Errorf("Label = %q", item.Label)
		}
		if item.Category != "quantization" {
			t.Errorf("Category = %q", item.Category)
		}
		if !item.Recommended {
			t.Error("Q4_K_M should be recommended")
		}
		if item.FilterValue != "q4_k_m" {
			t.Errorf("FilterValue = %q", item.FilterValue)
		}
		if len(item.Files) != 1 || item.Files[0] != "model.Q4_K_M.gguf" {
			t.Errorf("Files = %v", item.Files)
		}
	})

	t.Run("Q4_K_M is recommended when present", func(t *testing.T) {
		info := &GGUFInfo{
			Quantizations: []GGUFQuantization{
				{Name: "Q4_K_M", Quality: 4, File: FileInfo{Path: "model.Q4_K_M.gguf", Size: 4000000000}},
				{Name: "Q8_0", Quality: 5, File: FileInfo{Path: "model.Q8_0.gguf", Size: 8000000000}},
			},
		}

		items := GGUFToSelectableItems(info)

		var q4km, q8 *SelectableItem
		for i := range items {
			if items[i].Label == "Q4_K_M" {
				q4km = &items[i]
			}
			if items[i].Label == "Q8_0" {
				q8 = &items[i]
			}
		}

		if q4km == nil || !q4km.Recommended {
			t.Error("Q4_K_M should be recommended")
		}
		if q8 != nil && q8.Recommended {
			t.Error("Q8_0 should not be recommended when Q4_K_M exists")
		}
	})
}
