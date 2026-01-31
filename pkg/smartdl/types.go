// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

// Package smartdl provides intelligent repository analysis and download assistance
// for HuggingFace Hub repositories.
//
// The Smart Downloader analyzes repositories to determine their type (GGUF, Transformers,
// Diffusers, LoRA, etc.) and presents users with intelligent download options based on
// the repository structure.
//
// Example usage:
//
//	analyzer := smartdl.NewAnalyzer(smartdl.AnalyzerOptions{
//	    Token:    os.Getenv("HF_TOKEN"),
//	    Endpoint: "https://huggingface.co",
//	})
//
//	info, err := analyzer.Analyze(ctx, "TheBloke/Mistral-7B-Instruct-v0.2-GGUF", false)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Type: %s\n", info.Type)
//	if info.Type == smartdl.TypeGGUF {
//	    gguf := info.GGUF
//	    for _, q := range gguf.Quantizations {
//	        fmt.Printf("  %s: %s (%s RAM)\n", q.Name, q.File.SizeHuman, q.EstimatedRAMHuman)
//	    }
//	}
package smartdl

import (
	"strings"
	"time"
)

// RepoType identifies the type of HuggingFace repository.
type RepoType string

const (
	// TypeGGUF indicates a GGUF quantized model (llama.cpp, Ollama, vLLM).
	TypeGGUF RepoType = "gguf"

	// TypeTransformers indicates a standard transformers model (safetensors/bin).
	TypeTransformers RepoType = "transformers"

	// TypeDiffusers indicates a diffusers pipeline model (Stable Diffusion, SDXL, Flux).
	TypeDiffusers RepoType = "diffusers"

	// TypeLoRA indicates a LoRA/PEFT adapter.
	TypeLoRA RepoType = "lora"

	// TypeGPTQ indicates a GPTQ quantized model.
	TypeGPTQ RepoType = "gptq"

	// TypeAWQ indicates an AWQ quantized model.
	TypeAWQ RepoType = "awq"

	// TypeONNX indicates an ONNX model.
	TypeONNX RepoType = "onnx"

	// TypeDataset indicates a HuggingFace dataset.
	TypeDataset RepoType = "dataset"

	// TypeAudio indicates an audio model (ASR, TTS, etc.).
	TypeAudio RepoType = "audio"

	// TypeVision indicates a vision model (classification, detection, etc.).
	TypeVision RepoType = "vision"

	// TypeMultimodal indicates a multimodal model (VLM, etc.).
	TypeMultimodal RepoType = "multimodal"

	// TypeGeneric indicates an unknown or generic repository type.
	TypeGeneric RepoType = "generic"
)

// String returns the string representation of a RepoType.
func (t RepoType) String() string {
	return string(t)
}

// Description returns a human-readable description of the repo type.
func (t RepoType) Description() string {
	switch t {
	case TypeGGUF:
		return "GGUF quantized model (llama.cpp, Ollama)"
	case TypeTransformers:
		return "Transformers model (safetensors)"
	case TypeDiffusers:
		return "Diffusers pipeline (Stable Diffusion, SDXL, Flux)"
	case TypeLoRA:
		return "LoRA/PEFT adapter"
	case TypeGPTQ:
		return "GPTQ quantized model"
	case TypeAWQ:
		return "AWQ quantized model"
	case TypeONNX:
		return "ONNX model"
	case TypeDataset:
		return "HuggingFace dataset"
	case TypeAudio:
		return "Audio model (ASR, TTS)"
	case TypeVision:
		return "Vision model"
	case TypeMultimodal:
		return "Multimodal model (VLM)"
	default:
		return "Generic repository"
	}
}

// FileInfo represents a file in the repository.
type FileInfo struct {
	// Path is the relative path within the repository.
	Path string `json:"path"`

	// Name is the filename (basename of path).
	Name string `json:"name"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// SizeHuman is the human-readable size (e.g., "4.1 GB").
	SizeHuman string `json:"size_human"`

	// IsLFS indicates if the file is stored in Git LFS.
	IsLFS bool `json:"is_lfs"`

	// SHA256 is the file hash (for LFS files).
	SHA256 string `json:"sha256,omitempty"`

	// Directory is the parent directory path.
	Directory string `json:"directory,omitempty"`
}

// RepoRef represents a branch or tag in the repository.
type RepoRef struct {
	// Name is the ref name (e.g., "main", "v1.0", "fp16").
	Name string `json:"name"`

	// Type is "branch" or "tag".
	Type string `json:"type"`

	// Commit is the commit SHA this ref points to.
	Commit string `json:"commit,omitempty"`
}

// RepoInfo contains analyzed information about a HuggingFace repository.
type RepoInfo struct {
	// Repo is the repository ID in "owner/name" format.
	Repo string `json:"repo"`

	// IsDataset indicates if this is a dataset repository.
	IsDataset bool `json:"is_dataset"`

	// Type is the detected repository type.
	Type RepoType `json:"type"`

	// TypeDescription is a human-readable description of the type.
	TypeDescription string `json:"type_description"`

	// Files is the list of all files in the repository.
	Files []FileInfo `json:"files"`

	// TotalSize is the sum of all file sizes in bytes.
	TotalSize int64 `json:"total_size"`

	// TotalSizeHuman is the human-readable total size.
	TotalSizeHuman string `json:"total_size_human"`

	// FileCount is the number of files.
	FileCount int `json:"file_count"`

	// Commit is the resolved commit SHA.
	Commit string `json:"commit,omitempty"`

	// Branch is the branch/revision name.
	Branch string `json:"branch,omitempty"`

	// Refs is the list of available branches and tags.
	Refs []RepoRef `json:"refs,omitempty"`

	// AnalyzedAt is when the analysis was performed.
	AnalyzedAt time.Time `json:"analyzed_at"`

	// Metadata contains raw parsed metadata from config files.
	// Keys depend on repo type (e.g., "config.json", "model_index.json").
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Type-specific information (only one will be populated based on Type)
	GGUF         *GGUFInfo         `json:"gguf,omitempty"`
	Transformers *TransformersInfo `json:"transformers,omitempty"`
	Diffusers    *DiffusersInfo    `json:"diffusers,omitempty"`
	LoRA         *LoRAInfo         `json:"lora,omitempty"`
	Quantized    *QuantizedInfo    `json:"quantized,omitempty"`
	Dataset      *DatasetInfo      `json:"dataset,omitempty"`
	Audio        *AudioInfo        `json:"audio,omitempty"`
	Vision       *VisionInfo       `json:"vision,omitempty"`
	Multimodal   *MultimodalInfo   `json:"multimodal,omitempty"`
	ONNX         *ONNXInfo         `json:"onnx,omitempty"`

	// SelectableItems provides unified download options across all model types.
	// For GGUF: quantizations, for Diffusers: components/variants, for Transformers: formats, etc.
	SelectableItems []SelectableItem `json:"selectable_items,omitempty"`

	// CLICommand is the base download command without filters.
	CLICommand string `json:"cli_command,omitempty"`

	// CLICommandFull is the download command with recommended filters applied.
	CLICommandFull string `json:"cli_command_full,omitempty"`

	// RelatedDownloads lists related repositories (e.g., base model for LoRA).
	RelatedDownloads []RelatedDownload `json:"related_downloads,omitempty"`
}

// GGUFInfo contains GGUF-specific analysis results.
type GGUFInfo struct {
	// ModelName is the base model name extracted from filenames.
	ModelName string `json:"model_name,omitempty"`

	// ParameterCount is the detected parameter count (e.g., "7B", "13B").
	ParameterCount string `json:"parameter_count,omitempty"`

	// Quantizations is the list of available quantizations.
	Quantizations []GGUFQuantization `json:"quantizations"`
}

// GGUFQuantization represents a single GGUF quantization option.
type GGUFQuantization struct {
	// Name is the quantization name (e.g., "Q4_K_M").
	Name string `json:"name"`

	// File is the file info for this quantization.
	File FileInfo `json:"file"`

	// Quality is the quality rating (1-5 stars).
	Quality int `json:"quality"`

	// QualityStars is the star representation (e.g., "★★★★☆").
	QualityStars string `json:"quality_stars"`

	// EstimatedRAM is the estimated RAM needed in bytes.
	EstimatedRAM int64 `json:"estimated_ram"`

	// EstimatedRAMHuman is the human-readable RAM estimate.
	EstimatedRAMHuman string `json:"estimated_ram_human"`

	// Description is a human-readable description of this quantization level.
	Description string `json:"description,omitempty"`

	// Recommended indicates if this quantization is recommended for the user's system.
	Recommended bool `json:"recommended,omitempty"`
}

// DiffusersInfo contains Diffusers-specific analysis results.
type DiffusersInfo struct {
	// PipelineType is the pipeline class name (e.g., "StableDiffusionXLPipeline").
	PipelineType string `json:"pipeline_type"`

	// PipelineDescription is a human-readable description of the pipeline.
	PipelineDescription string `json:"pipeline_description,omitempty"`

	// DiffusersVersion is the diffusers library version from model_index.json.
	DiffusersVersion string `json:"diffusers_version,omitempty"`

	// Components is the list of pipeline components.
	Components []DiffusersComponent `json:"components"`

	// Variants is the list of available precision variants (fp16, fp32, bf16).
	Variants []string `json:"variants,omitempty"`

	// Precisions is the list of detected model precisions.
	Precisions []string `json:"precisions,omitempty"`
}

// DiffusersComponent represents a component in a diffusers pipeline.
type DiffusersComponent struct {
	// Name is the component name (e.g., "unet", "vae").
	Name string `json:"name"`

	// Library is the source library (e.g., "diffusers", "transformers").
	Library string `json:"library,omitempty"`

	// ClassName is the component class (e.g., "UNet2DConditionModel").
	ClassName string `json:"class_name,omitempty"`

	// Size is the total size of this component's files.
	Size int64 `json:"size"`

	// SizeHuman is the human-readable size.
	SizeHuman string `json:"size_human"`

	// Files is the list of files belonging to this component.
	Files []FileInfo `json:"files,omitempty"`

	// Required indicates if this component is required for the pipeline.
	Required bool `json:"required"`
}

// LoRAInfo contains LoRA/adapter-specific analysis results.
type LoRAInfo struct {
	// AdapterType is the adapter type (e.g., "lora", "qlora", "ia3").
	AdapterType string `json:"adapter_type"`

	// AdapterDescription is a human-readable description of the adapter type.
	AdapterDescription string `json:"adapter_description,omitempty"`

	// BaseModel is the base model this adapter is trained for.
	BaseModel string `json:"base_model,omitempty"`

	// Rank is the LoRA rank (r parameter).
	Rank int `json:"rank,omitempty"`

	// Alpha is the LoRA alpha scaling factor.
	Alpha float64 `json:"alpha,omitempty"`

	// Dropout is the LoRA dropout rate.
	Dropout float64 `json:"dropout,omitempty"`

	// TargetModules is the list of targeted module names.
	TargetModules []string `json:"target_modules,omitempty"`

	// Bias is the bias training mode ("none", "all", "lora_only").
	Bias string `json:"bias,omitempty"`

	// TaskType is the PEFT task type (e.g., "CAUSAL_LM").
	TaskType string `json:"task_type,omitempty"`

	// FanInFanOut indicates if fan_in_fan_out is enabled.
	FanInFanOut bool `json:"fan_in_fan_out,omitempty"`

	// InitLoraWeights indicates if LoRA weights are initialized.
	InitLoraWeights bool `json:"init_lora_weights,omitempty"`

	// QuantType is the quantization type for QLoRA (e.g., "nf4").
	QuantType string `json:"quant_type,omitempty"`
}

// QuantizedInfo contains GPTQ/AWQ-specific analysis results.
type QuantizedInfo struct {
	// Method is the quantization method (e.g., "gptq", "awq", "exl2").
	Method string `json:"method"`

	// MethodDescription is a human-readable description of the method.
	MethodDescription string `json:"method_description,omitempty"`

	// Bits is the quantization bit width.
	Bits int `json:"bits"`

	// GroupSize is the quantization group size.
	GroupSize int `json:"group_size,omitempty"`

	// DescAct indicates if GPTQ desc_act is enabled.
	DescAct bool `json:"desc_act,omitempty"`

	// Symmetric indicates if symmetric quantization is used.
	Symmetric bool `json:"symmetric,omitempty"`

	// ZeroPoint indicates if zero-point quantization is used (AWQ).
	ZeroPoint bool `json:"zero_point,omitempty"`

	// Version is the quantization format version.
	Version string `json:"version,omitempty"`

	// BitsPerWeight is the EXL2 bits per weight.
	BitsPerWeight float64 `json:"bits_per_weight,omitempty"`

	// ExcludedModules is the list of modules not quantized.
	ExcludedModules []string `json:"excluded_modules,omitempty"`

	// Backends is the list of compatible inference backends.
	Backends []string `json:"backends,omitempty"`

	// ModelArchitecture is the base model architecture.
	ModelArchitecture string `json:"model_architecture,omitempty"`

	// BaseModel is the base model this was quantized from.
	BaseModel string `json:"base_model,omitempty"`

	// EstimatedVRAM is the estimated VRAM needed for inference.
	EstimatedVRAM int64 `json:"estimated_vram,omitempty"`

	// EstimatedVRAMHuman is the human-readable VRAM estimate.
	EstimatedVRAMHuman string `json:"estimated_vram_human,omitempty"`
}

// DatasetInfo contains dataset-specific analysis results.
type DatasetInfo struct {
	// Splits is the list of available splits.
	Splits []DatasetSplit `json:"splits"`

	// Configs is the list of available configurations/subsets.
	Configs []string `json:"configs,omitempty"`

	// Formats is the list of file formats found.
	Formats []string `json:"formats,omitempty"`

	// PrimaryFormat is the recommended format to download.
	PrimaryFormat string `json:"primary_format,omitempty"`
}

// DatasetSplit represents a dataset split (train, test, etc.).
type DatasetSplit struct {
	// Name is the split name (e.g., "train", "test", "validation").
	Name string `json:"name"`

	// Files is the list of files in this split.
	Files []FileInfo `json:"files,omitempty"`

	// FileCount is the number of files in this split.
	FileCount int `json:"file_count"`

	// Size is the total size of this split.
	Size int64 `json:"size"`

	// SizeHuman is the human-readable size.
	SizeHuman string `json:"size_human"`
}

// AudioInfo contains audio model-specific analysis results.
type AudioInfo struct {
	// Task is the audio task (e.g., "automatic-speech-recognition", "text-to-speech").
	Task string `json:"task"`

	// TaskDescription is a human-readable description of the task.
	TaskDescription string `json:"task_description,omitempty"`

	// FeatureExtractorType is the feature extractor class name.
	FeatureExtractorType string `json:"feature_extractor_type,omitempty"`

	// SampleRate is the expected audio sample rate in Hz.
	SampleRate int `json:"sample_rate,omitempty"`

	// NumMelBins is the number of mel filterbank bins (for speech models).
	NumMelBins int `json:"num_mel_bins,omitempty"`

	// MaxLength is the maximum audio length in samples or seconds.
	MaxLength int `json:"max_length,omitempty"`

	// Languages is the list of supported languages (for multilingual models).
	Languages []string `json:"languages,omitempty"`

	// Framework is the model framework (e.g., "transformers", "speechbrain").
	Framework string `json:"framework,omitempty"`
}

// VisionInfo contains vision model-specific analysis results.
type VisionInfo struct {
	// Task is the vision task (e.g., "image-classification", "object-detection").
	Task string `json:"task"`

	// TaskDescription is a human-readable description of the task.
	TaskDescription string `json:"task_description,omitempty"`

	// ImageProcessorType is the image processor class name.
	ImageProcessorType string `json:"image_processor_type,omitempty"`

	// ImageSize is the expected input image size.
	ImageSize ImageSize `json:"image_size,omitempty"`

	// NumChannels is the number of input channels (typically 3 for RGB).
	NumChannels int `json:"num_channels,omitempty"`

	// NumLabels is the number of output classes (for classification).
	NumLabels int `json:"num_labels,omitempty"`

	// Normalization contains mean and std for image normalization.
	Normalization *ImageNormalization `json:"normalization,omitempty"`

	// Framework is the model framework.
	Framework string `json:"framework,omitempty"`
}

// ImageSize represents image dimensions.
type ImageSize struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

// ImageNormalization contains normalization parameters.
type ImageNormalization struct {
	Mean []float64 `json:"mean"`
	Std  []float64 `json:"std"`
}

// SelectableItem represents an item that can be selected for download.
// This provides a unified interface for GGUF quantizations, Diffusers components,
// Transformers weight formats, dataset splits, etc.
type SelectableItem struct {
	// ID is a unique identifier for this item (e.g., "q4_k_m", "fp16", "train").
	ID string `json:"id"`

	// Label is the display name shown in UI.
	Label string `json:"label"`

	// Description provides additional context about this option.
	Description string `json:"description,omitempty"`

	// Size is the item size in bytes.
	Size int64 `json:"size,omitempty"`

	// SizeHuman is the human-readable size (e.g., "4.1 GiB").
	SizeHuman string `json:"size_human,omitempty"`

	// Quality is the quality rating (1-5 stars, 0 if not applicable).
	Quality int `json:"quality,omitempty"`

	// QualityStars is the star representation (e.g., "★★★★☆").
	QualityStars string `json:"quality_stars,omitempty"`

	// Recommended indicates if this is the default/recommended selection.
	Recommended bool `json:"recommended,omitempty"`

	// Category groups items (e.g., "quantization", "variant", "component", "split", "format").
	Category string `json:"category,omitempty"`

	// FilterValue is the value to pass to -F flag for downloading.
	FilterValue string `json:"filter_value"`

	// Files is the list of files included in this selection.
	Files []string `json:"files,omitempty"`

	// RAM is the estimated RAM needed (for GGUF quantizations).
	RAM int64 `json:"ram,omitempty"`

	// RAMHuman is the human-readable RAM estimate.
	RAMHuman string `json:"ram_human,omitempty"`
}

// RelatedDownload represents a related repository that may be needed.
// For example, LoRA adapters require a base model.
type RelatedDownload struct {
	// Type identifies the relationship (e.g., "base_model", "vae", "lora").
	Type string `json:"type"`

	// Repo is the HuggingFace repository ID.
	Repo string `json:"repo"`

	// Label is the display name.
	Label string `json:"label"`

	// Description explains why this download is related.
	Description string `json:"description,omitempty"`

	// Required indicates if this is required for the main download to work.
	Required bool `json:"required,omitempty"`

	// Size is the estimated size if known.
	Size int64 `json:"size,omitempty"`

	// SizeHuman is the human-readable size.
	SizeHuman string `json:"size_human,omitempty"`
}

// GenerateCLICommand generates the CLI download command for selected items.
// If selectedFilters is empty, returns the base command without filters.
func (r *RepoInfo) GenerateCLICommand(selectedFilters []string) string {
	cmd := "hfdownloader download " + r.Repo

	if r.IsDataset {
		cmd += " --dataset"
	}

	// Include revision/branch if specified and not "main"
	if r.Branch != "" && r.Branch != "main" {
		cmd += " -b " + r.Branch
	}

	if len(selectedFilters) > 0 {
		cmd += " -F " + strings.Join(selectedFilters, ",")
	}

	return cmd
}

// GenerateRecommendedCommand generates the CLI command with recommended selections.
func (r *RepoInfo) GenerateRecommendedCommand() string {
	var recommended []string
	for _, item := range r.SelectableItems {
		if item.Recommended {
			recommended = append(recommended, item.FilterValue)
		}
	}
	return r.GenerateCLICommand(recommended)
}

// PopulateCLICommands sets the CLICommand and CLICommandFull fields.
func (r *RepoInfo) PopulateCLICommands() {
	r.CLICommand = r.GenerateCLICommand(nil)
	r.CLICommandFull = r.GenerateRecommendedCommand()

	// If no recommended items, full command equals base command
	if r.CLICommandFull == r.CLICommand {
		r.CLICommandFull = ""
	}
}

// GetSelectedSize calculates the total size of selected items.
func (r *RepoInfo) GetSelectedSize(selectedIDs []string) int64 {
	idMap := make(map[string]bool)
	for _, id := range selectedIDs {
		idMap[id] = true
	}

	var total int64
	for _, item := range r.SelectableItems {
		if idMap[item.ID] {
			total += item.Size
		}
	}
	return total
}

// MultimodalInfo contains multimodal model-specific analysis results.
type MultimodalInfo struct {
	// Task is the multimodal task (e.g., "visual-question-answering", "image-to-text").
	Task string `json:"task"`

	// TaskDescription is a human-readable description of the task.
	TaskDescription string `json:"task_description,omitempty"`

	// Modalities is the list of supported modalities (e.g., ["text", "image"]).
	Modalities []string `json:"modalities"`

	// ProcessorType is the processor class name.
	ProcessorType string `json:"processor_type,omitempty"`

	// VisionEncoder is info about the vision encoder component.
	VisionEncoder string `json:"vision_encoder,omitempty"`

	// TextEncoder is info about the text encoder component.
	TextEncoder string `json:"text_encoder,omitempty"`

	// ImageSize is the expected input image size.
	ImageSize ImageSize `json:"image_size,omitempty"`

	// MaxTextLength is the maximum text input length.
	MaxTextLength int `json:"max_text_length,omitempty"`

	// Framework is the model framework.
	Framework string `json:"framework,omitempty"`
}

// ONNXInfo contains ONNX model-specific analysis results.
type ONNXInfo struct {
	// Models is the list of ONNX model files.
	Models []ONNXModel `json:"models"`

	// Optimized indicates if optimized versions are available.
	Optimized bool `json:"optimized,omitempty"`

	// Quantized indicates if quantized versions are available.
	Quantized bool `json:"quantized,omitempty"`

	// Runtimes is the list of compatible runtimes.
	Runtimes []string `json:"runtimes,omitempty"`
}

// ONNXModel represents a single ONNX model file.
type ONNXModel struct {
	// Path is the file path.
	Path string `json:"path"`

	// Name is the model name extracted from the path.
	Name string `json:"name"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// SizeHuman is the human-readable size.
	SizeHuman string `json:"size_human"`

	// Variant indicates the model variant (e.g., "fp32", "fp16", "int8").
	Variant string `json:"variant,omitempty"`

	// Optimized indicates if this is an optimized model.
	Optimized bool `json:"optimized,omitempty"`
}

// TransformersInfo contains detailed Transformers model analysis results.
type TransformersInfo struct {
	// Architecture is the model architecture class (e.g., "LlamaForCausalLM", "BertForSequenceClassification").
	Architecture string `json:"architecture"`

	// ArchitectureDescription is a human-readable description of the architecture.
	ArchitectureDescription string `json:"architecture_description,omitempty"`

	// Task is the primary task the model is designed for.
	Task string `json:"task,omitempty"`

	// TaskDescription is a human-readable description of the task.
	TaskDescription string `json:"task_description,omitempty"`

	// ModelType is the model type identifier (e.g., "llama", "bert", "gpt2").
	ModelType string `json:"model_type,omitempty"`

	// HiddenSize is the dimensionality of the hidden layers.
	HiddenSize int `json:"hidden_size,omitempty"`

	// NumHiddenLayers is the number of hidden layers.
	NumHiddenLayers int `json:"num_hidden_layers,omitempty"`

	// NumAttentionHeads is the number of attention heads.
	NumAttentionHeads int `json:"num_attention_heads,omitempty"`

	// IntermediateSize is the size of the intermediate (feed-forward) layer.
	IntermediateSize int `json:"intermediate_size,omitempty"`

	// VocabSize is the vocabulary size.
	VocabSize int `json:"vocab_size,omitempty"`

	// MaxPositionEmbeddings is the maximum sequence length.
	MaxPositionEmbeddings int `json:"max_position_embeddings,omitempty"`

	// ContextLength is the effective context window (may differ from max_position_embeddings).
	ContextLength int `json:"context_length,omitempty"`

	// EstimatedParameters is the estimated parameter count string (e.g., "7B", "70B").
	EstimatedParameters string `json:"estimated_parameters,omitempty"`

	// EstimatedParametersNum is the estimated parameter count as a number.
	EstimatedParametersNum int64 `json:"estimated_parameters_num,omitempty"`

	// Precision is the detected model precision (e.g., "fp32", "fp16", "bf16").
	Precision string `json:"precision,omitempty"`

	// IsSharded indicates if the model is split across multiple files.
	IsSharded bool `json:"is_sharded,omitempty"`

	// ShardCount is the number of shards if sharded.
	ShardCount int `json:"shard_count,omitempty"`

	// WeightFiles lists the model weight files.
	WeightFiles []WeightFile `json:"weight_files,omitempty"`

	// Tokenizer contains tokenizer information.
	Tokenizer *TokenizerInfo `json:"tokenizer,omitempty"`

	// TorchDtype is the torch dtype from config (e.g., "float16", "bfloat16").
	TorchDtype string `json:"torch_dtype,omitempty"`

	// Backends lists compatible inference backends.
	Backends []string `json:"backends,omitempty"`

	// SpecialTokens contains special token information.
	SpecialTokens map[string]interface{} `json:"special_tokens,omitempty"`

	// GenerationConfig contains generation configuration if present.
	GenerationConfig map[string]interface{} `json:"generation_config,omitempty"`
}

// WeightFile represents a model weight file.
type WeightFile struct {
	// Path is the file path.
	Path string `json:"path"`

	// Name is the filename.
	Name string `json:"name"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// SizeHuman is the human-readable size.
	SizeHuman string `json:"size_human"`

	// Format is the file format (safetensors, bin, pt).
	Format string `json:"format"`

	// ShardIndex is the shard index (0-based) if sharded.
	ShardIndex int `json:"shard_index,omitempty"`

	// ShardTotal is the total number of shards.
	ShardTotal int `json:"shard_total,omitempty"`
}

// TokenizerInfo contains tokenizer details.
type TokenizerInfo struct {
	// Type is the tokenizer class (e.g., "LlamaTokenizerFast", "GPT2Tokenizer").
	Type string `json:"type,omitempty"`

	// VocabSize is the vocabulary size.
	VocabSize int `json:"vocab_size,omitempty"`

	// ModelMaxLength is the maximum input length.
	ModelMaxLength int `json:"model_max_length,omitempty"`

	// PaddingSide is the padding side ("left" or "right").
	PaddingSide string `json:"padding_side,omitempty"`

	// TruncationSide is the truncation side.
	TruncationSide string `json:"truncation_side,omitempty"`

	// AddBosToken indicates if BOS token is added.
	AddBosToken bool `json:"add_bos_token,omitempty"`

	// AddEosToken indicates if EOS token is added.
	AddEosToken bool `json:"add_eos_token,omitempty"`

	// ChatTemplate is the chat template string if present.
	ChatTemplate string `json:"chat_template,omitempty"`

	// HasChatTemplate indicates if a chat template is defined.
	HasChatTemplate bool `json:"has_chat_template,omitempty"`
}
