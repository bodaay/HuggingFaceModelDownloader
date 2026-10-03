// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"strings"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/filtermatch"
)

// formatFilterSelects reports whether an item's filter selects path.
func formatFilterSelects(filter, path string) bool {
	for _, f := range strings.Split(filter, ",") {
		if filtermatch.Match(strings.ToLower(path), strings.TrimSpace(strings.ToLower(f)), true) {
			return true
		}
	}
	return false
}

// openai/whisper-tiny / gpt2-style repos ship the same weights in several
// formats; each format item must select exactly its own files.
func TestWeightFormatItems(t *testing.T) {
	lfs := func(p string, size int64) FileInfo { return FileInfo{Path: p, Name: p, IsLFS: true, Size: size} }
	files := []FileInfo{
		lfs("model.safetensors", 100), lfs("pytorch_model.bin", 100), lfs("tf_model.h5", 100),
		lfs("flax_model.msgpack", 100), lfs("onnx/encoder_model.onnx", 40), lfs("onnx/decoder_model.onnx", 60),
		lfs("openvino/openvino_model.bin", 90), lfs("training_args.bin", 1),
		{Path: "config.json", Name: "config.json"},
	}
	items := WeightFormatItems(files)
	byID := map[string]SelectableItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, id := range []string{"safetensors", "pytorch", "onnx", "tensorflow", "flax", "openvino"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing format %q (have %v)", id, len(items))
		}
	}
	if !byID["safetensors"].Recommended {
		t.Error("safetensors not recommended")
	}
	if byID["onnx"].Size != 100 || byID["pytorch"].Size != 100 {
		t.Errorf("sizes: onnx %d pytorch %d", byID["onnx"].Size, byID["pytorch"].Size)
	}
	// Each filter selects its own weights and no other format's weights.
	for _, it := range items {
		for _, f := range files {
			if !f.IsLFS {
				continue
			}
			sel := formatFilterSelects(it.FilterValue, f.Path)
			own := false
			for _, wf := range weightFormats {
				if wf.id == it.ID && wf.match(strings.ToLower(f.Path)) {
					own = true
				}
			}
			if it.ID == "pytorch" && f.Path == "openvino/openvino_model.bin" {
				own = false
			}
			if sel != own && f.Path != "training_args.bin" {
				t.Errorf("format %s filter %q selects %s = %v, want %v", it.ID, it.FilterValue, f.Path, sel, own)
			}
		}
	}
	if items := WeightFormatItems(files[:1]); items != nil {
		t.Error("single-format repo should offer no format choice")
	}
}
