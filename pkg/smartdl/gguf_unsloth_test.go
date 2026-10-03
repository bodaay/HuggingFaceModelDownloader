// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import "testing"

// TestQuantPattern_UnslothDynamic verifies that unsloth's "Unsloth Dynamic"
// quantization filenames (the ones with a UD- prefix and _XL/_XXL suffixes, or
// 1-bit IQ1 variants) are captured by quantPattern with their full quant type,
// not a truncated prefix. Covers github issue #72 where Q4_K_XL files were
// labeled as "Q4_K" in the web UI and filter.
//
// The filenames below are taken verbatim from
// huggingface.co/unsloth/Qwen3-30B-A3B-GGUF.
func TestQuantPattern_UnslothDynamic(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		// Unsloth Dynamic XL variants — the main bug
		{"Qwen3-30B-A3B-UD-Q2_K_XL.gguf", "Q2_K_XL"},
		{"Qwen3-30B-A3B-UD-Q3_K_XL.gguf", "Q3_K_XL"},
		{"Qwen3-30B-A3B-UD-Q4_K_XL.gguf", "Q4_K_XL"},
		{"Qwen3-30B-A3B-UD-Q5_K_XL.gguf", "Q5_K_XL"},
		{"Qwen3-30B-A3B-UD-Q6_K_XL.gguf", "Q6_K_XL"},
		{"Qwen3-30B-A3B-UD-Q8_K_XL.gguf", "Q8_K_XL"},

		// 1-bit IQ variants — current regex is IQ[234] only
		{"Qwen3-30B-A3B-UD-IQ1_S.gguf", "IQ1_S"},
		{"Qwen3-30B-A3B-UD-IQ1_M.gguf", "IQ1_M"},

		// IQ2/IQ3 XXS/M variants present in unsloth repos — regex already
		// matches these but quality/description maps may be missing entries.
		{"Qwen3-30B-A3B-UD-IQ2_M.gguf", "IQ2_M"},
		{"Qwen3-30B-A3B-UD-IQ2_XXS.gguf", "IQ2_XXS"},
		{"Qwen3-30B-A3B-UD-IQ3_XXS.gguf", "IQ3_XXS"},

		// Q2_K_L (non-UD but exists in unsloth repos)
		{"Qwen3-30B-A3B-Q2_K_L.gguf", "Q2_K_L"},

		// Standard quants that should still work after the regex change
		{"Qwen3-30B-A3B-Q4_K_M.gguf", "Q4_K_M"},
		{"Qwen3-30B-A3B-Q4_K_S.gguf", "Q4_K_S"},
		{"Qwen3-30B-A3B-Q4_0.gguf", "Q4_0"},
		{"Qwen3-30B-A3B-Q8_0.gguf", "Q8_0"},
		{"Qwen3-30B-A3B-Q6_K.gguf", "Q6_K"},
		{"Qwen3-30B-A3B-IQ4_NL.gguf", "IQ4_NL"},
		{"Qwen3-30B-A3B-IQ4_XS.gguf", "IQ4_XS"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			matches := quantPattern.FindStringSubmatch(tt.file)
			if len(matches) < 2 {
				t.Fatalf("quantPattern.FindStringSubmatch(%q) = no match, want %q", tt.file, tt.want)
			}
			got := matches[1]
			// Case-insensitive regex, so compare upper-cased.
			if got != tt.want {
				t.Errorf("quantPattern.FindStringSubmatch(%q) = %q, want %q", tt.file, got, tt.want)
			}
		})
	}
}

// TestAnalyzeGGUF_UnslothRepoCoverage simulates the full analyzeGGUF pipeline
// against the file set from a real unsloth Qwen3-30B-A3B-GGUF repo and asserts
// every distinct quant filename produces a distinct, correctly-labeled entry.
// This reproduces the user-visible symptom in issue #72: before the fix, the
// six UD-Q*_K_XL files collide into six entries labeled Q2_K..Q8_K, so the
// web UI appears to be "missing" the XL variants.
func TestAnalyzeGGUF_UnslothRepoCoverage(t *testing.T) {
	ggufNames := []string{
		"Qwen3-30B-A3B-IQ4_NL.gguf",
		"Qwen3-30B-A3B-IQ4_XS.gguf",
		"Qwen3-30B-A3B-Q2_K.gguf",
		"Qwen3-30B-A3B-Q2_K_L.gguf",
		"Qwen3-30B-A3B-Q3_K_M.gguf",
		"Qwen3-30B-A3B-Q3_K_S.gguf",
		"Qwen3-30B-A3B-Q4_0.gguf",
		"Qwen3-30B-A3B-Q4_1.gguf",
		"Qwen3-30B-A3B-Q4_K_M.gguf",
		"Qwen3-30B-A3B-Q4_K_S.gguf",
		"Qwen3-30B-A3B-Q5_K_M.gguf",
		"Qwen3-30B-A3B-Q5_K_S.gguf",
		"Qwen3-30B-A3B-Q6_K.gguf",
		"Qwen3-30B-A3B-Q8_0.gguf",
		"Qwen3-30B-A3B-UD-IQ1_M.gguf",
		"Qwen3-30B-A3B-UD-IQ1_S.gguf",
		"Qwen3-30B-A3B-UD-IQ2_M.gguf",
		"Qwen3-30B-A3B-UD-IQ2_XXS.gguf",
		"Qwen3-30B-A3B-UD-IQ3_XXS.gguf",
		"Qwen3-30B-A3B-UD-Q2_K_XL.gguf",
		"Qwen3-30B-A3B-UD-Q3_K_XL.gguf",
		"Qwen3-30B-A3B-UD-Q4_K_XL.gguf",
		"Qwen3-30B-A3B-UD-Q5_K_XL.gguf",
		"Qwen3-30B-A3B-UD-Q6_K_XL.gguf",
		"Qwen3-30B-A3B-UD-Q8_K_XL.gguf",
	}
	files := make([]FileInfo, 0, len(ggufNames))
	for i, n := range ggufNames {
		files = append(files, FileInfo{
			Name: n, Path: n, Size: int64(1_000_000_000 + i*100_000_000),
		})
	}

	info := analyzeGGUF(files)
	if info == nil {
		t.Fatal("analyzeGGUF returned nil")
	}
	if got, want := len(info.Quantizations), len(ggufNames); got != want {
		t.Errorf("got %d quantizations, want %d", got, want)
	}

	// Build a set of labels observed and assert no "Unknown" slipped through.
	labels := make(map[string]int)
	for _, q := range info.Quantizations {
		labels[q.Name]++
	}
	for _, q := range info.Quantizations {
		if q.Name == "Unknown" {
			t.Errorf("got Unknown label (regex did not match a .gguf file); quantizations=%v", labels)
			break
		}
	}

	// Explicitly assert the UD labels are present and distinct from their
	// non-UD counterparts. Before the fix the UD entries would be labeled
	// Q2_K..Q8_K and collide with the non-UD files.
	// Unsloth Dynamic quants keep their "UD-" prefix so they are told apart
	// from plain quants of the same type.
	wantPresent := []string{
		"UD-Q2_K_XL", "UD-Q3_K_XL", "UD-Q4_K_XL", "UD-Q5_K_XL", "UD-Q6_K_XL", "UD-Q8_K_XL",
		"UD-IQ1_S", "UD-IQ1_M",
	}
	for _, w := range wantPresent {
		if labels[w] == 0 {
			t.Errorf("expected quantization label %q in result; labels=%v", w, labels)
		}
	}

	// Non-UD labels should still appear exactly once (not duplicated by a UD
	// file masquerading as the same label).
	for _, plain := range []string{"Q2_K", "Q6_K"} {
		if labels[plain] != 1 {
			t.Errorf("expected exactly one %q entry, got %d; labels=%v", plain, labels[plain], labels)
		}
	}
}
