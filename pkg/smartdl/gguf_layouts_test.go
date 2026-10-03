// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/internal/filtermatch"
)

func ggufFiles(paths ...string) []FileInfo {
	var out []FileInfo
	for i, p := range paths {
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		out = append(out, FileInfo{Path: p, Name: path.Base(p), Directory: dir, Size: int64(1_000_000 * (i + 1)), IsLFS: true})
	}
	return out
}

// selects returns the paths an --exact filter picks among files.
func selects(filter string, files []FileInfo) []string {
	var out []string
	for _, f := range files {
		if filtermatch.Match(strings.ToLower(f.Path), filter, true) {
			out = append(out, f.Path)
		}
	}
	sort.Strings(out)
	return out
}

// checkItemsSelectExactly asserts every selectable item's filter picks
// exactly that item's files — the property that makes the analyzer's
// generated commands download what they show.
func checkItemsSelectExactly(t *testing.T, items []SelectableItem, files []FileInfo) {
	t.Helper()
	for _, it := range items {
		want := append([]string(nil), it.Files...)
		sort.Strings(want)
		got := selects(it.FilterValue, files)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("item %q filter %q selects %v, want %v", it.Label, it.FilterValue, got, want)
		}
	}
}

func byLabel(items []SelectableItem) map[string]SelectableItem {
	m := map[string]SelectableItem{}
	for _, it := range items {
		m[it.Label] = it
	}
	return m
}

// unsloth/bartowski-style: big quants split into shards inside per-quant
// folders, small quants at the root, plus imatrix data.
func TestGGUF_SplitShardsInFolders(t *testing.T) {
	files := ggufFiles(
		"Q4_K_M/Model-480B-Q4_K_M-00001-of-00003.gguf",
		"Q4_K_M/Model-480B-Q4_K_M-00002-of-00003.gguf",
		"Q4_K_M/Model-480B-Q4_K_M-00003-of-00003.gguf",
		"UD-Q2_K_XL/Model-480B-UD-Q2_K_XL-00001-of-00002.gguf",
		"UD-Q2_K_XL/Model-480B-UD-Q2_K_XL-00002-of-00002.gguf",
		"Model-480B-Q2_K.gguf",
		"imatrix_unsloth.gguf",
	)
	info := analyzeGGUF(files)
	if got := len(info.Quantizations); got != 3 {
		t.Fatalf("got %d quantizations, want 3 (one per quant, shards merged)", got)
	}
	items := GGUFToSelectableItems(info)
	m := byLabel(items)
	q4 := m["Q4_K_M"]
	if len(q4.Files) != 3 || q4.Size != 1_000_000+2_000_000+3_000_000 {
		t.Errorf("Q4_K_M: %d files, size %d; want 3 shards with combined size", len(q4.Files), q4.Size)
	}
	if !q4.Recommended {
		t.Error("Q4_K_M should be recommended")
	}
	if _, ok := m["UD-Q2_K_XL"]; !ok {
		t.Errorf("UD- prefix lost; labels: %v", keys(m))
	}
	for label := range m {
		if strings.Contains(strings.ToLower(label), "imatrix") {
			t.Error("imatrix calibration file offered as a quant")
		}
	}
	checkItemsSelectExactly(t, items, files)
}

// unsloth/gemma-4-12B-it-qat-GGUF (github issue #86): MTP drafts in an MTP/
// folder and at the root were shown as quants Q8_0/BF16/F16/Q4_0 and
// "Unknown", and gave the model its name.
func TestGGUF_MTPDraftsAreCompanions(t *testing.T) {
	files := ggufFiles(
		"MTP/mtp-gemma-4-12B-it-BF16.gguf",
		"MTP/mtp-gemma-4-12B-it-F16.gguf",
		"MTP/mtp-gemma-4-12B-it-Q4_0.gguf",
		"MTP/mtp-gemma-4-12B-it-Q8_0.gguf",
		"gemma-4-12B-it-qat-UD-Q4_K_XL.gguf",
		"mmproj-BF16.gguf",
		"mmproj-F16.gguf",
		"mtp-gemma-4-12B-it.gguf",
	)
	info := analyzeGGUF(files)
	if len(info.Quantizations) != 1 || info.Quantizations[0].Name != "UD-Q4_K_XL" {
		t.Fatalf("quantizations = %v; want only UD-Q4_K_XL", quantNames(info))
	}
	if len(info.MTPFiles) != 5 {
		t.Errorf("MTP drafts = %d, want 5", len(info.MTPFiles))
	}
	if strings.Contains(strings.ToLower(info.ModelName), "mtp") {
		t.Errorf("model name taken from an MTP draft: %q", info.ModelName)
	}
	items := GGUFToSelectableItems(info)
	var mtp, recMTP int
	for _, it := range items {
		if it.Category == "mtp_draft" {
			mtp++
			if it.Recommended {
				recMTP++
			}
		}
	}
	if mtp != 5 || recMTP != 0 {
		t.Errorf("mtp items = %d (recommended %d); want 5 optional items", mtp, recMTP)
	}
	checkItemsSelectExactly(t, items, files)
}

// nerkyor/Qwen3.6-27B-...-GGUF (github issue #89): quants named by folder
// with non-standard file names, split main models with MTP built in, and
// MTP drafts sharing the quant folder — all used to be "Unknown" / -F unknown.
func TestGGUF_FolderNamedCustomQuants(t *testing.T) {
	files := ggufFiles(
		"Q3_LynnStyle/Q3-imatrix-MTP-draft.gguf",
		"Q3_LynnStyle/Qwen3.6-27B-Coding-Q3-LynnStyle.gguf",
		"Q4_LynnStyle/Q4-imatrix-MTP-draft-q4.gguf",
		"Q4_LynnStyle/Q4-imatrix-MTP-draft.gguf",
		"Q4_LynnStyle/Qwen3.6-27B-Coding-Q4-LynnStyle.gguf",
		"Q8_0/Q8-MTP-00001-of-00003.gguf",
		"Q8_0/Q8-MTP-00002-of-00003.gguf",
		"Q8_0/Q8-MTP-00003-of-00003.gguf",
		"Q8_0/Q8-MTP-draft.gguf",
		"mmproj-Qwen3.5-27B-Q8_0.gguf",
	)
	info := analyzeGGUF(files)
	names := quantNames(info)
	sort.Strings(names)
	if strings.Join(names, ",") != "Q3_LynnStyle,Q4_LynnStyle,Q8_0" {
		t.Errorf("quantizations = %v; want Q3_LynnStyle, Q4_LynnStyle, Q8_0", names)
	}
	for _, q := range info.Quantizations {
		if q.Name == "Q8_0" && len(q.Files) != 3 {
			t.Errorf("Q8_0 has %d files, want the 3 shards (not the draft)", len(q.Files))
		}
		if q.Quality == 0 {
			t.Errorf("%s has no quality rating", q.Name)
		}
	}
	if len(info.MTPFiles) != 4 {
		t.Errorf("MTP drafts = %d, want 4", len(info.MTPFiles))
	}
	checkItemsSelectExactly(t, GGUFToSelectableItems(info), files)
}

func TestGGUF_NewQuantTypes(t *testing.T) {
	for name, want := range map[string]string{
		"Model-UD-TQ1_0.gguf":   "UD-TQ1_0",
		"Model-Q4_0_4_4.gguf":   "Q4_0_4_4",
		"Model-Q4_0.gguf":       "Q4_0",
		"gpt-oss-MXFP4.gguf":    "MXFP4",
		"Model-IQ4_XS.gguf":     "IQ4_XS",
		"model.q4_k_m.gguf":     "Q4_K_M",
		"Model-UD-IQ2_XXS.gguf": "UD-IQ2_XXS",
	} {
		if got := parseGGUFQuantization(FileInfo{Name: name, Path: name}).Name; got != want {
			t.Errorf("%s → %q, want %q", name, got, want)
		}
	}
}

// Only one quant is recommended when the repo has no Q4_K_M; previously every
// quant rated 4+ and under 10 GiB was, so "Recommended" downloaded them all.
func TestGGUF_RecommendsOneQuant(t *testing.T) {
	files := ggufFiles("SmolVLM-Q8_0.gguf", "SmolVLM-F16.gguf", "SmolVLM-BF16.gguf", "SmolVLM-Q5_K_M.gguf")
	n := 0
	for _, it := range GGUFToSelectableItems(analyzeGGUF(files)) {
		if it.Category == "quantization" && it.Recommended {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d quants recommended, want 1", n)
	}
}

func quantNames(info *GGUFInfo) []string {
	var out []string
	for _, q := range info.Quantizations {
		out = append(out, q.Name)
	}
	return out
}

func keys(m map[string]SelectableItem) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// When no quant fits the usual rule (repo of large custom quants), one quant
// is still recommended so the recommended command isn't just the mmproj.
func TestGGUF_RecommendFallback(t *testing.T) {
	big := int64(19 << 30)
	files := []FileInfo{
		{Path: "Q8_0/m-Q8_0.gguf", Name: "m-Q8_0.gguf", Directory: "Q8_0", Size: big + (7 << 30), IsLFS: true},
		{Path: "Q5_Custom/m-Q5-Custom.gguf", Name: "m-Q5-Custom.gguf", Directory: "Q5_Custom", Size: big, IsLFS: true},
		{Path: "Q3_Custom/m-Q3-Custom.gguf", Name: "m-Q3-Custom.gguf", Directory: "Q3_Custom", Size: big / 2, IsLFS: true},
		{Path: "mmproj-F16.gguf", Name: "mmproj-F16.gguf", Size: 1 << 29, IsLFS: true},
	}
	var rec []string
	for _, it := range GGUFToSelectableItems(analyzeGGUF(files)) {
		if it.Category == "quantization" && it.Recommended {
			rec = append(rec, it.Label)
		}
	}
	if len(rec) != 1 || rec[0] != "Q5_Custom" {
		t.Errorf("recommended %v, want [Q5_Custom] (smallest 4-star quant)", rec)
	}
}
