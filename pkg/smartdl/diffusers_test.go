// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"strings"
	"testing"
)

func TestPipelineDescriptions(t *testing.T) {
	expected := map[string]string{
		"StableDiffusionPipeline":        "Stable Diffusion v1.x text-to-image",
		"StableDiffusionXLPipeline":      "Stable Diffusion XL text-to-image",
		"FluxPipeline":                   "Flux text-to-image",
		"StableVideoDiffusionPipeline":   "Stable Video Diffusion",
		"LatentConsistencyModelPipeline": "LCM fast inference",
	}

	for pipeline, desc := range expected {
		t.Run(pipeline, func(t *testing.T) {
			got := pipelineDescriptions[pipeline]
			if got != desc {
				t.Errorf("pipelineDescriptions[%q] = %q, want %q", pipeline, got, desc)
			}
		})
	}
}

func TestRequiredComponents(t *testing.T) {
	t.Run("StableDiffusionPipeline", func(t *testing.T) {
		required := requiredComponents["StableDiffusionPipeline"]
		expected := []string{"unet", "vae", "text_encoder", "tokenizer", "scheduler"}

		if len(required) != len(expected) {
			t.Errorf("got %d components, want %d", len(required), len(expected))
		}

		for _, exp := range expected {
			found := false
			for _, r := range required {
				if r == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing required component %q", exp)
			}
		}
	})

	t.Run("StableDiffusionXLPipeline", func(t *testing.T) {
		required := requiredComponents["StableDiffusionXLPipeline"]
		// SDXL has dual encoders
		if len(required) < 6 {
			t.Errorf("SDXL should have at least 6 components, got %d", len(required))
		}
	})

	t.Run("FluxPipeline", func(t *testing.T) {
		required := requiredComponents["FluxPipeline"]
		// Flux uses transformer instead of unet
		hasTransformer := false
		for _, r := range required {
			if r == "transformer" {
				hasTransformer = true
				break
			}
		}
		if !hasTransformer {
			t.Error("FluxPipeline should have 'transformer' component")
		}
	})
}

func TestAnalyzeDiffusers(t *testing.T) {
	t.Run("with model_index.json", func(t *testing.T) {
		files := []FileInfo{
			{Name: "model_index.json", Path: "model_index.json", Size: 500},
			{Name: "diffusion_pytorch_model.safetensors", Path: "unet/diffusion_pytorch_model.safetensors", Size: 3400000000},
			{Name: "diffusion_pytorch_model.safetensors", Path: "vae/diffusion_pytorch_model.safetensors", Size: 330000000},
			{Name: "model.safetensors", Path: "text_encoder/model.safetensors", Size: 500000000},
		}

		metadata := map[string]interface{}{
			"model_index.json": map[string]interface{}{
				"_class_name":        "StableDiffusionPipeline",
				"_diffusers_version": "0.25.0",
				"unet":               []interface{}{"diffusers", "UNet2DConditionModel"},
				"vae":                []interface{}{"diffusers", "AutoencoderKL"},
				"text_encoder":       []interface{}{"transformers", "CLIPTextModel"},
				"tokenizer":          []interface{}{"transformers", "CLIPTokenizer"},
				"scheduler":          []interface{}{"diffusers", "PNDMScheduler"},
			},
		}

		info := analyzeDiffusers(files, metadata)
		if info == nil {
			t.Fatal("expected non-nil result")
		}

		if info.PipelineType != "StableDiffusionPipeline" {
			t.Errorf("PipelineType = %q", info.PipelineType)
		}

		if info.DiffusersVersion != "0.25.0" {
			t.Errorf("DiffusersVersion = %q", info.DiffusersVersion)
		}

		if len(info.Components) < 4 {
			t.Errorf("expected at least 4 components, got %d", len(info.Components))
		}

		// Check unet component
		var unet *DiffusersComponent
		for i := range info.Components {
			if info.Components[i].Name == "unet" {
				unet = &info.Components[i]
				break
			}
		}
		if unet == nil {
			t.Fatal("unet component not found")
		}
		if unet.Library != "diffusers" {
			t.Errorf("unet.Library = %q", unet.Library)
		}
		if unet.ClassName != "UNet2DConditionModel" {
			t.Errorf("unet.ClassName = %q", unet.ClassName)
		}
		if !unet.Required {
			t.Error("unet should be required")
		}
	})

	t.Run("empty metadata", func(t *testing.T) {
		files := []FileInfo{}
		metadata := map[string]interface{}{}

		info := analyzeDiffusers(files, metadata)
		if info == nil {
			t.Fatal("expected non-nil result")
		}
		if info.PipelineType != "" {
			t.Errorf("PipelineType should be empty, got %q", info.PipelineType)
		}
	})

	t.Run("unknown pipeline type", func(t *testing.T) {
		metadata := map[string]interface{}{
			"model_index.json": map[string]interface{}{
				"_class_name": "CustomPipeline",
			},
		}

		info := analyzeDiffusers(nil, metadata)
		if info.PipelineType != "CustomPipeline" {
			t.Errorf("PipelineType = %q", info.PipelineType)
		}
		if info.PipelineDescription != "Diffusers pipeline" {
			t.Errorf("PipelineDescription = %q", info.PipelineDescription)
		}
	})
}

func TestCalculateComponentSize(t *testing.T) {
	files := []FileInfo{
		{Path: "unet/diffusion_pytorch_model.safetensors", Size: 3400000000},
		{Path: "unet/config.json", Size: 1000},
		{Path: "vae/diffusion_pytorch_model.safetensors", Size: 330000000},
		{Path: "text_encoder/model.safetensors", Size: 500000000},
		{Path: "config.json", Size: 500}, // Root file, not in any component
	}

	tests := []struct {
		component string
		expected  int64
	}{
		{"unet", 3400001000},
		{"vae", 330000000},
		{"text_encoder", 500000000},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			size := calculateComponentSize(files, tt.component)
			if size != tt.expected {
				t.Errorf("calculateComponentSize(%q) = %d, want %d", tt.component, size, tt.expected)
			}
		})
	}
}

func TestDetectVariants(t *testing.T) {
	tests := []struct {
		name     string
		files    []FileInfo
		expected []string
	}{
		{
			name: "fp16 in filename",
			files: []FileInfo{
				{Name: "model.fp16.safetensors", Path: "unet/model.fp16.safetensors"},
			},
			expected: []string{"fp16"},
		},
		{
			name: "fp16 in directory",
			files: []FileInfo{
				{Name: "model.safetensors", Path: "unet/fp16/model.safetensors", Directory: "unet/fp16"},
			},
			expected: []string{"fp16"},
		},
		{
			name: "multiple variants",
			files: []FileInfo{
				{Name: "model.fp16.safetensors", Path: "model.fp16.safetensors"},
				{Name: "model.fp32.safetensors", Path: "model.fp32.safetensors"},
				{Name: "model.bf16.safetensors", Path: "model.bf16.safetensors"},
			},
			expected: []string{"fp16", "fp32", "bf16"},
		},
		{
			name:     "no variants",
			files:    []FileInfo{{Name: "model.safetensors", Path: "model.safetensors"}},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectVariants(tt.files)
			if len(tt.expected) == 0 && len(result) == 0 {
				return // Both empty, OK
			}

			for _, exp := range tt.expected {
				found := false
				for _, r := range result {
					if r == exp {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing variant %q in result %v", exp, result)
				}
			}
		})
	}
}

func TestDetectPrecisions(t *testing.T) {
	tests := []struct {
		name    string
		files   []FileInfo
		hasFP32 bool
	}{
		{
			name: "standard safetensors",
			files: []FileInfo{
				{Name: "model.safetensors"},
			},
			hasFP32: true,
		},
		{
			name: "fp16 safetensors",
			files: []FileInfo{
				{Name: "model_fp16.safetensors"},
			},
			hasFP32: false, // Has fp16 indicator
		},
		{
			name: "pytorch bin",
			files: []FileInfo{
				{Name: "pytorch_model.bin"},
			},
			hasFP32: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectPrecisions(tt.files)

			hasFP32 := false
			for _, p := range result {
				if p == "fp32" {
					hasFP32 = true
					break
				}
			}

			if hasFP32 != tt.hasFP32 {
				t.Errorf("hasFP32 = %v, want %v (result: %v)", hasFP32, tt.hasFP32, result)
			}
		})
	}
}

func TestGetComponentFiles(t *testing.T) {
	files := []FileInfo{
		{Path: "unet/diffusion_pytorch_model.safetensors", Name: "diffusion_pytorch_model.safetensors"},
		{Path: "unet/config.json", Name: "config.json"},
		{Path: "vae/diffusion_pytorch_model.safetensors", Name: "diffusion_pytorch_model.safetensors"},
		{Path: "config.json", Name: "config.json"},
	}

	t.Run("unet component", func(t *testing.T) {
		result := GetComponentFiles(files, "unet")
		if len(result) != 2 {
			t.Errorf("expected 2 files, got %d", len(result))
		}
	})

	t.Run("vae component", func(t *testing.T) {
		result := GetComponentFiles(files, "vae")
		if len(result) != 1 {
			t.Errorf("expected 1 file, got %d", len(result))
		}
	})

	t.Run("nonexistent component", func(t *testing.T) {
		result := GetComponentFiles(files, "nonexistent")
		if len(result) != 0 {
			t.Errorf("expected 0 files, got %d", len(result))
		}
	})
}

func TestDiffusersToSelectableItems(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		items := DiffusersToSelectableItems(nil)
		if items != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("components select their chosen weights", func(t *testing.T) {
		info := &DiffusersInfo{
			PipelineType: "StableDiffusionPipeline",
			Variants:     []string{"fp16", "fp32"},
			Components: []DiffusersComponent{
				{Name: "unet", ClassName: "UNet2DConditionModel", Size: 3400000000, Required: true,
					WeightFiles: []string{"unet/diffusion_pytorch_model.fp16.safetensors"}, WeightSize: 1700000000},
				{Name: "scheduler", ClassName: "PNDMScheduler", Required: true},
			},
		}

		items := DiffusersToSelectableItems(info)
		if len(items) != 2 { // one per component; variants are not OR-filter items
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		unet, sched := items[0], items[1]
		if unet.FilterValue != "unet/diffusion_pytorch_model.fp16.safetensors" || unet.Size != 1700000000 || !unet.Recommended {
			t.Errorf("unet item = %+v", unet)
		}
		if sched.FilterValue != "scheduler/" {
			t.Errorf("weightless component filter = %q, want the folder", sched.FilterValue)
		}
	})
}

// SDXL-style component folders ship several weight formats side by side; only
// one should be chosen (the old folder filter pulled ~39 GiB instead of ~7).
func TestChooseComponentWeights(t *testing.T) {
	lfs := func(p string) FileInfo {
		return FileInfo{Path: p, Name: p[strings.LastIndex(p, "/")+1:], IsLFS: true, Size: 100}
	}
	files := []FileInfo{
		lfs("unet/diffusion_pytorch_model.safetensors"),
		lfs("unet/diffusion_pytorch_model.fp16.safetensors"),
		lfs("unet/diffusion_pytorch_model.bin"),
		lfs("unet/diffusion_pytorch_model.msgpack"),
		lfs("unet/openvino_model.bin"),
		lfs("unet/model.onnx"),
		lfs("text_encoder/model-00001-of-00002.safetensors"),
		lfs("text_encoder/model-00002-of-00002.safetensors"),
		lfs("text_encoder/flax_model.msgpack"),
		lfs("vae/diffusion_pytorch_model.bin"),
		lfs("vae/diffusion_pytorch_model.msgpack"),
		{Path: "unet/config.json", Name: "config.json"},
	}
	paths := func(fs []FileInfo) string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Path)
		}
		return strings.Join(out, ",")
	}
	cases := map[string]string{
		"unet":         "unet/diffusion_pytorch_model.fp16.safetensors",
		"text_encoder": "text_encoder/model-00001-of-00002.safetensors,text_encoder/model-00002-of-00002.safetensors",
		"vae":          "vae/diffusion_pytorch_model.bin",
		"tokenizer":    "",
	}
	for comp, want := range cases {
		if got := paths(chooseComponentWeights(files, comp)); got != want {
			t.Errorf("%s: chose %q, want %q", comp, got, want)
		}
	}
}

func TestCalculateDownloadSize(t *testing.T) {
	info := &DiffusersInfo{
		Components: []DiffusersComponent{
			{Name: "unet"},
			{Name: "vae"},
		},
	}

	files := []FileInfo{
		{Path: "unet/model.safetensors", Name: "model.safetensors", Size: 3000000000},
		{Path: "unet/model.fp16.safetensors", Name: "model.fp16.safetensors", Size: 1500000000},
		{Path: "vae/model.safetensors", Name: "model.safetensors", Size: 300000000},
		{Path: "text_encoder/model.safetensors", Name: "model.safetensors", Size: 500000000},
	}

	t.Run("all components no variant", func(t *testing.T) {
		size := CalculateDownloadSize(info, files, []string{"unet", "vae"}, "")
		// Should include unet + vae files
		if size < 3000000000 {
			t.Errorf("size = %d, expected > 3GB", size)
		}
	})

	t.Run("single component", func(t *testing.T) {
		size := CalculateDownloadSize(info, files, []string{"vae"}, "")
		if size != 300000000 {
			t.Errorf("size = %d, expected 300000000", size)
		}
	})
}
