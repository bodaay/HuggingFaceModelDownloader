// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"path/filepath"
	"strings"
)

// Audio task descriptions.
var audioTaskDescriptions = map[string]string{
	"automatic-speech-recognition": "Automatic Speech Recognition (ASR) - transcribes audio to text",
	"audio-classification":         "Audio Classification - categorizes audio into classes",
	"text-to-speech":               "Text-to-Speech (TTS) - generates speech from text",
	"text-to-audio":                "Text-to-Audio - generates audio from text descriptions",
	"audio-to-audio":               "Audio-to-Audio - transforms audio (e.g., voice conversion)",
	"voice-activity-detection":     "Voice Activity Detection - detects speech in audio",
}

// Vision task descriptions.
var visionTaskDescriptions = map[string]string{
	"image-classification":     "Image Classification - categorizes images into classes",
	"object-detection":         "Object Detection - locates and identifies objects in images",
	"image-segmentation":       "Image Segmentation - segments images into regions",
	"semantic-segmentation":    "Semantic Segmentation - classifies each pixel",
	"instance-segmentation":    "Instance Segmentation - identifies individual object instances",
	"panoptic-segmentation":    "Panoptic Segmentation - combines semantic and instance segmentation",
	"depth-estimation":         "Depth Estimation - estimates depth from images",
	"image-to-image":           "Image-to-Image - transforms images",
	"unconditional-image-generation": "Unconditional Image Generation - generates images without prompts",
	"zero-shot-image-classification": "Zero-Shot Classification - classifies without training",
}

// Multimodal task descriptions.
var multimodalTaskDescriptions = map[string]string{
	"visual-question-answering": "Visual Question Answering (VQA) - answers questions about images",
	"image-to-text":             "Image-to-Text - generates text descriptions of images",
	"image-text-to-text":        "Image-Text-to-Text - generates text from image and text input",
	"document-question-answering": "Document QA - answers questions about documents",
	"video-text-to-text":        "Video-Text-to-Text - generates text from video and text input",
	"any-to-any":                "Any-to-Any - handles multiple modalities",
}

// analyzeAudio analyzes audio model metadata.
func analyzeAudio(files []FileInfo, metadata map[string]interface{}) *AudioInfo {
	info := &AudioInfo{}

	// Parse preprocessor_config.json for audio features
	if preprocessor, ok := metadata["preprocessor_config.json"].(map[string]interface{}); ok {
		// Feature extractor type
		if feType, ok := preprocessor["feature_extractor_type"].(string); ok {
			info.FeatureExtractorType = feType
		}
		if feType, ok := preprocessor["processor_class"].(string); ok {
			info.FeatureExtractorType = feType
		}

		// Sample rate
		if sr, ok := preprocessor["sampling_rate"].(float64); ok {
			info.SampleRate = int(sr)
		}

		// Mel bins
		if mel, ok := preprocessor["num_mel_bins"].(float64); ok {
			info.NumMelBins = int(mel)
		}

		// Max length
		if ml, ok := preprocessor["max_length"].(float64); ok {
			info.MaxLength = int(ml)
		}
	}

	// Parse config.json for additional info
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		// Task type from config
		if taskType, ok := config["task_specific_params"].(map[string]interface{}); ok {
			for task := range taskType {
				info.Task = task
				break
			}
		}

		// Languages for multilingual models
		if langs, ok := config["forced_decoder_ids"].([]interface{}); ok && len(langs) > 0 {
			// Whisper-style language detection
			info.Languages = append(info.Languages, "multilingual")
		}
	}

	// Detect task from file patterns if not set
	if info.Task == "" {
		info.Task = detectAudioTask(files, metadata)
	}

	// Set task description
	if desc, ok := audioTaskDescriptions[info.Task]; ok {
		info.TaskDescription = desc
	}

	// Detect framework
	info.Framework = detectFramework(files)

	return info
}

// analyzeVision analyzes vision model metadata.
func analyzeVision(files []FileInfo, metadata map[string]interface{}) *VisionInfo {
	info := &VisionInfo{}

	// Parse preprocessor_config.json for image features
	if preprocessor, ok := metadata["preprocessor_config.json"].(map[string]interface{}); ok {
		// Image processor type
		if ipType, ok := preprocessor["image_processor_type"].(string); ok {
			info.ImageProcessorType = ipType
		}
		if ipType, ok := preprocessor["processor_class"].(string); ok {
			info.ImageProcessorType = ipType
		}

		// Image size
		if size, ok := preprocessor["size"].(map[string]interface{}); ok {
			if h, ok := size["height"].(float64); ok {
				info.ImageSize.Height = int(h)
			}
			if w, ok := size["width"].(float64); ok {
				info.ImageSize.Width = int(w)
			}
		}
		// Alternative size format
		if size, ok := preprocessor["size"].(float64); ok {
			info.ImageSize.Height = int(size)
			info.ImageSize.Width = int(size)
		}

		// Normalization
		if mean, ok := preprocessor["image_mean"].([]interface{}); ok {
			info.Normalization = &ImageNormalization{}
			for _, v := range mean {
				if f, ok := v.(float64); ok {
					info.Normalization.Mean = append(info.Normalization.Mean, f)
				}
			}
		}
		if std, ok := preprocessor["image_std"].([]interface{}); ok {
			if info.Normalization == nil {
				info.Normalization = &ImageNormalization{}
			}
			for _, v := range std {
				if f, ok := v.(float64); ok {
					info.Normalization.Std = append(info.Normalization.Std, f)
				}
			}
		}
	}

	// Parse config.json for additional info
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		// Number of labels
		if numLabels, ok := config["num_labels"].(float64); ok {
			info.NumLabels = int(numLabels)
		}

		// Number of channels
		if numChannels, ok := config["num_channels"].(float64); ok {
			info.NumChannels = int(numChannels)
		}
	}

	// Default channels
	if info.NumChannels == 0 {
		info.NumChannels = 3 // RGB default
	}

	// Detect task from file patterns if not set
	if info.Task == "" {
		info.Task = detectVisionTask(files, metadata)
	}

	// Set task description
	if desc, ok := visionTaskDescriptions[info.Task]; ok {
		info.TaskDescription = desc
	}

	// Detect framework
	info.Framework = detectFramework(files)

	return info
}

// analyzeMultimodal analyzes multimodal model metadata.
func analyzeMultimodal(files []FileInfo, metadata map[string]interface{}) *MultimodalInfo {
	info := &MultimodalInfo{
		Modalities: []string{},
	}

	// Parse processor_config.json
	if processor, ok := metadata["processor_config.json"].(map[string]interface{}); ok {
		if procType, ok := processor["processor_class"].(string); ok {
			info.ProcessorType = procType
		}
	}

	// Parse config.json for model architecture
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		// Vision encoder
		if visionConfig, ok := config["vision_config"].(map[string]interface{}); ok {
			if model, ok := visionConfig["model_type"].(string); ok {
				info.VisionEncoder = model
			}
			// Image size from vision config
			if imgSize, ok := visionConfig["image_size"].(float64); ok {
				info.ImageSize.Height = int(imgSize)
				info.ImageSize.Width = int(imgSize)
			}
			info.Modalities = append(info.Modalities, "image")
		}

		// Text encoder
		if textConfig, ok := config["text_config"].(map[string]interface{}); ok {
			if model, ok := textConfig["model_type"].(string); ok {
				info.TextEncoder = model
			}
			if maxLen, ok := textConfig["max_position_embeddings"].(float64); ok {
				info.MaxTextLength = int(maxLen)
			}
			info.Modalities = append(info.Modalities, "text")
		}

		// Check for audio modality
		if _, ok := config["audio_config"].(map[string]interface{}); ok {
			info.Modalities = append(info.Modalities, "audio")
		}
	}

	// Detect task
	info.Task = detectMultimodalTask(files, metadata)
	if desc, ok := multimodalTaskDescriptions[info.Task]; ok {
		info.TaskDescription = desc
	}

	// Detect framework
	info.Framework = detectFramework(files)

	return info
}

// analyzeONNX analyzes ONNX model files.
func analyzeONNX(files []FileInfo) *ONNXInfo {
	info := &ONNXInfo{
		Models:   []ONNXModel{},
		Runtimes: []string{"onnxruntime", "onnxruntime-gpu"},
	}

	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".onnx") {
			continue
		}

		model := ONNXModel{
			Path:      f.Path,
			Name:      strings.TrimSuffix(f.Name, ".onnx"),
			Size:      f.Size,
			SizeHuman: f.SizeHuman,
		}

		// Detect variant from filename
		nameLower := strings.ToLower(f.Name)
		if strings.Contains(nameLower, "fp16") {
			model.Variant = "fp16"
		} else if strings.Contains(nameLower, "int8") || strings.Contains(nameLower, "quantized") {
			model.Variant = "int8"
			info.Quantized = true
		} else if strings.Contains(nameLower, "int4") {
			model.Variant = "int4"
			info.Quantized = true
		} else {
			model.Variant = "fp32"
		}

		// Check if optimized
		if strings.Contains(nameLower, "optimized") || strings.Contains(nameLower, "opt") {
			model.Optimized = true
			info.Optimized = true
		}

		info.Models = append(info.Models, model)
	}

	// Add TensorRT if CUDA models found
	for _, m := range info.Models {
		if strings.Contains(strings.ToLower(m.Name), "cuda") || strings.Contains(strings.ToLower(m.Name), "gpu") {
			info.Runtimes = append(info.Runtimes, "tensorrt")
			break
		}
	}

	return info
}

// detectAudioTask detects the audio task from files and metadata.
func detectAudioTask(files []FileInfo, metadata map[string]interface{}) string {
	// Check config.json for model type hints
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		if modelType, ok := config["model_type"].(string); ok {
			modelType = strings.ToLower(modelType)
			if strings.Contains(modelType, "whisper") || strings.Contains(modelType, "wav2vec") ||
				strings.Contains(modelType, "speech") || strings.Contains(modelType, "asr") {
				return "automatic-speech-recognition"
			}
			if strings.Contains(modelType, "tts") || strings.Contains(modelType, "vits") ||
				strings.Contains(modelType, "speecht5") {
				return "text-to-speech"
			}
		}
	}

	// Check for specific file patterns
	for _, f := range files {
		name := strings.ToLower(f.Name)
		if strings.Contains(name, "asr") || strings.Contains(name, "stt") {
			return "automatic-speech-recognition"
		}
		if strings.Contains(name, "tts") {
			return "text-to-speech"
		}
	}

	return "audio-classification" // Default
}

// detectVisionTask detects the vision task from files and metadata.
func detectVisionTask(files []FileInfo, metadata map[string]interface{}) string {
	// Check config.json for model type hints
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		if modelType, ok := config["model_type"].(string); ok {
			modelType = strings.ToLower(modelType)
			if strings.Contains(modelType, "detr") || strings.Contains(modelType, "yolo") {
				return "object-detection"
			}
			if strings.Contains(modelType, "segformer") || strings.Contains(modelType, "mask2former") {
				return "image-segmentation"
			}
			if strings.Contains(modelType, "depth") || strings.Contains(modelType, "dpt") {
				return "depth-estimation"
			}
		}

		// Check architectures
		if arch, ok := config["architectures"].([]interface{}); ok && len(arch) > 0 {
			archStr := strings.ToLower(arch[0].(string))
			if strings.Contains(archStr, "fordetection") {
				return "object-detection"
			}
			if strings.Contains(archStr, "forsegmentation") || strings.Contains(archStr, "segmentation") {
				return "image-segmentation"
			}
			if strings.Contains(archStr, "forclassification") || strings.Contains(archStr, "classification") {
				return "image-classification"
			}
		}
	}

	return "image-classification" // Default
}

// detectMultimodalTask detects the multimodal task from files and metadata.
func detectMultimodalTask(files []FileInfo, metadata map[string]interface{}) string {
	// Check config.json for model type hints
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		if modelType, ok := config["model_type"].(string); ok {
			modelType = strings.ToLower(modelType)
			if strings.Contains(modelType, "llava") || strings.Contains(modelType, "idefics") ||
				strings.Contains(modelType, "paligemma") || strings.Contains(modelType, "qwen2_vl") {
				return "image-text-to-text"
			}
			if strings.Contains(modelType, "blip") || strings.Contains(modelType, "git") {
				return "image-to-text"
			}
			if strings.Contains(modelType, "vqa") {
				return "visual-question-answering"
			}
		}

		// Check architectures
		if arch, ok := config["architectures"].([]interface{}); ok && len(arch) > 0 {
			archStr := strings.ToLower(arch[0].(string))
			if strings.Contains(archStr, "forcausallm") || strings.Contains(archStr, "conditiongeneration") {
				return "image-text-to-text"
			}
		}
	}

	return "image-to-text" // Default
}

// detectFramework detects the model framework from files.
func detectFramework(files []FileInfo) string {
	for _, f := range files {
		name := strings.ToLower(f.Name)
		if strings.HasSuffix(name, ".safetensors") || strings.HasSuffix(name, ".bin") {
			return "transformers"
		}
		if strings.HasSuffix(name, ".onnx") {
			return "onnx"
		}
		if strings.HasSuffix(name, ".pt") || strings.HasSuffix(name, ".pth") {
			return "pytorch"
		}
	}
	return "transformers"
}

// isAudioModel checks if the repository is an audio model.
func isAudioModel(files []FileInfo, metadata map[string]interface{}) bool {
	// Check preprocessor_config.json for audio features
	if preprocessor, ok := metadata["preprocessor_config.json"].(map[string]interface{}); ok {
		if _, ok := preprocessor["sampling_rate"]; ok {
			return true
		}
		if _, ok := preprocessor["num_mel_bins"]; ok {
			return true
		}
		if feType, ok := preprocessor["feature_extractor_type"].(string); ok {
			feType = strings.ToLower(feType)
			if strings.Contains(feType, "audio") || strings.Contains(feType, "speech") ||
				strings.Contains(feType, "wav2vec") || strings.Contains(feType, "whisper") {
				return true
			}
		}
	}

	// Check config.json model type
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		if modelType, ok := config["model_type"].(string); ok {
			modelType = strings.ToLower(modelType)
			audioTypes := []string{"whisper", "wav2vec", "hubert", "speech", "audio", "tts", "vits", "speecht5"}
			for _, t := range audioTypes {
				if strings.Contains(modelType, t) {
					return true
				}
			}
		}
	}

	return false
}

// isVisionModel checks if the repository is a vision model.
func isVisionModel(files []FileInfo, metadata map[string]interface{}) bool {
	// Check preprocessor_config.json for image features
	if preprocessor, ok := metadata["preprocessor_config.json"].(map[string]interface{}); ok {
		if _, ok := preprocessor["image_mean"]; ok {
			return true
		}
		if _, ok := preprocessor["image_std"]; ok {
			return true
		}
		if ipType, ok := preprocessor["image_processor_type"].(string); ok {
			return ipType != ""
		}
	}

	// Check config.json model type
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		if modelType, ok := config["model_type"].(string); ok {
			modelType = strings.ToLower(modelType)
			visionTypes := []string{"vit", "resnet", "convnext", "swin", "deit", "beit", "detr", "yolos", "segformer", "dpt"}
			for _, t := range visionTypes {
				if strings.Contains(modelType, t) {
					return true
				}
			}
		}
	}

	return false
}

// isMultimodalModel checks if the repository is a multimodal model.
func isMultimodalModel(files []FileInfo, metadata map[string]interface{}) bool {
	// Check config.json for multimodal indicators
	if config, ok := metadata["config.json"].(map[string]interface{}); ok {
		// Multiple modality configs present
		hasVision := false
		hasText := false

		if _, ok := config["vision_config"]; ok {
			hasVision = true
		}
		if _, ok := config["text_config"]; ok {
			hasText = true
		}

		if hasVision && hasText {
			return true
		}

		// Check model type
		if modelType, ok := config["model_type"].(string); ok {
			modelType = strings.ToLower(modelType)
			multimodalTypes := []string{"llava", "blip", "clip", "flava", "git", "pix2struct",
				"idefics", "fuyu", "paligemma", "qwen2_vl", "internvl", "cogvlm"}
			for _, t := range multimodalTypes {
				if strings.Contains(modelType, t) {
					return true
				}
			}
		}
	}

	// Check for processor_config.json (common in multimodal)
	if _, ok := metadata["processor_config.json"]; ok {
		return true
	}

	return false
}

// hasONNXFiles checks if the repository contains ONNX files.
func hasONNXFiles(files []FileInfo) bool {
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.Name), ".onnx") {
			return true
		}
	}
	return false
}

// detectSpecializedType determines if a model is audio, vision, or multimodal.
// Returns the detected type or empty string if not a specialized model.
func detectSpecializedType(files []FileInfo, metadata map[string]interface{}) RepoType {
	// Check in order of specificity
	if isMultimodalModel(files, metadata) {
		return TypeMultimodal
	}
	if isAudioModel(files, metadata) {
		return TypeAudio
	}
	if isVisionModel(files, metadata) {
		return TypeVision
	}
	if hasONNXFiles(files) {
		return TypeONNX
	}
	return ""
}

// getONNXModelDir returns the directory containing ONNX files.
func getONNXModelDir(files []FileInfo) string {
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.Name), ".onnx") {
			return filepath.Dir(f.Path)
		}
	}
	return ""
}
