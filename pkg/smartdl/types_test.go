// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"testing"
)

func TestRepoType_String(t *testing.T) {
	tests := []struct {
		repoType RepoType
		expected string
	}{
		{TypeGGUF, "gguf"},
		{TypeTransformers, "transformers"},
		{TypeDiffusers, "diffusers"},
		{TypeLoRA, "lora"},
		{TypeGPTQ, "gptq"},
		{TypeAWQ, "awq"},
		{TypeONNX, "onnx"},
		{TypeDataset, "dataset"},
		{TypeAudio, "audio"},
		{TypeVision, "vision"},
		{TypeMultimodal, "multimodal"},
		{TypeGeneric, "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.repoType.String() != tt.expected {
				t.Errorf("String() = %q, want %q", tt.repoType.String(), tt.expected)
			}
		})
	}
}

func TestRepoType_Description(t *testing.T) {
	tests := []struct {
		repoType RepoType
		contains string
	}{
		{TypeGGUF, "GGUF"},
		{TypeTransformers, "Transformers"},
		{TypeDiffusers, "Diffusers"},
		{TypeLoRA, "LoRA"},
		{TypeGPTQ, "GPTQ"},
		{TypeAWQ, "AWQ"},
		{TypeONNX, "ONNX"},
		{TypeDataset, "dataset"},
		{TypeAudio, "Audio"},
		{TypeVision, "Vision"},
		{TypeMultimodal, "Multimodal"},
		{TypeGeneric, "Generic"},
	}

	for _, tt := range tests {
		t.Run(string(tt.repoType), func(t *testing.T) {
			desc := tt.repoType.Description()
			if desc == "" {
				t.Error("Description should not be empty")
			}
			// Just verify it returns a non-empty string
		})
	}
}

func TestRepoInfo_GenerateCLICommand(t *testing.T) {
	t.Run("basic model", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/repo"}
		cmd := info.GenerateCLICommand(nil)
		if cmd != "hfdownloader download owner/repo" {
			t.Errorf("cmd = %q", cmd)
		}
	})

	t.Run("dataset", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/repo", IsDataset: true}
		cmd := info.GenerateCLICommand(nil)
		if cmd != "hfdownloader download owner/repo --dataset" {
			t.Errorf("cmd = %q", cmd)
		}
	})

	t.Run("with branch", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/repo", Branch: "dev"}
		cmd := info.GenerateCLICommand(nil)
		if cmd != "hfdownloader download owner/repo -b dev" {
			t.Errorf("cmd = %q", cmd)
		}
	})

	t.Run("main branch omitted", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/repo", Branch: "main"}
		cmd := info.GenerateCLICommand(nil)
		if cmd != "hfdownloader download owner/repo" {
			t.Errorf("cmd = %q, branch 'main' should be omitted", cmd)
		}
	})

	t.Run("with filters", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/repo"}
		cmd := info.GenerateCLICommand([]string{"q4_k_m"})
		if cmd != "hfdownloader download owner/repo -F q4_k_m" {
			t.Errorf("cmd = %q", cmd)
		}
	})

	t.Run("with multiple filters", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/repo"}
		cmd := info.GenerateCLICommand([]string{"q4_k_m", "q5_k_m"})
		if cmd != "hfdownloader download owner/repo -F q4_k_m,q5_k_m" {
			t.Errorf("cmd = %q", cmd)
		}
	})

	t.Run("dataset with branch and filters", func(t *testing.T) {
		info := &RepoInfo{Repo: "owner/dataset", IsDataset: true, Branch: "v2"}
		cmd := info.GenerateCLICommand([]string{"train"})
		if cmd != "hfdownloader download owner/dataset --dataset -b v2 -F train" {
			t.Errorf("cmd = %q", cmd)
		}
	})
}

func TestRepoInfo_GenerateRecommendedCommand(t *testing.T) {
	info := &RepoInfo{
		Repo: "owner/repo",
		SelectableItems: []SelectableItem{
			{ID: "q4_k_m", FilterValue: "q4_k_m", Recommended: true},
			{ID: "q8_0", FilterValue: "q8_0", Recommended: false},
			{ID: "q5_k_m", FilterValue: "q5_k_m", Recommended: true},
		},
	}

	cmd := info.GenerateRecommendedCommand()
	// Should include both recommended filters
	if cmd != "hfdownloader download owner/repo -F q4_k_m,q5_k_m" {
		t.Errorf("cmd = %q", cmd)
	}
}

func TestRepoInfo_PopulateCLICommands(t *testing.T) {
	t.Run("with recommended items", func(t *testing.T) {
		info := &RepoInfo{
			Repo: "owner/repo",
			SelectableItems: []SelectableItem{
				{ID: "q4_k_m", FilterValue: "q4_k_m", Recommended: true},
			},
		}
		info.PopulateCLICommands()

		if info.CLICommand != "hfdownloader download owner/repo" {
			t.Errorf("CLICommand = %q", info.CLICommand)
		}
		if info.CLICommandFull != "hfdownloader download owner/repo -F q4_k_m" {
			t.Errorf("CLICommandFull = %q", info.CLICommandFull)
		}
	})

	t.Run("without recommended items", func(t *testing.T) {
		info := &RepoInfo{
			Repo: "owner/repo",
			SelectableItems: []SelectableItem{
				{ID: "q4_k_m", FilterValue: "q4_k_m", Recommended: false},
			},
		}
		info.PopulateCLICommands()

		if info.CLICommand != "hfdownloader download owner/repo" {
			t.Errorf("CLICommand = %q", info.CLICommand)
		}
		// CLICommandFull should be empty when same as CLICommand
		if info.CLICommandFull != "" {
			t.Errorf("CLICommandFull = %q, want empty", info.CLICommandFull)
		}
	})
}

func TestRepoInfo_GetSelectedSize(t *testing.T) {
	info := &RepoInfo{
		SelectableItems: []SelectableItem{
			{ID: "a", Size: 1000},
			{ID: "b", Size: 2000},
			{ID: "c", Size: 3000},
		},
	}

	tests := []struct {
		name     string
		selected []string
		expected int64
	}{
		{"none selected", []string{}, 0},
		{"one selected", []string{"a"}, 1000},
		{"two selected", []string{"a", "c"}, 4000},
		{"all selected", []string{"a", "b", "c"}, 6000},
		{"nonexistent", []string{"x", "y"}, 0},
		{"mixed", []string{"a", "x"}, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := info.GetSelectedSize(tt.selected)
			if result != tt.expected {
				t.Errorf("GetSelectedSize(%v) = %d, want %d", tt.selected, result, tt.expected)
			}
		})
	}
}

func TestFileInfo_Fields(t *testing.T) {
	file := FileInfo{
		Path:      "path/to/model.safetensors",
		Name:      "model.safetensors",
		Size:      4000000000,
		SizeHuman: "3.7 GiB",
		IsLFS:     true,
		SHA256:    "abc123",
		Directory: "path/to",
	}

	if file.Path != "path/to/model.safetensors" {
		t.Errorf("Path = %q", file.Path)
	}
	if file.Name != "model.safetensors" {
		t.Errorf("Name = %q", file.Name)
	}
	if file.Size != 4000000000 {
		t.Errorf("Size = %d", file.Size)
	}
	if !file.IsLFS {
		t.Error("IsLFS should be true")
	}
	if file.SHA256 != "abc123" {
		t.Errorf("SHA256 = %q", file.SHA256)
	}
}

func TestRepoRef_Fields(t *testing.T) {
	ref := RepoRef{
		Name:   "main",
		Type:   "branch",
		Commit: "abc123",
	}

	if ref.Name != "main" {
		t.Errorf("Name = %q", ref.Name)
	}
	if ref.Type != "branch" {
		t.Errorf("Type = %q", ref.Type)
	}
	if ref.Commit != "abc123" {
		t.Errorf("Commit = %q", ref.Commit)
	}
}

func TestSelectableItem_Fields(t *testing.T) {
	item := SelectableItem{
		ID:           "q4_k_m",
		Label:        "Q4_K_M",
		Description:  "Medium 4-bit, recommended",
		Size:         4000000000,
		SizeHuman:    "3.7 GiB",
		Quality:      4,
		QualityStars: "★★★★☆",
		Recommended:  true,
		Category:     "quantization",
		FilterValue:  "q4_k_m",
		Files:        []string{"model.Q4_K_M.gguf"},
		RAM:          5000000000,
		RAMHuman:     "4.7 GiB",
	}

	if item.ID != "q4_k_m" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Label != "Q4_K_M" {
		t.Errorf("Label = %q", item.Label)
	}
	if item.Quality != 4 {
		t.Errorf("Quality = %d", item.Quality)
	}
	if !item.Recommended {
		t.Error("Recommended should be true")
	}
	if len(item.Files) != 1 {
		t.Errorf("Files count = %d", len(item.Files))
	}
}

func TestRelatedDownload_Fields(t *testing.T) {
	related := RelatedDownload{
		Type:        "base_model",
		Repo:        "meta-llama/Llama-2-7b",
		Label:       "Llama 2 7B",
		Description: "Base model required for this LoRA",
		Required:    true,
		Size:        14000000000,
		SizeHuman:   "13 GiB",
	}

	if related.Type != "base_model" {
		t.Errorf("Type = %q", related.Type)
	}
	if related.Repo != "meta-llama/Llama-2-7b" {
		t.Errorf("Repo = %q", related.Repo)
	}
	if !related.Required {
		t.Error("Required should be true")
	}
}

func TestGGUFInfo_Fields(t *testing.T) {
	info := GGUFInfo{
		ModelName:      "Mistral 7B Instruct",
		ParameterCount: "7B",
		Quantizations: []GGUFQuantization{
			{Name: "Q4_K_M", Quality: 4},
		},
	}

	if info.ModelName != "Mistral 7B Instruct" {
		t.Errorf("ModelName = %q", info.ModelName)
	}
	if info.ParameterCount != "7B" {
		t.Errorf("ParameterCount = %q", info.ParameterCount)
	}
	if len(info.Quantizations) != 1 {
		t.Errorf("Quantizations count = %d", len(info.Quantizations))
	}
}

func TestDiffusersInfo_Fields(t *testing.T) {
	info := DiffusersInfo{
		PipelineType:        "StableDiffusionXLPipeline",
		PipelineDescription: "SDXL Pipeline",
		DiffusersVersion:    "0.25.0",
		Components: []DiffusersComponent{
			{Name: "unet", Required: true},
			{Name: "vae", Required: true},
		},
		Variants:   []string{"fp16", "fp32"},
		Precisions: []string{"float16"},
	}

	if info.PipelineType != "StableDiffusionXLPipeline" {
		t.Errorf("PipelineType = %q", info.PipelineType)
	}
	if len(info.Components) != 2 {
		t.Errorf("Components count = %d", len(info.Components))
	}
	if len(info.Variants) != 2 {
		t.Errorf("Variants count = %d", len(info.Variants))
	}
}

func TestLoRAInfo_Fields(t *testing.T) {
	info := LoRAInfo{
		AdapterType:   "lora",
		BaseModel:     "meta-llama/Llama-2-7b",
		Rank:          16,
		Alpha:         32.0,
		TargetModules: []string{"q_proj", "v_proj"},
		TaskType:      "CAUSAL_LM",
	}

	if info.AdapterType != "lora" {
		t.Errorf("AdapterType = %q", info.AdapterType)
	}
	if info.Rank != 16 {
		t.Errorf("Rank = %d", info.Rank)
	}
	if len(info.TargetModules) != 2 {
		t.Errorf("TargetModules count = %d", len(info.TargetModules))
	}
}

func TestQuantizedInfo_Fields(t *testing.T) {
	info := QuantizedInfo{
		Method:            "gptq",
		MethodDescription: "GPTQ quantized model",
		Bits:              4,
		GroupSize:         128,
		DescAct:           true,
		Backends:          []string{"exllama", "auto-gptq"},
	}

	if info.Method != "gptq" {
		t.Errorf("Method = %q", info.Method)
	}
	if info.Bits != 4 {
		t.Errorf("Bits = %d", info.Bits)
	}
	if info.GroupSize != 128 {
		t.Errorf("GroupSize = %d", info.GroupSize)
	}
}

func TestDatasetInfo_Fields(t *testing.T) {
	info := DatasetInfo{
		Splits: []DatasetSplit{
			{Name: "train", FileCount: 10, Size: 1000000000},
			{Name: "test", FileCount: 2, Size: 100000000},
		},
		Configs:       []string{"default", "large"},
		Formats:       []string{"parquet", "arrow"},
		PrimaryFormat: "parquet",
	}

	if len(info.Splits) != 2 {
		t.Errorf("Splits count = %d", len(info.Splits))
	}
	if info.PrimaryFormat != "parquet" {
		t.Errorf("PrimaryFormat = %q", info.PrimaryFormat)
	}
}

func TestTransformersInfo_Fields(t *testing.T) {
	info := TransformersInfo{
		Architecture:            "LlamaForCausalLM",
		ArchitectureDescription: "Llama causal language model",
		ModelType:               "llama",
		HiddenSize:              4096,
		NumHiddenLayers:         32,
		NumAttentionHeads:       32,
		VocabSize:               32000,
		MaxPositionEmbeddings:   4096,
		EstimatedParameters:     "7B",
		Precision:               "float16",
		IsSharded:               true,
		ShardCount:              4,
	}

	if info.Architecture != "LlamaForCausalLM" {
		t.Errorf("Architecture = %q", info.Architecture)
	}
	if info.HiddenSize != 4096 {
		t.Errorf("HiddenSize = %d", info.HiddenSize)
	}
	if !info.IsSharded {
		t.Error("IsSharded should be true")
	}
}

func TestAudioInfo_Fields(t *testing.T) {
	info := AudioInfo{
		Task:                 "automatic-speech-recognition",
		TaskDescription:      "ASR model",
		FeatureExtractorType: "WhisperFeatureExtractor",
		SampleRate:           16000,
		Languages:            []string{"en", "es", "fr"},
	}

	if info.Task != "automatic-speech-recognition" {
		t.Errorf("Task = %q", info.Task)
	}
	if info.SampleRate != 16000 {
		t.Errorf("SampleRate = %d", info.SampleRate)
	}
}

func TestVisionInfo_Fields(t *testing.T) {
	info := VisionInfo{
		Task:               "image-classification",
		ImageProcessorType: "ViTImageProcessor",
		ImageSize:          ImageSize{Height: 224, Width: 224},
		NumChannels:        3,
		NumLabels:          1000,
	}

	if info.Task != "image-classification" {
		t.Errorf("Task = %q", info.Task)
	}
	if info.ImageSize.Height != 224 {
		t.Errorf("ImageSize.Height = %d", info.ImageSize.Height)
	}
}

func TestMultimodalInfo_Fields(t *testing.T) {
	info := MultimodalInfo{
		Task:          "visual-question-answering",
		Modalities:    []string{"text", "image"},
		ProcessorType: "LlavaProcessor",
		VisionEncoder: "CLIP",
		ImageSize:     ImageSize{Height: 336, Width: 336},
	}

	if info.Task != "visual-question-answering" {
		t.Errorf("Task = %q", info.Task)
	}
	if len(info.Modalities) != 2 {
		t.Errorf("Modalities count = %d", len(info.Modalities))
	}
}

func TestONNXInfo_Fields(t *testing.T) {
	info := ONNXInfo{
		Models: []ONNXModel{
			{Path: "model.onnx", Name: "model", Size: 500000000},
			{Path: "model_fp16.onnx", Name: "model_fp16", Variant: "fp16"},
		},
		Optimized: true,
		Quantized: false,
		Runtimes:  []string{"onnxruntime", "openvino"},
	}

	if len(info.Models) != 2 {
		t.Errorf("Models count = %d", len(info.Models))
	}
	if !info.Optimized {
		t.Error("Optimized should be true")
	}
}

func TestWeightFile_Fields(t *testing.T) {
	wf := WeightFile{
		Path:       "model-00001-of-00004.safetensors",
		Name:       "model-00001-of-00004.safetensors",
		Size:       4000000000,
		SizeHuman:  "3.7 GiB",
		Format:     "safetensors",
		ShardIndex: 0,
		ShardTotal: 4,
	}

	if wf.Format != "safetensors" {
		t.Errorf("Format = %q", wf.Format)
	}
	if wf.ShardTotal != 4 {
		t.Errorf("ShardTotal = %d", wf.ShardTotal)
	}
}

func TestTokenizerInfo_Fields(t *testing.T) {
	tok := TokenizerInfo{
		Type:            "LlamaTokenizerFast",
		VocabSize:       32000,
		ModelMaxLength:  4096,
		PaddingSide:     "left",
		AddBosToken:     true,
		AddEosToken:     false,
		HasChatTemplate: true,
	}

	if tok.Type != "LlamaTokenizerFast" {
		t.Errorf("Type = %q", tok.Type)
	}
	if tok.VocabSize != 32000 {
		t.Errorf("VocabSize = %d", tok.VocabSize)
	}
	if !tok.HasChatTemplate {
		t.Error("HasChatTemplate should be true")
	}
}
