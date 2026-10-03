// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"strings"
	"testing"
)

// Branch names of turboderp/Qwen3.8-27B-exl3 (github issue #94).
var turboderpRefs = func() []RepoRef {
	var refs []RepoRef
	for _, n := range []string{"main", "SC_6.00bpw_H6_V6", "SC_4.00bpw_H5_V6", "SC_2.00bpw_H3", "6.00bpw", "5.00bpw",
		"3.50bpw", "3.00bpw", "4.00bpw", "2.50bpw", "2.00bpw"} {
		refs = append(refs, RepoRef{Name: n, Type: "branch"})
	}
	return append(refs, RepoRef{Name: "v1.0", Type: "tag"})
}()

func TestQuantBranchesFromRefs(t *testing.T) {
	branches := quantBranchesFromRefs(turboderpRefs, "turboderp/Qwen3.8-27B-exl3")
	if len(branches) != 10 {
		t.Fatalf("got %d branches, want 10 (main and tags excluded)", len(branches))
	}
	if branches[0].BitsPerWeight != 2.0 || branches[len(branches)-1].BitsPerWeight != 6.0 {
		t.Errorf("not sorted by bpw: first %v last %v", branches[0], branches[len(branches)-1])
	}
	for _, b := range branches {
		if b.Name == "SC_6.00bpw_H6_V6" && (b.BitsPerWeight != 6.0 || b.HeadBits != 6) {
			t.Errorf("SC_6.00bpw_H6_V6 parsed as %+v", b)
		}
	}
	if got := recommendedBranch(branches); got != "4.00bpw" {
		t.Errorf("recommended %q, want 4.00bpw (closest to 4 bpw, plain name)", got)
	}
	if got := quantBranchesFromRefs([]RepoRef{{Name: "main", Type: "branch"}, {Name: "fp16", Type: "branch"}}, "o/r"); len(got) != 0 {
		t.Errorf("non-bpw branches detected: %v", got)
	}
}

func TestQuantBranchItemsAndCommand(t *testing.T) {
	info := &RepoInfo{Repo: "turboderp/Qwen3.8-27B-exl3"}
	info.SelectableItems = QuantBranchItems(quantBranchesFromRefs(turboderpRefs, "turboderp/Qwen3.8-27B-exl3"), "exl3")
	info.PopulateCLICommands()

	var rec []string
	for _, it := range info.SelectableItems {
		if it.Revision == "" || it.Category != "branch" {
			t.Errorf("branch item without revision/category: %+v", it)
		}
		if it.Recommended {
			rec = append(rec, it.Revision)
		}
	}
	if len(rec) != 1 || rec[0] != "4.00bpw" {
		t.Errorf("recommended %v", rec)
	}
	if want := "hfdownloader download turboderp/Qwen3.8-27B-exl3 -b 4.00bpw"; info.CLICommandFull != want {
		t.Errorf("recommended command %q, want %q", info.CLICommandFull, want)
	}
	if !strings.Contains(info.SelectableItems[0].Description, "EXL3 2 bpw") {
		t.Errorf("description %q", info.SelectableItems[0].Description)
	}
}

// bartowski names EXL2 branches "3_5", "4_25", "8_0"; trusted only for repos
// whose name says exl2/exl3.
func TestQuantBranches_UnderscoreNames(t *testing.T) {
	var refs []RepoRef
	for _, n := range []string{"main", "3_5", "4_25", "5_0", "6_5", "8_0"} {
		refs = append(refs, RepoRef{Name: n, Type: "branch"})
	}
	b := quantBranchesFromRefs(refs, "bartowski/magnum-12b-v2.5-kto-exl2")
	if len(b) != 5 || b[1].Name != "4_25" || b[1].BitsPerWeight != 4.25 {
		t.Errorf("got %+v", b)
	}
	if got := recommendedBranch(b); got != "4_25" {
		t.Errorf("recommended %q", got)
	}
	if got := quantBranchesFromRefs(refs, "someone/plain-model"); len(got) != 0 {
		t.Errorf("underscore branches of a non-exl repo detected: %v", got)
	}
}
