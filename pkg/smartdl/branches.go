// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// QuantBranch is a quantization stored on its own branch, as EXL2/EXL3
// uploaders (e.g. turboderp) do: main holds only measurement files and each
// bitrate lives on a branch such as "4.00bpw" or "SC_6.00bpw_H6_V6".
type QuantBranch struct {
	Name          string  `json:"name"`
	BitsPerWeight float64 `json:"bits_per_weight"`
	HeadBits      int     `json:"head_bits,omitempty"`
	Size          int64   `json:"size,omitempty"`
	SizeHuman     string  `json:"size_human,omitempty"`
	Files         int     `json:"files,omitempty"`
}

// bpwBranch matches bitrate branch names: "4.00bpw", "3.0bpw_H6",
// "SC_6.00bpw_H6_V6".
var bpwBranch = regexp.MustCompile(`(?i)(?:^|[_-])(\d+(?:\.\d+)?)bpw(?:[_-]h(\d+))?`)

// underscoreBranch matches bartowski-style EXL2 branch names ("4_25" = 4.25
// bpw, "8_0"); only trusted for repos whose name says exl2/exl3.
var underscoreBranch = regexp.MustCompile(`^(\d+)_(\d+)$`)

// maxSizedBranches bounds how many branches get a tree listing for sizes.
const maxSizedBranches = 40

// quantBranchesFromRefs returns the bitrate branches among refs, sorted by
// bits per weight.
func quantBranchesFromRefs(refs []RepoRef, repo string) []QuantBranch {
	exl := strings.Contains(strings.ToLower(repo), "exl")
	var out []QuantBranch
	for _, r := range refs {
		if r.Type != "branch" || r.Name == "main" {
			continue
		}
		var b QuantBranch
		if m := bpwBranch.FindStringSubmatch(r.Name); m != nil {
			bits, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				continue
			}
			b = QuantBranch{Name: r.Name, BitsPerWeight: bits}
			if m[2] != "" {
				b.HeadBits, _ = strconv.Atoi(m[2])
			}
		} else if m := underscoreBranch.FindStringSubmatch(r.Name); m != nil && exl {
			bits, err := strconv.ParseFloat(m[1]+"."+m[2], 64)
			if err != nil {
				continue
			}
			b = QuantBranch{Name: r.Name, BitsPerWeight: bits}
		} else {
			continue
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].BitsPerWeight != out[j].BitsPerWeight {
			return out[i].BitsPerWeight < out[j].BitsPerWeight
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// recommendedBranch picks the branch closest to 4 bpw (a common quality/size
// balance), preferring plain names ("4.00bpw" over "SC_4.00bpw_H5_V6").
func recommendedBranch(branches []QuantBranch) string {
	best, bestScore := "", math.MaxFloat64
	for _, b := range branches {
		score := math.Abs(b.BitsPerWeight-4.0)*100 + float64(len(b.Name))/100
		if score < bestScore {
			best, bestScore = b.Name, score
		}
	}
	return best
}

// branchQuantMethod guesses the method from the repo name, else asks the
// first branch's quantization config.
func (a *Analyzer) branchQuantMethod(ctx context.Context, repo string, branches []QuantBranch) string {
	lower := strings.ToLower(repo)
	switch {
	case strings.Contains(lower, "exl3"):
		return "exl3"
	case strings.Contains(lower, "exl2"):
		return "exl2"
	}
	if len(branches) == 0 {
		return ""
	}
	for _, name := range []string{"quantization_config.json", "config.json"} {
		head, err := a.fetchJSONHead(ctx, repo, false, branches[0].Name, name)
		if err != nil {
			continue
		}
		if q := analyzeQuantized(map[string]interface{}{name: head}); q != nil && q.Method != "" {
			return q.Method
		}
	}
	return ""
}

// fillBranchSizes lists each branch's tree (a few at a time) to report its
// download size.
func (a *Analyzer) fillBranchSizes(ctx context.Context, repo string, branches []QuantBranch) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range branches {
		if i >= maxSizedBranches {
			break
		}
		wg.Add(1)
		go func(b *QuantBranch) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			files, _, err := a.fetchFileTree(ctx, repo, false, b.Name)
			if err != nil {
				return
			}
			for _, f := range files {
				b.Size += f.Size
			}
			b.Files = len(files)
			b.SizeHuman = humanSize(b.Size)
		}(&branches[i])
	}
	wg.Wait()
}

// QuantBranchItems converts bitrate branches to SelectableItems. Choosing one
// downloads that branch (Revision) rather than filtering files.
func QuantBranchItems(branches []QuantBranch, method string) []SelectableItem {
	rec := recommendedBranch(branches)
	label := strings.ToUpper(method)
	if label == "" {
		label = "Quantized"
	}
	var items []SelectableItem
	for _, b := range branches {
		desc := label + " " + strconv.FormatFloat(b.BitsPerWeight, 'f', -1, 64) + " bpw"
		if b.HeadBits > 0 {
			desc += ", " + strconv.Itoa(b.HeadBits) + "-bit head"
		}
		items = append(items, SelectableItem{
			ID:          "branch:" + b.Name,
			Label:       b.Name,
			Description: desc,
			Size:        b.Size,
			SizeHuman:   b.SizeHuman,
			Recommended: b.Name == rec,
			Category:    "branch",
			Revision:    b.Name,
		})
	}
	return items
}
