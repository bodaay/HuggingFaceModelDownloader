// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"testing"
)

func TestParseTransformersConfig(t *testing.T) {
	t.Run("full config", func(t *testing.T) {
		cfg := map[string]interface{}{
			"architectures":          []interface{}{"LlamaForCausalLM"},
			"model_type":             "llama",
			"hidden_size":            float64(4096),
			"num_hidden_layers":      float64(32),
			"num_attention_heads":    float64(32),
			"intermediate_size":      float64(11008),
			"vocab_size":             float64(32000),
			"max_position_embeddings": float64(4096),
			"torch_dtype":            "float16",
			"bos_token_id":           float64(1),
			"eos_token_id":           float64(2),
		}

		info := &TransformersInfo{}
		parseTransformersConfig(cfg, info)

		if info.Architecture != "LlamaForCausalLM" {
			t.Errorf("Architecture = %q", info.Architecture)
		}
		if info.ModelType != "llama" {
			t.Errorf("ModelType = %q", info.ModelType)
		}
		if info.HiddenSize != 4096 {
			t.Errorf("HiddenSize = %d", info.HiddenSize)
		}
		if info.NumHiddenLayers != 32 {
			t.Errorf("NumHiddenLayers = %d", info.NumHiddenLayers)
		}
		if info.NumAttentionHeads != 32 {
			t.Errorf("NumAttentionHeads = %d", info.NumAttentionHeads)
		}
		if info.IntermediateSize != 11008 {
			t.Errorf("IntermediateSize = %d", info.IntermediateSize)
		}
		if info.VocabSize != 32000 {
			t.Errorf("VocabSize = %d", info.VocabSize)
		}
		if info.MaxPositionEmbeddings != 4096 {
			t.Errorf("MaxPositionEmbeddings = %d", info.MaxPositionEmbeddings)
		}
		if info.ContextLength != 4096 {
			t.Errorf("ContextLength = %d", info.ContextLength)
		}
		if info.TorchDtype != "float16" {
			t.Errorf("TorchDtype = %q", info.TorchDtype)
		}
		if info.Precision != "fp16" {
			t.Errorf("Precision = %q", info.Precision)
		}
	})

	t.Run("sliding window context", func(t *testing.T) {
		cfg := map[string]interface{}{
			"max_position_embeddings": float64(32768),
			"sliding_window":          float64(4096),
		}

		info := &TransformersInfo{}
		parseTransformersConfig(cfg, info)

		if info.ContextLength != 4096 {
			t.Errorf("ContextLength should be sliding_window, got %d", info.ContextLength)
		}
	})

	t.Run("special tokens", func(t *testing.T) {
		cfg := map[string]interface{}{
			"bos_token_id": float64(1),
			"eos_token_id": float64(2),
			"pad_token_id": float64(0),
		}

		info := &TransformersInfo{}
		parseTransformersConfig(cfg, info)

		if info.SpecialTokens == nil {
			t.Fatal("SpecialTokens should not be nil")
		}
		if len(info.SpecialTokens) != 3 {
			t.Errorf("SpecialTokens count = %d", len(info.SpecialTokens))
		}
	})
}

func TestParseTokenizerConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"tokenizer_class":   "LlamaTokenizerFast",
		"vocab_size":        float64(32000),
		"model_max_length":  float64(4096),
		"padding_side":      "left",
		"truncation_side":   "right",
		"add_bos_token":     true,
		"add_eos_token":     false,
		"chat_template":     "{% for message in messages %}...",
	}

	tok := parseTokenizerConfig(cfg)

	if tok.Type != "LlamaTokenizerFast" {
		t.Errorf("Type = %q", tok.Type)
	}
	if tok.VocabSize != 32000 {
		t.Errorf("VocabSize = %d", tok.VocabSize)
	}
	if tok.ModelMaxLength != 4096 {
		t.Errorf("ModelMaxLength = %d", tok.ModelMaxLength)
	}
	if tok.PaddingSide != "left" {
		t.Errorf("PaddingSide = %q", tok.PaddingSide)
	}
	if tok.TruncationSide != "right" {
		t.Errorf("TruncationSide = %q", tok.TruncationSide)
	}
	if !tok.AddBosToken {
		t.Error("AddBosToken should be true")
	}
	if tok.AddEosToken {
		t.Error("AddEosToken should be false")
	}
	if !tok.HasChatTemplate {
		t.Error("HasChatTemplate should be true")
	}
	if tok.ChatTemplate == "" {
		t.Error("ChatTemplate should not be empty")
	}
}

func TestAnalyzeWeightFiles(t *testing.T) {
	files := []FileInfo{
		{Name: "model.safetensors", Path: "model.safetensors", Size: 14000000000, SizeHuman: "13 GiB"},
		{Name: "model-00001-of-00004.safetensors", Path: "model-00001-of-00004.safetensors", Size: 4000000000},
		{Name: "model-00002-of-00004.safetensors", Path: "model-00002-of-00004.safetensors", Size: 4000000000},
		{Name: "pytorch_model.bin", Path: "pytorch_model.bin", Size: 14000000000},
		{Name: "config.json", Path: "config.json", Size: 1000}, // Not a weight file
		{Name: "optimizer.pt", Path: "optimizer.pt", Size: 5000000000}, // Should be skipped
	}

	weights := analyzeWeightFiles(files)

	// Should have 4 weight files (not config.json or optimizer)
	if len(weights) != 4 {
		t.Errorf("expected 4 weight files, got %d", len(weights))
	}

	// Check format detection
	var hasSafetensors, hasBin bool
	for _, wf := range weights {
		if wf.Format == "safetensors" {
			hasSafetensors = true
		}
		if wf.Format == "pytorch_bin" {
			hasBin = true
		}
	}

	if !hasSafetensors {
		t.Error("should detect safetensors format")
	}
	if !hasBin {
		t.Error("should detect pytorch_bin format")
	}

	// Check shard detection
	var shardedFile *WeightFile
	for i := range weights {
		if weights[i].ShardTotal > 0 {
			shardedFile = &weights[i]
			break
		}
	}

	if shardedFile == nil {
		t.Error("should detect sharded files")
	} else {
		if shardedFile.ShardIndex != 1 {
			t.Errorf("ShardIndex = %d", shardedFile.ShardIndex)
		}
		if shardedFile.ShardTotal != 4 {
			t.Errorf("ShardTotal = %d", shardedFile.ShardTotal)
		}
	}
}

func TestDetectPrecisionFromFiles(t *testing.T) {
	tests := []struct {
		name     string
		files    []FileInfo
		expected string
	}{
		{
			name: "fp16 in name",
			files: []FileInfo{
				{Name: "model_fp16.safetensors"},
			},
			expected: "fp16",
		},
		{
			name: "float16 in name",
			files: []FileInfo{
				{Name: "model_float16.safetensors"},
			},
			expected: "fp16",
		},
		{
			name: "bf16 in name",
			files: []FileInfo{
				{Name: "model_bf16.safetensors"},
			},
			expected: "bf16",
		},
		{
			name: "fp32 in name",
			files: []FileInfo{
				{Name: "model_fp32.safetensors"},
			},
			expected: "fp32",
		},
		{
			name: "no precision indicator",
			files: []FileInfo{
				{Name: "model.safetensors"},
			},
			expected: "fp32", // Default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectPrecisionFromFiles(tt.files)
			if result != tt.expected {
				t.Errorf("detectPrecisionFromFiles() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDetectSharding(t *testing.T) {
	t.Run("explicit sharding", func(t *testing.T) {
		info := &TransformersInfo{
			WeightFiles: []WeightFile{
				{Name: "model-00001-of-00004.safetensors", ShardIndex: 1, ShardTotal: 4},
				{Name: "model-00002-of-00004.safetensors", ShardIndex: 2, ShardTotal: 4},
			},
		}

		detectSharding(info)

		if !info.IsSharded {
			t.Error("IsSharded should be true")
		}
		if info.ShardCount != 4 {
			t.Errorf("ShardCount = %d", info.ShardCount)
		}
	})

	t.Run("implicit sharding (multiple files)", func(t *testing.T) {
		info := &TransformersInfo{
			WeightFiles: []WeightFile{
				{Name: "model-part1.safetensors", Format: "safetensors"},
				{Name: "model-part2.safetensors", Format: "safetensors"},
				{Name: "model-part3.safetensors", Format: "safetensors"},
			},
		}

		detectSharding(info)

		if !info.IsSharded {
			t.Error("IsSharded should be true")
		}
		if info.ShardCount != 3 {
			t.Errorf("ShardCount = %d", info.ShardCount)
		}
	})

	t.Run("not sharded", func(t *testing.T) {
		info := &TransformersInfo{
			WeightFiles: []WeightFile{
				{Name: "model.safetensors", Format: "safetensors"},
			},
		}

		detectSharding(info)

		if info.IsSharded {
			t.Error("IsSharded should be false")
		}
	})

	t.Run("empty weight files", func(t *testing.T) {
		info := &TransformersInfo{}
		detectSharding(info)
		if info.IsSharded {
			t.Error("IsSharded should be false for empty")
		}
	})
}

func TestEstimateParameters(t *testing.T) {
	tests := []struct {
		name    string
		info    *TransformersInfo
		minSize int64
		maxSize int64
	}{
		{
			name: "7B model",
			info: &TransformersInfo{
				HiddenSize:      4096,
				NumHiddenLayers: 32,
				VocabSize:       32000,
			},
			minSize: 5_000_000_000,
			maxSize: 10_000_000_000,
		},
		{
			name: "1B model",
			info: &TransformersInfo{
				HiddenSize:      2048,
				NumHiddenLayers: 24,
				VocabSize:       50000,
			},
			minSize: 500_000_000,
			maxSize: 2_000_000_000,
		},
		{
			name: "missing dimensions",
			info: &TransformersInfo{
				HiddenSize:      0,
				NumHiddenLayers: 32,
			},
			minSize: 0,
			maxSize: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimateParameters(tt.info)
			if result < tt.minSize || result > tt.maxSize {
				t.Errorf("estimateParameters() = %d, want between %d and %d", result, tt.minSize, tt.maxSize)
			}
		})
	}
}

func TestFormatParameterCount(t *testing.T) {
	tests := []struct {
		params   int64
		expected string
	}{
		{0, ""},
		{1_000_000, "1M"},
		{7_000_000_000, "7.0B"},
		{70_000_000_000, "70.0B"},
		{500_000_000, "500M"},
		{1_500_000_000, "1.5B"},
		{100_000_000_000, "100B"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatParameterCount(tt.params)
			if result != tt.expected {
				t.Errorf("formatParameterCount(%d) = %q, want %q", tt.params, result, tt.expected)
			}
		})
	}
}

func TestNormalizePrecision(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"float16", "fp16"},
		{"torch.float16", "fp16"},
		{"bfloat16", "bf16"},
		{"torch.bfloat16", "bf16"},
		{"float32", "fp32"},
		{"torch.float32", "fp32"},
		{"float64", "fp64"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizePrecision(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePrecision(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestInferTaskFromArchitecture(t *testing.T) {
	tests := []struct {
		arch         string
		expectedTask string
	}{
		{"LlamaForCausalLM", "text-generation"},
		{"MistralForCausalLM", "text-generation"},
		{"GPT2LMHeadModel", "text-generation"},
		{"BertForSequenceClassification", "text-classification"},
		{"BertForTokenClassification", "token-classification"},
		{"BertForQuestionAnswering", "question-answering"},
		{"BertForMaskedLM", "fill-mask"},
		{"T5ForConditionalGeneration", "text2text-generation"},
		{"BartForConditionalGeneration", "text2text-generation"},
		{"BertModel", "feature-extraction"},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			task, _ := inferTaskFromArchitecture(tt.arch)
			if task != tt.expectedTask {
				t.Errorf("inferTaskFromArchitecture(%q) task = %q, want %q", tt.arch, task, tt.expectedTask)
			}
		})
	}
}

func TestDescribeArchitecture(t *testing.T) {
	tests := []struct {
		arch     string
		contains string
	}{
		{"LlamaForCausalLM", "Llama"},
		{"MistralForCausalLM", "Mistral"},
		{"BertForSequenceClassification", "BERT"},
		{"GPT2LMHeadModel", "GPT-2"},
		{"UnknownForCausalLM", "Decoder"},
		{"UnknownModel", "Transformer"},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			desc := describeArchitecture(tt.arch)
			if desc == "" {
				t.Error("description should not be empty")
			}
		})
	}
}

func TestTransformersToSelectableItems(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		items := TransformersToSelectableItems(nil, nil)
		if items != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("with both formats", func(t *testing.T) {
		info := &TransformersInfo{
			WeightFiles: []WeightFile{
				{Name: "model.safetensors", Format: "safetensors", Size: 14000000000},
				{Name: "pytorch_model.bin", Format: "pytorch_bin", Size: 14000000000},
			},
		}

		items := TransformersToSelectableItems(info, nil)
		if len(items) < 2 {
			t.Errorf("expected at least 2 items, got %d", len(items))
		}

		var safetensorsItem *SelectableItem
		for i := range items {
			if items[i].ID == "safetensors" {
				safetensorsItem = &items[i]
				break
			}
		}

		if safetensorsItem == nil {
			t.Fatal("safetensors item not found")
		}
		if !safetensorsItem.Recommended {
			t.Error("safetensors should be recommended")
		}
		if safetensorsItem.Quality != 5 {
			t.Errorf("safetensors quality = %d, want 5", safetensorsItem.Quality)
		}
	})

	t.Run("only safetensors", func(t *testing.T) {
		info := &TransformersInfo{
			WeightFiles: []WeightFile{
				{Name: "model.safetensors", Format: "safetensors", Size: 14000000000},
			},
		}

		items := TransformersToSelectableItems(info, nil)
		// Should not add format selection when only one format
		for _, item := range items {
			if item.Category == "format" {
				t.Error("should not have format items when only one format available")
			}
		}
	})

	t.Run("with precision variants", func(t *testing.T) {
		info := &TransformersInfo{}
		files := []FileInfo{
			{Path: "model_fp16.safetensors", Name: "model_fp16.safetensors"},
			{Path: "model_fp32.safetensors", Name: "model_fp32.safetensors"},
		}

		items := TransformersToSelectableItems(info, files)

		var hasFP16, hasFP32 bool
		for _, item := range items {
			if item.ID == "fp16" {
				hasFP16 = true
				if !item.Recommended {
					t.Error("fp16 should be recommended")
				}
			}
			if item.ID == "fp32" {
				hasFP32 = true
			}
		}

		if !hasFP16 {
			t.Error("should have fp16 item")
		}
		if !hasFP32 {
			t.Error("should have fp32 item")
		}
	})
}

func TestDetectPrecisionVariants(t *testing.T) {
	files := []FileInfo{
		{Path: "model_fp16.safetensors"},
		{Path: "model_fp32.safetensors"},
		{Path: "model_bf16.safetensors"},
		{Path: "model.safetensors"}, // No precision indicator
	}

	variants := detectPrecisionVariants(files)

	expected := map[string]bool{"fp16": true, "fp32": true, "bf16": true}
	for _, v := range variants {
		if !expected[v] {
			t.Errorf("unexpected variant %q", v)
		}
		delete(expected, v)
	}

	if len(expected) > 0 {
		t.Errorf("missing variants: %v", expected)
	}
}

func TestAnalyzeTransformers(t *testing.T) {
	files := []FileInfo{
		{Name: "model.safetensors", Path: "model.safetensors", Size: 14000000000, SizeHuman: "13 GiB"},
		{Name: "config.json", Path: "config.json", Size: 1000},
		{Name: "tokenizer_config.json", Path: "tokenizer_config.json", Size: 500},
	}

	metadata := map[string]interface{}{
		"config.json": map[string]interface{}{
			"architectures":          []interface{}{"LlamaForCausalLM"},
			"model_type":             "llama",
			"hidden_size":            float64(4096),
			"num_hidden_layers":      float64(32),
			"vocab_size":             float64(32000),
			"torch_dtype":            "float16",
		},
		"tokenizer_config.json": map[string]interface{}{
			"tokenizer_class":  "LlamaTokenizerFast",
			"model_max_length": float64(4096),
		},
	}

	info := analyzeTransformers(files, metadata)

	if info == nil {
		t.Fatal("expected non-nil result")
	}

	if info.Architecture != "LlamaForCausalLM" {
		t.Errorf("Architecture = %q", info.Architecture)
	}

	if info.Tokenizer == nil {
		t.Fatal("Tokenizer should not be nil")
	}

	if info.Tokenizer.Type != "LlamaTokenizerFast" {
		t.Errorf("Tokenizer.Type = %q", info.Tokenizer.Type)
	}

	if len(info.WeightFiles) == 0 {
		t.Error("WeightFiles should not be empty")
	}

	if info.Precision != "fp16" {
		t.Errorf("Precision = %q", info.Precision)
	}

	if info.Task == "" {
		t.Error("Task should be inferred")
	}

	// Check that backends are set
	if len(info.Backends) == 0 {
		t.Error("Backends should not be empty")
	}
}
