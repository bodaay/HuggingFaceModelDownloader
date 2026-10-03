// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import "strings"

// weightFormat describes one way a repo ships its weights.
type weightFormat struct {
	id, label, desc string
	quality         int
	// filter selects this format's files with --exact (comma list allowed).
	filter string
	// match reports whether a lowercased path belongs to this format.
	match func(p string) bool
}

var weightFormats = []weightFormat{
	{"safetensors", "SafeTensors", "PyTorch weights, fast and safe loading (recommended)", 5, "safetensors",
		func(p string) bool {
			return strings.HasSuffix(p, ".safetensors") && !strings.Contains(p, "onnx/") && !strings.Contains(p, "openvino/")
		}},
	{"pytorch", "PyTorch (.bin)", "Legacy PyTorch pickle weights", 3, "",
		func(p string) bool { return strings.HasSuffix(p, ".bin") && strings.Contains(p, "pytorch_model") }},
	{"onnx", "ONNX", "ONNX Runtime / transformers.js exports", 4, ".onnx,.onnx_data",
		func(p string) bool { return strings.HasSuffix(p, ".onnx") || strings.HasSuffix(p, ".onnx_data") }},
	{"tensorflow", "TensorFlow (.h5)", "TensorFlow / Keras weights", 3, ".h5",
		func(p string) bool { return strings.HasSuffix(p, ".h5") }},
	{"tflite", "TFLite", "TensorFlow Lite (mobile) models", 3, ".tflite",
		func(p string) bool { return strings.HasSuffix(p, ".tflite") }},
	{"flax", "Flax (.msgpack)", "JAX / Flax weights", 3, ".msgpack",
		func(p string) bool { return strings.HasSuffix(p, ".msgpack") }},
	{"openvino", "OpenVINO", "Intel OpenVINO exports", 3, "openvino/",
		func(p string) bool { return strings.Contains(p, "openvino") && strings.HasSuffix(p, ".bin") }},
	{"rust", "Rust (.ot)", "rust-bert weights", 2, ".ot",
		func(p string) bool { return strings.HasSuffix(p, ".ot") }},
}

// WeightFormatItems offers one item per weight format a repo ships, when it
// ships more than one. Many repos carry the same model as PyTorch, ONNX, TF,
// Flax, TFLite and Rust weights (gpt2: 4.7 GiB in total, 0.5 GiB as
// safetensors); downloading without choosing pulls them all. SafeTensors is
// recommended, else PyTorch, else the first format found.
func WeightFormatItems(files []FileInfo) []SelectableItem {
	type found struct {
		f    weightFormat
		size int64
	}
	var formats []found
	for _, wf := range weightFormats {
		var size int64
		for _, file := range files {
			if file.IsLFS && wf.match(strings.ToLower(file.Path)) {
				size += file.Size
			}
		}
		if size == 0 {
			continue
		}
		if wf.id == "pytorch" {
			var binSize int64
			wf.filter, binSize = pytorchBinSelection(files)
			if binSize > 0 {
				size = binSize
			}
		}
		formats = append(formats, found{wf, size})
	}
	if len(formats) < 2 {
		return nil
	}

	recommended := formats[0].f.id
	for _, f := range formats {
		if f.f.id == "safetensors" {
			recommended = "safetensors"
			break
		}
		if f.f.id == "pytorch" {
			recommended = "pytorch"
		}
	}

	var items []SelectableItem
	for _, f := range formats {
		items = append(items, SelectableItem{
			ID:           f.f.id,
			Label:        f.f.label,
			Description:  f.f.desc,
			Size:         f.size,
			SizeHuman:    humanSize(f.size),
			Quality:      f.f.quality,
			QualityStars: qualityToStars(f.f.quality),
			Recommended:  f.f.id == recommended,
			Category:     "format",
			FilterValue:  f.f.filter,
		})
	}
	return items
}
