// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// items builds plan items from "path" (LFS) and "path|small" (non-LFS) specs.
func items(specs ...string) []PlanItem {
	var out []PlanItem
	for _, s := range specs {
		p, small := strings.CutSuffix(s, "|small")
		out = append(out, PlanItem{RelativePath: p, LFS: !small})
	}
	return out
}

func selectedPaths(its []PlanItem) []string {
	var out []string
	for _, it := range its {
		out = append(out, it.RelativePath)
	}
	sort.Strings(out)
	return out
}

// The repo layouts below are taken from repos where repo-type QA found the
// old name-only, six-extension filtering selecting the wrong files.
func TestApplyFilters_RealLayouts(t *testing.T) {
	tinyllama := items(
		"model.safetensors", "pytorch_model.bin", "tokenizer.model", "Responsible-Use-Guide.pdf",
		"config.json|small", "tokenizer.json|small",
	)
	gpt2 := items(
		"model.safetensors", "pytorch_model.bin", "tf_model.h5", "flax_model.msgpack",
		"rust_model.ot", "64-8bits.tflite", "onnx/decoder_model.onnx", "onnx/config.json|small",
		"config.json|small", "tokenizer.json|small", "README.md|small",
	)
	sdPipe := items(
		"unet/diffusion_pytorch_model.safetensors", "unet/diffusion_pytorch_model.fp16.safetensors",
		"unet/diffusion_pytorch_model.msgpack", "unet/config.json|small",
		"vae/diffusion_pytorch_model.safetensors", "vae/config.json|small",
		"text_encoder/model.safetensors", "text_encoder/config.json|small",
		"sd_xl_base_1.0_0.9vae.safetensors", "model_index.json|small",
	)
	unsloth := items(
		"imatrix_unsloth.dat", "Q4_K_M/Model-Q4_K_M-00001-of-00002.gguf", "Q4_K_M/Model-Q4_K_M-00002-of-00002.gguf",
		"Q6_K/Model-Q6_K-00001-of-00001.gguf", "Q6_K_XL/Model-UD-Q6_K_XL.gguf", "Q4_K_M/eval/results.json|small",
		"Model-Q8_0.gguf", "mmproj-F16.gguf", "README.md|small",
	)
	glue := items(
		"cola/train-00000-of-00001.parquet", "cola/validation-00000-of-00001.parquet", "cola/test-00000-of-00001.parquet",
		"mnli/train-00000-of-00001.parquet", "mnli/validation_matched-00000-of-00001.parquet", "README.md|small",
	)

	cases := []struct {
		name    string
		its     []PlanItem
		filters []string
		exact   bool
		want    []string
	}{
		{"no filters keeps everything", gpt2, nil, false, selectedPaths(gpt2)},
		{"blank filters keep everything", gpt2, []string{" ", ""}, false, selectedPaths(gpt2)},
		{"safetensors drops every other weight format", gpt2, []string{"safetensors"}, false,
			[]string{"README.md", "config.json", "model.safetensors", "onnx/config.json", "tokenizer.json"}},
		{"LFS tokenizer and docs survive a weight filter (Llama/Mistral/Gemma)", tinyllama, []string{"safetensors"}, false,
			[]string{"Responsible-Use-Guide.pdf", "config.json", "model.safetensors", "tokenizer.json", "tokenizer.model"}},
		{"small (non-LFS) files never go through filters", items(
			"model.safetensors", "Iris.csv|small", "train.csv|small", "pytorch_model.bin"), []string{"safetensors"}, false,
			[]string{"Iris.csv", "model.safetensors", "train.csv"}},
		{"variant filter keeps weightless components (sd-turbo -F fp16)", items(
			"unet/diffusion_pytorch_model.safetensors", "unet/diffusion_pytorch_model.fp16.safetensors",
			"tokenizer/vocab.json|small", "tokenizer/merges.txt|small", "scheduler/scheduler_config.json|small",
			"model_index.json|small"), []string{"fp16"}, false,
			[]string{"model_index.json", "scheduler/scheduler_config.json", "tokenizer/merges.txt",
				"tokenizer/vocab.json", "unet/diffusion_pytorch_model.fp16.safetensors"}},
		{"component folder selects its weights and config", sdPipe, []string{"unet"}, true,
			[]string{"model_index.json", "text_encoder/config.json", "unet/config.json", "unet/diffusion_pytorch_model.fp16.safetensors",
				"unet/diffusion_pytorch_model.msgpack", "unet/diffusion_pytorch_model.safetensors", "vae/config.json"}},
		{"exact vae does not match 0.9vae checkpoint", sdPipe, []string{"vae"}, true,
			[]string{"model_index.json", "text_encoder/config.json", "unet/config.json", "vae/config.json", "vae/diffusion_pytorch_model.safetensors"}},
		{"quant folder selects all shards, drops imatrix and other quants", unsloth, []string{"q4_k_m"}, true,
			[]string{"Q4_K_M/Model-Q4_K_M-00001-of-00002.gguf", "Q4_K_M/Model-Q4_K_M-00002-of-00002.gguf", "Q4_K_M/eval/results.json", "README.md"}},
		{"exact q6_k excludes q6_k_xl folder and file", unsloth, []string{"q6_k"}, true,
			[]string{"Q4_K_M/eval/results.json", "Q6_K/Model-Q6_K-00001-of-00001.gguf", "README.md"}},
		{"folder filter with slash", unsloth, []string{"Q4_K_M/"}, false,
			[]string{"Q4_K_M/Model-Q4_K_M-00001-of-00002.gguf", "Q4_K_M/Model-Q4_K_M-00002-of-00002.gguf", "Q4_K_M/eval/results.json", "README.md"}},
		{"full-name mmproj filter plus quant", unsloth, []string{"q8_0", "mmproj-f16"}, true,
			[]string{"Model-Q8_0.gguf", "Q4_K_M/eval/results.json", "README.md", "mmproj-F16.gguf"}},
		{"spaces around comma-separated filters", unsloth, []string{" q8_0", " mmproj-f16 "}, true,
			[]string{"Model-Q8_0.gguf", "Q4_K_M/eval/results.json", "README.md", "mmproj-F16.gguf"}},
		{"exact extension filter", gpt2, []string{".bin"}, true,
			[]string{"README.md", "config.json", "onnx/config.json", "pytorch_model.bin", "tokenizer.json"}},
		{"dataset split filter drops other splits", glue, []string{"validation"}, false,
			[]string{"README.md", "cola/validation-00000-of-00001.parquet", "mnli/validation_matched-00000-of-00001.parquet"}},
		{"dataset config folder", glue, []string{"cola/"}, true,
			[]string{"README.md", "cola/test-00000-of-00001.parquet", "cola/train-00000-of-00001.parquet", "cola/validation-00000-of-00001.parquet"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectedPaths(applyFilters(tc.its, tc.filters, tc.exact))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestApplyFilters_SubdirRecordsLongestFilter(t *testing.T) {
	got := applyFilters(items("Model-Q4_K_M.gguf", "Model-Q4_K.gguf"), []string{"Q4_K", "Q4_K_M"}, false)
	subdirs := map[string]string{}
	for _, it := range got {
		subdirs[it.RelativePath] = it.Subdir
	}
	if subdirs["Model-Q4_K_M.gguf"] != "Q4_K_M" || subdirs["Model-Q4_K.gguf"] != "Q4_K" {
		t.Errorf("Subdir = %v; want the longest matching filter, as given", subdirs)
	}
}

func TestUnmatchedFiltersWarning(t *testing.T) {
	plan := &Plan{Items: applyFilters(items("Model-Q8_0.gguf", "README.md|small"), []string{"q4_k_m"}, true)}
	if w := UnmatchedFiltersWarning(Job{Repo: "o/r", Filters: []string{"q4_k_m"}}, plan); !strings.Contains(w, "q4_k_m") {
		t.Errorf("expected a warning naming the filter, got %q", w)
	}
	plan = &Plan{Items: applyFilters(items("Model-Q8_0.gguf"), []string{"q8_0"}, true)}
	if w := UnmatchedFiltersWarning(Job{Repo: "o/r", Filters: []string{"q8_0"}}, plan); w != "" {
		t.Errorf("unexpected warning: %q", w)
	}
	if w := UnmatchedFiltersWarning(Job{Repo: "o/r"}, &Plan{}); w != "" {
		t.Errorf("unexpected warning without filters: %q", w)
	}
}
