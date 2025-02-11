package hfclient

import (
	"path/filepath"
	"regexp"
	"strings"
)

// FilterFiles filters the list of files based on patterns and returns matched files
func FilterFiles(files []*File, patterns []string) []*File {
	if len(patterns) == 0 {
		return files // Return all files if no patterns specified
	}

	// Compile all patterns
	var matchers []fileMatcher
	for _, pattern := range patterns {
		matcher := createMatcher(pattern)
		if matcher != nil {
			matchers = append(matchers, matcher)
		}
	}

	// Filter files
	var matched []*File
	for _, file := range files {
		for _, matcher := range matchers {
			if matcher.matches(file.Path) {
				file.Pattern = matcher.pattern() // Store the pattern that matched
				matched = append(matched, file)
				break // File can only match once
			}
		}
	}

	return matched
}

// fileMatcher interface allows for different types of matching
type fileMatcher interface {
	matches(path string) bool
	pattern() string
}

// globMatcher uses filepath.Match for glob patterns
type globMatcher struct {
	glob        string
	patternText string // renamed from pattern
}

func (g *globMatcher) matches(path string) bool {
	match, _ := filepath.Match(g.glob, filepath.Base(path))
	if !match {
		// Try matching against full path if base name didn't match
		match, _ = filepath.Match(g.glob, path)
	}
	return match
}

func (g *globMatcher) pattern() string {
	return g.patternText
}

// regexMatcher uses regexp for regex patterns
type regexMatcher struct {
	regex       *regexp.Regexp
	patternText string // renamed from pattern
}

func (r *regexMatcher) matches(path string) bool {
	return r.regex.MatchString(path)
}

func (r *regexMatcher) pattern() string {
	return r.patternText
}

// createMatcher creates appropriate matcher based on pattern type
func createMatcher(pattern string) fileMatcher {
	// Remove any leading/trailing whitespace
	pattern = strings.TrimSpace(pattern)

	// Check if it's a regex pattern (enclosed in / /)
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
		// Extract regex between slashes
		regexStr := pattern[1 : len(pattern)-1]
		regex, err := regexp.Compile(regexStr)
		if err == nil {
			return &regexMatcher{regex: regex, patternText: pattern}
		}
	}

	// Treat as glob pattern
	// Convert common regex-like patterns to glob patterns
	pattern = strings.ReplaceAll(pattern, ".*", "*")
	pattern = strings.ReplaceAll(pattern, ".+", "*")

	return &globMatcher{glob: pattern, patternText: pattern}
}
