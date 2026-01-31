// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/smartdl"
)

func TestNewAnalyzeCmd(t *testing.T) {
	ctx := context.Background()
	ro := &RootOpts{}

	cmd := newAnalyzeCmd(ctx, ro)
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}

	if cmd.Use != "analyze <repo>" {
		t.Errorf("Use = %q", cmd.Use)
	}

	// Check flags exist
	flags := []string{"dataset", "endpoint", "format", "revision", "interactive", "cache-dir"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q should exist", name)
		}
	}
}

func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestPrintAnalysis(t *testing.T) {
	info := &smartdl.RepoInfo{
		Repo:           "owner/repo",
		Branch:         "main",
		Type:           smartdl.TypeGGUF,
		TypeDescription: "GGUF quantized model",
		FileCount:      5,
		TotalSizeHuman: "4.5 GiB",
		CLICommand:     "hfdownloader download owner/repo",
	}

	output := captureOutput(func() {
		printAnalysis(info)
	})

	if !strings.Contains(output, "owner/repo") {
		t.Error("output should contain repo name")
	}
	if !strings.Contains(output, "GGUF") {
		t.Error("output should contain type")
	}
}

func TestPrintGGUFAnalysis(t *testing.T) {
	t.Run("nil GGUF info", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printGGUFAnalysis(info)
		})
		// Should not panic, output may be empty
		_ = output
	})

	t.Run("with GGUF info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			GGUF: &smartdl.GGUFInfo{
				ModelName:      "Mistral-7B",
				ParameterCount: "7B",
			},
		}
		output := captureOutput(func() {
			printGGUFAnalysis(info)
		})
		if !strings.Contains(output, "Mistral-7B") {
			t.Error("output should contain model name")
		}
	})
}

func TestPrintTransformersAnalysis(t *testing.T) {
	t.Run("nil transformers info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Files: []smartdl.FileInfo{
				{Name: "config.json", Size: 1024},
			},
		}
		output := captureOutput(func() {
			printTransformersAnalysis(info)
		})
		// Should not panic, may fall back to generic or be empty
		_ = output
	})

	t.Run("with transformers info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Transformers: &smartdl.TransformersInfo{
				Architecture:            "LlamaForCausalLM",
				ArchitectureDescription: "Llama language model",
				Task:                    "text-generation",
				HiddenSize:              4096,
				NumHiddenLayers:         32,
				NumAttentionHeads:       32,
				VocabSize:               32000,
				ContextLength:           4096,
				IsSharded:               true,
				ShardCount:              2,
				WeightFiles: []smartdl.WeightFile{
					{Name: "model-00001-of-00002.safetensors", SizeHuman: "4.0 GiB", Format: "safetensors"},
				},
			},
		}
		output := captureOutput(func() {
			printTransformersAnalysis(info)
		})
		if !strings.Contains(output, "LlamaForCausalLM") {
			t.Error("output should contain architecture")
		}
		if !strings.Contains(output, "4096") {
			t.Error("output should contain hidden size")
		}
	})
}

func TestPrintDiffusersAnalysis(t *testing.T) {
	t.Run("nil diffusers info", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printDiffusersAnalysis(info)
		})
		_ = output // Should not panic
	})

	t.Run("with diffusers info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Diffusers: &smartdl.DiffusersInfo{
				PipelineType:        "StableDiffusionXLPipeline",
				PipelineDescription: "SDXL image generation",
				DiffusersVersion:    "0.25.0",
				Variants:            []string{"fp16", "fp32"},
				Components: []smartdl.DiffusersComponent{
					{Name: "unet", ClassName: "UNet2DConditionModel", SizeHuman: "5.0 GiB", Required: true},
					{Name: "vae", ClassName: "AutoencoderKL", SizeHuman: "300 MiB", Required: true},
				},
			},
		}
		output := captureOutput(func() {
			printDiffusersAnalysis(info)
		})
		if !strings.Contains(output, "StableDiffusionXLPipeline") {
			t.Error("output should contain pipeline type")
		}
		if !strings.Contains(output, "unet") {
			t.Error("output should contain components")
		}
	})
}

func TestPrintLoRAAnalysis(t *testing.T) {
	t.Run("nil LoRA info", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printLoRAAnalysis(info)
		})
		_ = output
	})

	t.Run("with LoRA info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			LoRA: &smartdl.LoRAInfo{
				AdapterType:        "LoRA",
				AdapterDescription: "Low-Rank Adaptation",
				BaseModel:          "meta-llama/Llama-2-7b",
				Rank:               16,
				Alpha:              32,
				TargetModules:      []string{"q_proj", "v_proj"},
			},
		}
		output := captureOutput(func() {
			printLoRAAnalysis(info)
		})
		if !strings.Contains(output, "LoRA") {
			t.Error("output should contain adapter type")
		}
		if !strings.Contains(output, "meta-llama") {
			t.Error("output should contain base model")
		}
	})
}

func TestPrintQuantizedAnalysis(t *testing.T) {
	t.Run("nil quantized info", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printQuantizedAnalysis(info)
		})
		_ = output
	})

	t.Run("with GPTQ info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Quantized: &smartdl.QuantizedInfo{
				Method:            "gptq",
				MethodDescription: "GPTQ quantization",
				Bits:              4,
				GroupSize:         128,
				DescAct:           true,
				EstimatedVRAM:     8 * 1024 * 1024 * 1024,
				Backends:          []string{"auto-gptq", "exllamav2"},
			},
		}
		output := captureOutput(func() {
			printQuantizedAnalysis(info)
		})
		if !strings.Contains(output, "GPTQ") {
			t.Error("output should contain method")
		}
		if !strings.Contains(output, "4-bit") {
			t.Error("output should contain bits")
		}
	})
}

func TestPrintDatasetAnalysis(t *testing.T) {
	t.Run("nil dataset info", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printDatasetAnalysis(info)
		})
		_ = output
	})

	t.Run("with dataset info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Dataset: &smartdl.DatasetInfo{
				Formats:       []string{"parquet", "json"},
				PrimaryFormat: "parquet",
				Configs:       []string{"en", "fr"},
				Splits: []smartdl.DatasetSplit{
					{Name: "train", Size: 1024 * 1024 * 1024, SizeHuman: "1.0 GiB", FileCount: 10},
					{Name: "test", Size: 100 * 1024 * 1024, SizeHuman: "100 MiB", FileCount: 1},
				},
			},
		}
		output := captureOutput(func() {
			printDatasetAnalysis(info)
		})
		if !strings.Contains(output, "parquet") {
			t.Error("output should contain format")
		}
		if !strings.Contains(output, "train") {
			t.Error("output should contain splits")
		}
	})
}

func TestPrintGenericAnalysis(t *testing.T) {
	info := &smartdl.RepoInfo{
		Files: []smartdl.FileInfo{
			{Name: "model.bin", Path: "model.bin", Size: 1024 * 1024 * 1024, SizeHuman: "1.0 GiB", IsLFS: true},
			{Name: "config.json", Path: "config.json", Size: 1024, SizeHuman: "1.0 KiB", IsLFS: false},
		},
	}

	output := captureOutput(func() {
		printGenericAnalysis(info)
	})

	if !strings.Contains(output, "model.bin") {
		t.Error("output should contain file names")
	}
	if !strings.Contains(output, "yes") {
		t.Error("output should show LFS status")
	}
}

func TestPrintSelectableItems(t *testing.T) {
	t.Run("empty items", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printSelectableItems(info)
		})
		if output != "" {
			t.Error("should produce no output for empty items")
		}
	})

	t.Run("quantization items", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			SelectableItems: []smartdl.SelectableItem{
				{
					ID:           "q4_k_m",
					Label:        "Q4_K_M",
					Category:     "quantization",
					Size:         4 * 1024 * 1024 * 1024,
					SizeHuman:    "4.0 GiB",
					RAMHuman:     "5.0 GiB",
					Quality:      4,
					Recommended:  true,
					FilterValue:  "q4_k_m",
				},
			},
		}
		output := captureOutput(func() {
			printSelectableItems(info)
		})
		if !strings.Contains(output, "Q4_K_M") {
			t.Error("output should contain item label")
		}
		if !strings.Contains(output, "Quantization") {
			t.Error("output should contain category title")
		}
	})

	t.Run("component items", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			SelectableItems: []smartdl.SelectableItem{
				{
					ID:          "unet",
					Label:       "UNet",
					Category:    "component",
					SizeHuman:   "5.0 GiB",
					Recommended: true,
					FilterValue: "unet",
				},
			},
		}
		output := captureOutput(func() {
			printSelectableItems(info)
		})
		if !strings.Contains(output, "UNet") {
			t.Error("output should contain component")
		}
	})
}

func TestPrintRelatedDownloads(t *testing.T) {
	t.Run("no related downloads", func(t *testing.T) {
		info := &smartdl.RepoInfo{}
		output := captureOutput(func() {
			printRelatedDownloads(info)
		})
		if output != "" {
			t.Error("should produce no output")
		}
	})

	t.Run("with related downloads", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			RelatedDownloads: []smartdl.RelatedDownload{
				{
					Label:       "Base Model",
					Repo:        "meta-llama/Llama-2-7b",
					Required:    true,
					Description: "Required for inference",
					SizeHuman:   "13.5 GiB",
				},
			},
		}
		output := captureOutput(func() {
			printRelatedDownloads(info)
		})
		if !strings.Contains(output, "Base Model") {
			t.Error("output should contain label")
		}
		if !strings.Contains(output, "required") {
			t.Error("output should indicate required")
		}
	})
}

func TestPrintCLICommands(t *testing.T) {
	info := &smartdl.RepoInfo{
		CLICommand:     "hfdownloader download owner/repo",
		CLICommandFull: "hfdownloader download owner/repo -F q4_k_m",
	}

	output := captureOutput(func() {
		printCLICommands(info)
	})

	if !strings.Contains(output, "Download Commands") {
		t.Error("output should have header")
	}
	if !strings.Contains(output, "hfdownloader download") {
		t.Error("output should contain command")
	}
}

func TestPrintAudioAnalysis(t *testing.T) {
	t.Run("nil audio info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Files: []smartdl.FileInfo{{Name: "model.bin"}},
		}
		output := captureOutput(func() {
			printAudioAnalysis(info)
		})
		// Should not panic
		_ = output
	})

	t.Run("with audio info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Audio: &smartdl.AudioInfo{
				Task:                 "automatic-speech-recognition",
				TaskDescription:      "Speech to text",
				FeatureExtractorType: "WhisperFeatureExtractor",
				SampleRate:           16000,
				NumMelBins:           80,
				Languages:            []string{"en", "es", "fr"},
				Framework:            "transformers",
			},
		}
		output := captureOutput(func() {
			printAudioAnalysis(info)
		})
		if !strings.Contains(output, "automatic-speech-recognition") {
			t.Error("output should contain task")
		}
		if !strings.Contains(output, "16000") {
			t.Error("output should contain sample rate")
		}
	})
}

func TestPrintVisionAnalysis(t *testing.T) {
	t.Run("nil vision info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Files: []smartdl.FileInfo{{Name: "model.bin"}},
		}
		output := captureOutput(func() {
			printVisionAnalysis(info)
		})
		// Should not panic
		_ = output
	})

	t.Run("with vision info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Vision: &smartdl.VisionInfo{
				Task:               "image-classification",
				TaskDescription:    "Classify images",
				ImageProcessorType: "ViTImageProcessor",
				ImageSize:          smartdl.ImageSize{Width: 224, Height: 224},
				NumChannels:        3,
				NumLabels:          1000,
				Framework:          "transformers",
			},
		}
		output := captureOutput(func() {
			printVisionAnalysis(info)
		})
		if !strings.Contains(output, "image-classification") {
			t.Error("output should contain task")
		}
		if !strings.Contains(output, "224x224") {
			t.Error("output should contain image size")
		}
	})
}

func TestPrintMultimodalAnalysis(t *testing.T) {
	t.Run("nil multimodal info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Files: []smartdl.FileInfo{{Name: "model.bin"}},
		}
		output := captureOutput(func() {
			printMultimodalAnalysis(info)
		})
		// Should not panic
		_ = output
	})

	t.Run("with multimodal info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Multimodal: &smartdl.MultimodalInfo{
				Task:            "image-to-text",
				TaskDescription: "Generate text from images",
				Modalities:      []string{"image", "text"},
				ProcessorType:   "LlavaProcessor",
				VisionEncoder:   "clip-vit-large",
				TextEncoder:     "llama",
				ImageSize:       smartdl.ImageSize{Width: 336, Height: 336},
				MaxTextLength:   2048,
				Framework:       "transformers",
			},
		}
		output := captureOutput(func() {
			printMultimodalAnalysis(info)
		})
		if !strings.Contains(output, "image-to-text") {
			t.Error("output should contain task")
		}
		if !strings.Contains(output, "image, text") {
			t.Error("output should contain modalities")
		}
	})
}

func TestPrintONNXAnalysis(t *testing.T) {
	t.Run("nil ONNX info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			Files: []smartdl.FileInfo{{Name: "model.onnx"}},
		}
		output := captureOutput(func() {
			printONNXAnalysis(info)
		})
		// Should not panic
		_ = output
	})

	t.Run("with ONNX info", func(t *testing.T) {
		info := &smartdl.RepoInfo{
			ONNX: &smartdl.ONNXInfo{
				Optimized: true,
				Quantized: true,
				Runtimes:  []string{"onnxruntime", "onnxruntime-gpu"},
				Models: []smartdl.ONNXModel{
					{Name: "model.onnx", SizeHuman: "500 MiB", Variant: "fp32", Optimized: false},
					{Name: "model_quantized.onnx", SizeHuman: "125 MiB", Variant: "int8", Optimized: true},
				},
			},
		}
		output := captureOutput(func() {
			printONNXAnalysis(info)
		})
		if !strings.Contains(output, "Optimized") {
			t.Error("output should mention optimization")
		}
		if !strings.Contains(output, "model.onnx") {
			t.Error("output should contain model names")
		}
	})
}
