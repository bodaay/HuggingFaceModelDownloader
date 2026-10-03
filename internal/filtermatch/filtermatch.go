// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

// Package filtermatch decides whether a download filter selects a repo
// file. It is shared by the downloader (which applies filters) and the
// analyzer (which generates filters and must know exactly what they select).
package filtermatch

import (
	"path"
	"regexp"
	"strings"
)

// shardSuffix matches a split-file suffix such as "-00001-of-00003".
var shardSuffix = regexp.MustCompile(`-\d{3,}-of-\d{3,}$`)

// StripShard removes the extension and any split-file suffix from a file
// name: "Model-Q4_K_M-00001-of-00003.gguf" → "Model-Q4_K_M".
func StripShard(name string) string {
	name = strings.TrimSuffix(name, path.Ext(name))
	return shardSuffix.ReplaceAllString(name, "")
}

// Match reports whether filter fLower matches the repo path relLower (both
// already lowercased). In substring mode (exact=false) it is a plain
// substring check on the full path. In exact mode it matches when fLower:
//   - starts with "." and is a suffix of the path (an extension filter);
//   - contains "/" and is a run of whole path elements ("q4_k_m/");
//   - equals the whole path or file name, with or without its extension or
//     split-file suffix (all shards of "model-q4_k_m-0000N-of-0000M.gguf"
//     match "model-q4_k_m");
//   - equals one delimiter-bounded segment of the path.
//
// So "q6_k" matches "...-Q6_K.gguf" and the folder "Q6_K/" but not
// "...-Q6_K_XL.gguf" (github issue #78).
func Match(relLower, fLower string, exact bool) bool {
	if !exact {
		return strings.Contains(relLower, fLower)
	}
	if strings.HasPrefix(fLower, ".") && !strings.Contains(fLower, "/") {
		return strings.HasSuffix(relLower, fLower)
	}
	if strings.Contains(fLower, "/") {
		p := strings.Trim(fLower, "/")
		return relLower == p || strings.HasPrefix(relLower, p+"/") || strings.Contains(relLower, "/"+p+"/")
	}
	nameLower := path.Base(relLower)
	if fLower == relLower || fLower == nameLower || fLower == StripShard(nameLower) ||
		fLower == strings.TrimSuffix(relLower, path.Ext(relLower)) {
		return true
	}
	for _, seg := range strings.FieldsFunc(relLower, isDelimiter) {
		if seg == fLower {
			return true
		}
	}
	return false
}

// isDelimiter reports whether r separates segments for exact matching.
// Underscores are intentionally NOT delimiters because quantization names
// contain them (e.g. Q6_K, Q4_K_M).
func isDelimiter(r rune) bool {
	return r == '/' || r == '-' || r == '.' || r == ' '
}
