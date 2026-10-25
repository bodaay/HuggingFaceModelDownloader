// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"fmt"
	"regexp"
	"strings"
)

// compilePathMatcher builds an OR matcher for repo-relative paths. Compile
// once per scan, before any network requests, rather than once per file.
func compilePathMatcher(patterns []string, useRegex bool) (func(string) bool, error) {
	if useRegex {
		compiled := make([]*regexp.Regexp, 0, len(patterns))
		for _, pattern := range patterns {
			// Do not lowercase regexes: that would change escapes such as \S.
			re, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				return nil, fmt.Errorf("%q: %w", pattern, err)
			}
			compiled = append(compiled, re)
		}
		return func(rel string) bool {
			for _, re := range compiled {
				if re.MatchString(rel) {
					return true
				}
			}
			return false
		}, nil
	}

	lower := make([]string, len(patterns))
	for i, pattern := range patterns {
		lower[i] = strings.ToLower(pattern)
	}
	return func(rel string) bool {
		rel = strings.ToLower(rel)
		for _, pattern := range lower {
			if strings.Contains(rel, pattern) {
				return true
			}
		}
		return false
	}, nil
}
