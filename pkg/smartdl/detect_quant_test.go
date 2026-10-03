// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import "testing"

// PyTorch repos that also ship an onnx/ export (gpt2, all-MiniLM-L6-v2,
// SmolVLM) were labeled ONNX because ONNX was checked first.
func TestDetectType_TransformersBeforeONNX(t *testing.T) {
	a := &Analyzer{}
	withONNX := []FileInfo{
		{Path: "config.json", Name: "config.json"},
		{Path: "model.safetensors", Name: "model.safetensors"},
		{Path: "onnx/model.onnx", Name: "model.onnx"},
		{Path: "onnx/config.json", Name: "config.json"},
	}
	if got := a.detectType(withONNX, false); got != TypeTransformers {
		t.Errorf("repo with PyTorch weights and onnx/ = %s, want transformers", got)
	}
	onnxOnly := []FileInfo{
		{Path: "onnx/model.onnx", Name: "model.onnx"},
		{Path: "onnx/config.json", Name: "config.json"},
		{Path: "onnx/model.safetensors", Name: "model.safetensors"},
	}
	if got := a.detectType(onnxOnly, false); got != TypeONNX {
		t.Errorf("ONNX-only repo (config only in a subfolder) = %s, want onnx", got)
	}
}

// Quantization settings in the shapes real repos use.
func TestAnalyzeQuantized_ConfigShapes(t *testing.T) {
	cfg := func(k string, v map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"config.json": map[string]interface{}{k: v}}
	}
	cases := []struct {
		name     string
		metadata map[string]interface{}
		method   string
		repoType RepoType
		bits     int
		bpw      float64
	}{
		{"AWQ in quantization_config (Qwen2.5-AWQ)", cfg("quantization_config", map[string]interface{}{"quant_method": "awq", "bits": 4.0, "group_size": 128.0}), "awq", TypeAWQ, 4, 0},
		{"GPTQ in quantization_config", cfg("quantization_config", map[string]interface{}{"quant_method": "gptq", "bits": 4.0}), "gptq", TypeGPTQ, 4, 0},
		{"old quantize_config.json without quant_method (TheBloke)", map[string]interface{}{"quantize_config.json": map[string]interface{}{"bits": 4.0, "group_size": 128.0}}, "gptq", TypeGPTQ, 4, 0},
		{"bitsandbytes 4-bit (unsloth bnb-4bit)", cfg("quantization_config", map[string]interface{}{"quant_method": "bitsandbytes", "load_in_4bit": true}), "bitsandbytes", TypeQuantized, 4, 0},
		{"bitsandbytes without quant_method", cfg("quantization_config", map[string]interface{}{"load_in_4bit": true}), "bitsandbytes", TypeQuantized, 4, 0},
		{"MLX quantization block (mlx-community)", cfg("quantization", map[string]interface{}{"group_size": 64.0, "bits": 4.0}), "mlx", TypeQuantized, 4, 0},
		{"EXL3 fractional bits", cfg("quantization_config", map[string]interface{}{"quant_method": "exl3", "bits": 2.51, "head_bits": 6.0}), "exl3", TypeQuantized, 2, 2.51},
		{"FP8", cfg("quantization_config", map[string]interface{}{"quant_method": "fp8"}), "fp8", TypeQuantized, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := analyzeQuantized(tc.metadata)
			if q == nil {
				t.Fatal("not detected as quantized")
			}
			if q.Method != tc.method || quantizedRepoType(q.Method) != tc.repoType || q.Bits != tc.bits || q.BitsPerWeight != tc.bpw {
				t.Errorf("method=%q type=%s bits=%d bpw=%v; want %q %s %d %v",
					q.Method, quantizedRepoType(q.Method), q.Bits, q.BitsPerWeight, tc.method, tc.repoType, tc.bits, tc.bpw)
			}
			if len(q.Backends) == 0 && tc.method != "" {
				t.Errorf("no backends for %s", tc.method)
			}
		})
	}
	if q := analyzeQuantized(map[string]interface{}{"config.json": map[string]interface{}{"hidden_size": 768.0}}); q != nil {
		t.Errorf("plain config detected as quantized: %+v", q)
	}
}
