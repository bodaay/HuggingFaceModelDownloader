// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// analyzeTransformers performs detailed analysis of a Transformers model.
func analyzeTransformers(files []FileInfo, metadata map[string]interface{}) *TransformersInfo {
	info := &TransformersInfo{
		Backends: []string{"transformers", "vLLM", "text-generation-inference"},
	}

	// Parse config.json for model architecture details
	if cfg, ok := metadata["config.json"].(map[string]interface{}); ok {
		parseTransformersConfig(cfg, info)
	}

	// Parse tokenizer_config.json for tokenizer details
	if tokCfg, ok := metadata["tokenizer_config.json"].(map[string]interface{}); ok {
		info.Tokenizer = parseTokenizerConfig(tokCfg)
	}

	// Parse generation_config.json if present
	if genCfg, ok := metadata["generation_config.json"].(map[string]interface{}); ok {
		info.GenerationConfig = genCfg
	}

	// Analyze weight files
	info.WeightFiles = analyzeWeightFiles(files)

	// Detect precision from files
	if info.Precision == "" {
		info.Precision = detectPrecisionFromFiles(files)
	}

	// Detect sharding
	detectSharding(info)

	// Estimate parameters if not already set
	if info.EstimatedParameters == "" && info.HiddenSize > 0 && info.NumHiddenLayers > 0 {
		info.EstimatedParametersNum = estimateParameters(info)
		info.EstimatedParameters = formatParameterCount(info.EstimatedParametersNum)
	}

	// Infer task from architecture
	if info.Task == "" && info.Architecture != "" {
		info.Task, info.TaskDescription = inferTaskFromArchitecture(info.Architecture)
	}

	return info
}

// parseTransformersConfig extracts model configuration from config.json.
func parseTransformersConfig(cfg map[string]interface{}, info *TransformersInfo) {
	// Get architectures array
	if archs, ok := cfg["architectures"].([]interface{}); ok && len(archs) > 0 {
		if arch, ok := archs[0].(string); ok {
			info.Architecture = arch
			info.ArchitectureDescription = describeArchitecture(arch)
		}
	}

	// Model type
	if mt, ok := cfg["model_type"].(string); ok {
		info.ModelType = mt
	}

	// Hidden size
	if hs, ok := cfg["hidden_size"].(float64); ok {
		info.HiddenSize = int(hs)
	}

	// Number of hidden layers
	if nl, ok := cfg["num_hidden_layers"].(float64); ok {
		info.NumHiddenLayers = int(nl)
	}

	// Number of attention heads
	if nh, ok := cfg["num_attention_heads"].(float64); ok {
		info.NumAttentionHeads = int(nh)
	}

	// Intermediate size
	if is, ok := cfg["intermediate_size"].(float64); ok {
		info.IntermediateSize = int(is)
	}

	// Vocabulary size
	if vs, ok := cfg["vocab_size"].(float64); ok {
		info.VocabSize = int(vs)
	}

	// Max position embeddings
	if mp, ok := cfg["max_position_embeddings"].(float64); ok {
		info.MaxPositionEmbeddings = int(mp)
		info.ContextLength = int(mp)
	}

	// Some models use different keys for context length
	if cl, ok := cfg["max_seq_len"].(float64); ok {
		info.ContextLength = int(cl)
	}
	if cl, ok := cfg["sliding_window"].(float64); ok && int(cl) > 0 {
		info.ContextLength = int(cl)
	}

	// Torch dtype
	if td, ok := cfg["torch_dtype"].(string); ok {
		info.TorchDtype = td
		info.Precision = normalizePrecision(td)
	}

	// BOS/EOS tokens
	specialTokens := make(map[string]interface{})
	if bos, ok := cfg["bos_token_id"]; ok {
		specialTokens["bos_token_id"] = bos
	}
	if eos, ok := cfg["eos_token_id"]; ok {
		specialTokens["eos_token_id"] = eos
	}
	if pad, ok := cfg["pad_token_id"]; ok {
		specialTokens["pad_token_id"] = pad
	}
	if len(specialTokens) > 0 {
		info.SpecialTokens = specialTokens
	}
}

// parseTokenizerConfig extracts tokenizer information.
func parseTokenizerConfig(cfg map[string]interface{}) *TokenizerInfo {
	tok := &TokenizerInfo{}

	if tc, ok := cfg["tokenizer_class"].(string); ok {
		tok.Type = tc
	}

	if vs, ok := cfg["vocab_size"].(float64); ok {
		tok.VocabSize = int(vs)
	}

	if ml, ok := cfg["model_max_length"].(float64); ok {
		tok.ModelMaxLength = int(ml)
	}

	if ps, ok := cfg["padding_side"].(string); ok {
		tok.PaddingSide = ps
	}

	if ts, ok := cfg["truncation_side"].(string); ok {
		tok.TruncationSide = ts
	}

	if ab, ok := cfg["add_bos_token"].(bool); ok {
		tok.AddBosToken = ab
	}

	if ae, ok := cfg["add_eos_token"].(bool); ok {
		tok.AddEosToken = ae
	}

	if ct, ok := cfg["chat_template"].(string); ok && ct != "" {
		tok.ChatTemplate = ct
		tok.HasChatTemplate = true
	}

	return tok
}

// analyzeWeightFiles extracts information about model weight files.
func analyzeWeightFiles(files []FileInfo) []WeightFile {
	var weights []WeightFile

	shardPattern := regexp.MustCompile(`[-_](\d+)[-_]of[-_](\d+)`)

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".safetensors" && ext != ".bin" && ext != ".pt" && ext != ".pth" {
			continue
		}

		// Skip non-model files
		lower := strings.ToLower(f.Name)
		if strings.Contains(lower, "optimizer") || strings.Contains(lower, "training") {
			continue
		}

		wf := WeightFile{
			Path:      f.Path,
			Name:      f.Name,
			Size:      f.Size,
			SizeHuman: f.SizeHuman,
		}

		// Determine format
		switch ext {
		case ".safetensors":
			wf.Format = "safetensors"
		case ".bin":
			wf.Format = "pytorch_bin"
		case ".pt", ".pth":
			wf.Format = "pytorch"
		}

		// Check for sharding pattern
		if matches := shardPattern.FindStringSubmatch(f.Name); len(matches) == 3 {
			if idx, err := strconv.Atoi(matches[1]); err == nil {
				wf.ShardIndex = idx
			}
			if total, err := strconv.Atoi(matches[2]); err == nil {
				wf.ShardTotal = total
			}
		}

		weights = append(weights, wf)
	}

	// Sort by shard index
	sort.Slice(weights, func(i, j int) bool {
		if weights[i].ShardIndex != weights[j].ShardIndex {
			return weights[i].ShardIndex < weights[j].ShardIndex
		}
		return weights[i].Name < weights[j].Name
	})

	return weights
}

// detectPrecisionFromFiles infers precision from file names and sizes.
func detectPrecisionFromFiles(files []FileInfo) string {
	for _, f := range files {
		lower := strings.ToLower(f.Name)
		if strings.Contains(lower, "fp16") || strings.Contains(lower, "float16") {
			return "fp16"
		}
		if strings.Contains(lower, "bf16") || strings.Contains(lower, "bfloat16") {
			return "bf16"
		}
		if strings.Contains(lower, "fp32") || strings.Contains(lower, "float32") {
			return "fp32"
		}
	}
	return "fp32" // Default assumption
}

// detectSharding determines if the model is sharded.
func detectSharding(info *TransformersInfo) {
	if len(info.WeightFiles) == 0 {
		return
	}

	// Check if any file has shard information
	for _, wf := range info.WeightFiles {
		if wf.ShardTotal > 1 {
			info.IsSharded = true
			info.ShardCount = wf.ShardTotal
			return
		}
	}

	// Check for multiple safetensors/bin files (implicit sharding)
	safetensorsCount := 0
	binCount := 0
	for _, wf := range info.WeightFiles {
		if wf.Format == "safetensors" {
			safetensorsCount++
		} else if wf.Format == "pytorch_bin" {
			binCount++
		}
	}

	if safetensorsCount > 1 || binCount > 1 {
		info.IsSharded = true
		if safetensorsCount > binCount {
			info.ShardCount = safetensorsCount
		} else {
			info.ShardCount = binCount
		}
	}
}

// estimateParameters estimates the parameter count based on architecture.
func estimateParameters(info *TransformersInfo) int64 {
	// For transformer models, approximate formula:
	// params ≈ 12 * L * H^2 (where L = layers, H = hidden size)
	// This is a rough estimate that works for most decoder-only models
	if info.HiddenSize > 0 && info.NumHiddenLayers > 0 {
		h := int64(info.HiddenSize)
		l := int64(info.NumHiddenLayers)
		v := int64(info.VocabSize)

		// Embedding: vocab_size * hidden_size
		embedding := v * h

		// Attention: 4 * hidden_size^2 per layer (Q, K, V, O projections)
		attention := 4 * h * h * l

		// FFN: 2 * hidden_size * intermediate_size per layer
		var ffn int64
		if info.IntermediateSize > 0 {
			ffn = 2 * h * int64(info.IntermediateSize) * l
		} else {
			// Assume intermediate_size = 4 * hidden_size (common default)
			ffn = 8 * h * h * l
		}

		// Layer norms: 2 * hidden_size per layer + 1 final
		layerNorm := 2 * h * l + h

		return embedding + attention + ffn + layerNorm
	}
	return 0
}

// formatParameterCount formats a parameter count as a human-readable string.
func formatParameterCount(params int64) string {
	if params == 0 {
		return ""
	}

	billion := int64(1_000_000_000)
	million := int64(1_000_000)

	if params >= billion {
		b := float64(params) / float64(billion)
		if b >= 100 {
			return strconv.FormatInt(int64(b), 10) + "B"
		}
		return strconv.FormatFloat(b, 'f', 1, 64) + "B"
	} else if params >= million {
		m := float64(params) / float64(million)
		return strconv.FormatFloat(m, 'f', 0, 64) + "M"
	}
	return strconv.FormatInt(params, 10)
}

// normalizePrecision converts torch dtype to precision string.
func normalizePrecision(dtype string) string {
	switch strings.ToLower(dtype) {
	case "float16", "torch.float16":
		return "fp16"
	case "bfloat16", "torch.bfloat16":
		return "bf16"
	case "float32", "torch.float32":
		return "fp32"
	case "float64", "torch.float64":
		return "fp64"
	default:
		return dtype
	}
}

// inferTaskFromArchitecture determines the task from the architecture class name.
func inferTaskFromArchitecture(arch string) (string, string) {
	lower := strings.ToLower(arch)

	// Causal LM / Text Generation
	if strings.Contains(lower, "causallm") || strings.Contains(lower, "gpt") ||
		strings.Contains(lower, "llama") || strings.Contains(lower, "mistral") ||
		strings.Contains(lower, "falcon") || strings.Contains(lower, "codegen") {
		return "text-generation", "Autoregressive text generation (chat, completion)"
	}

	// Sequence Classification
	if strings.Contains(lower, "sequenceclassification") || strings.Contains(lower, "forsequence") {
		return "text-classification", "Text classification (sentiment, topic)"
	}

	// Token Classification
	if strings.Contains(lower, "tokenclassification") || strings.Contains(lower, "fortoken") {
		return "token-classification", "Token classification (NER, POS tagging)"
	}

	// Question Answering
	if strings.Contains(lower, "questionanswering") || strings.Contains(lower, "forqa") {
		return "question-answering", "Extractive question answering"
	}

	// Masked LM
	if strings.Contains(lower, "maskedlm") || strings.Contains(lower, "formasked") {
		return "fill-mask", "Fill-mask (masked language modeling)"
	}

	// Seq2Seq / Conditional Generation
	if strings.Contains(lower, "conditionalgeneration") || strings.Contains(lower, "seq2seq") ||
		strings.Contains(lower, "t5") || strings.Contains(lower, "bart") {
		return "text2text-generation", "Text-to-text generation (translation, summarization)"
	}

	// Feature Extraction
	if strings.Contains(lower, "model") && !strings.Contains(lower, "for") {
		return "feature-extraction", "Feature extraction (embeddings)"
	}

	return "text-generation", "General language model"
}

// describeArchitecture provides a human-readable description of the architecture.
func describeArchitecture(arch string) string {
	descriptions := map[string]string{
		"LlamaForCausalLM":              "Meta Llama decoder-only transformer",
		"MistralForCausalLM":            "Mistral AI decoder-only transformer with sliding window attention",
		"Qwen2ForCausalLM":              "Alibaba Qwen2 decoder-only transformer",
		"Phi3ForCausalLM":               "Microsoft Phi-3 small language model",
		"PhiForCausalLM":                "Microsoft Phi small language model",
		"GPT2LMHeadModel":               "OpenAI GPT-2 decoder-only transformer",
		"GPTNeoForCausalLM":             "EleutherAI GPT-Neo decoder-only transformer",
		"GPTNeoXForCausalLM":            "EleutherAI GPT-NeoX decoder-only transformer",
		"GPTJForCausalLM":               "EleutherAI GPT-J decoder-only transformer",
		"FalconForCausalLM":             "TII Falcon decoder-only transformer",
		"BertForSequenceClassification": "BERT encoder for sequence classification",
		"BertForTokenClassification":    "BERT encoder for token classification",
		"BertForQuestionAnswering":      "BERT encoder for question answering",
		"BertModel":                     "BERT encoder base model",
		"RobertaForSequenceClassification": "RoBERTa encoder for sequence classification",
		"RobertaModel":                     "RoBERTa encoder base model",
		"T5ForConditionalGeneration":       "T5 encoder-decoder for text-to-text",
		"BartForConditionalGeneration":     "BART encoder-decoder for text-to-text",
		"BloomForCausalLM":                 "BigScience BLOOM multilingual transformer",
		"OPTForCausalLM":                   "Meta OPT decoder-only transformer",
		"GemmaForCausalLM":                 "Google Gemma decoder-only transformer",
		"Gemma2ForCausalLM":                "Google Gemma 2 decoder-only transformer",
		"StableLmForCausalLM":              "StabilityAI StableLM decoder-only transformer",
		"CodeLlamaForCausalLM":             "Meta Code Llama for code generation",
		"DeepseekV2ForCausalLM":            "DeepSeek V2 decoder-only transformer",
		"InternLM2ForCausalLM":             "InternLM 2 decoder-only transformer",
	}

	if desc, ok := descriptions[arch]; ok {
		return desc
	}

	// Generate generic description
	if strings.Contains(arch, "ForCausalLM") {
		return "Decoder-only transformer for text generation"
	}
	if strings.Contains(arch, "ForSequenceClassification") {
		return "Encoder transformer for sequence classification"
	}
	if strings.Contains(arch, "ForTokenClassification") {
		return "Encoder transformer for token classification"
	}
	if strings.Contains(arch, "ForQuestionAnswering") {
		return "Encoder transformer for question answering"
	}
	if strings.Contains(arch, "ForConditionalGeneration") {
		return "Encoder-decoder transformer for text generation"
	}

	return "Transformer model"
}

// TransformersToSelectableItems converts Transformers weight files to SelectableItems.
func TransformersToSelectableItems(info *TransformersInfo, files []FileInfo) []SelectableItem {
	if info == nil {
		return nil
	}

	var items []SelectableItem

	// Group files by format
	hasSafetensors := false
	hasPytorchBin := false
	var safetensorsSize, pytorchBinSize int64

	for _, wf := range info.WeightFiles {
		if wf.Format == "safetensors" {
			hasSafetensors = true
			safetensorsSize += wf.Size
		} else if wf.Format == "pytorch_bin" {
			hasPytorchBin = true
			pytorchBinSize += wf.Size
		}
	}

	// Add format options if both are available
	if hasSafetensors && hasPytorchBin {
		items = append(items, SelectableItem{
			ID:           "safetensors",
			Label:        "SafeTensors",
			Description:  "Faster loading, recommended",
			Size:         safetensorsSize,
			SizeHuman:    humanSize(safetensorsSize),
			Quality:      5,
			QualityStars: "★★★★★",
			Recommended:  true,
			Category:     "format",
			FilterValue:  "safetensors",
		})

		items = append(items, SelectableItem{
			ID:           "pytorch",
			Label:        "PyTorch (.bin)",
			Description:  "Legacy format",
			Size:         pytorchBinSize,
			SizeHuman:    humanSize(pytorchBinSize),
			Quality:      3,
			QualityStars: "★★★☆☆",
			Recommended:  false,
			Category:     "format",
			FilterValue:  ".bin",
		})
	}

	// Detect precision variants by checking file paths
	precisions := detectPrecisionVariants(files)
	if len(precisions) > 1 {
		precisionDescriptions := map[string]string{
			"fp16": "Half precision - Recommended for inference",
			"fp32": "Full precision - Training and maximum accuracy",
			"bf16": "Brain float - Good for Ampere+ GPUs",
		}

		for _, prec := range precisions {
			desc := precisionDescriptions[prec]
			if desc == "" {
				desc = "Precision variant"
			}

			item := SelectableItem{
				ID:          prec,
				Label:       strings.ToUpper(prec),
				Description: desc,
				Recommended: prec == "fp16",
				Category:    "precision",
				FilterValue: prec,
			}
			items = append(items, item)
		}
	}

	return items
}

// detectPrecisionVariants finds available precision variants from files.
func detectPrecisionVariants(files []FileInfo) []string {
	variants := make(map[string]bool)

	for _, f := range files {
		lower := strings.ToLower(f.Path)
		if strings.Contains(lower, "fp16") || strings.Contains(lower, "float16") {
			variants["fp16"] = true
		}
		if strings.Contains(lower, "fp32") || strings.Contains(lower, "float32") {
			variants["fp32"] = true
		}
		if strings.Contains(lower, "bf16") || strings.Contains(lower, "bfloat16") {
			variants["bf16"] = true
		}
	}

	var result []string
	for v := range variants {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}
