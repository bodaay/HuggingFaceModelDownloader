// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/csv"
	"path"
	"strings"
	"sync"
)

// parsePathPatterns preserves each regex verbatim. Literal mode uses the
// same CSV parsing as pflag's StringSlice, including quoted commas.
func parsePathPatterns(values []string, useRegex bool) ([]string, error) {
	if useRegex {
		return values, nil
	}
	var patterns []string
	for _, value := range values {
		if value == "" {
			continue
		}
		fields, err := csv.NewReader(strings.NewReader(value)).Read()
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, fields...)
	}
	return patterns, nil
}

type commandShell uint8

const (
	posixShell commandShell = iota
	cmdShell
	powerShell
	legacyPowerShell
)

var pathPatternShell = sync.OnceValue(detectCommandShell)

func shellForParent(executable string, fallback commandShell) commandShell {
	switch strings.ToLower(path.Base(strings.ReplaceAll(executable, `\`, "/"))) {
	case "pwsh", "pwsh.exe":
		return powerShell
	case "powershell.exe":
		return legacyPowerShell
	case "cmd.exe":
		return cmdShell
	case "sh", "sh.exe", "bash", "bash.exe", "zsh", "zsh.exe", "dash", "dash.exe", "ksh", "ksh.exe", "ash", "ash.exe", "fish", "fish.exe":
		return posixShell
	}
	return fallback
}

// pathPatternArgumentForShell encodes one parsed pattern for a runnable
// manifest command: CSV protects literal commas; shell-specific quoting
// protects regex syntax and literal punctuation.
func pathPatternArgumentForShell(pattern string, useRegex bool, shell commandShell) string {
	if !useRegex {
		if pattern == "" {
			pattern = `""` // An empty CSV field, not an empty list.
		} else {
			var out strings.Builder
			writer := csv.NewWriter(&out)
			_ = writer.Write([]string{pattern})
			writer.Flush()
			pattern = strings.TrimSuffix(out.String(), "\n")
		}
	}
	if pattern != "" && strings.IndexFunc(pattern, func(c rune) bool {
		return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || strings.ContainsRune("_-./", c))
	}) == -1 {
		return pattern
	}
	switch shell {
	case cmdShell:
		// Protect both cmd.exe's metacharacters and the native executable's
		// double-quote/backslash argument parsing.
		var out strings.Builder
		for _, c := range `"` + escapeWindowsQuotes(pattern, true) + `"` {
			if strings.ContainsRune("()[]%!^\"`<>&|;, *?\t", c) {
				out.WriteByte('^')
			}
			out.WriteRune(c)
		}
		return out.String()
	case legacyPowerShell:
		// Windows PowerShell joins native arguments using legacy parsing.
		// Preserve embedded quotes (including CSV quotes) and empty values.
		pattern = escapeWindowsQuotes(pattern, false)
		if pattern == "" {
			pattern = `""`
		}
		fallthrough
	case powerShell:
		return "'" + strings.ReplaceAll(pattern, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(pattern, "'", "'\"'\"'") + "'"
}

// escapeWindowsQuotes preserves backslashes before embedded double quotes.
// Double trailing backslashes only when the caller adds closing quotes.
func escapeWindowsQuotes(pattern string, closingQuote bool) string {
	var out strings.Builder
	slashes := 0
	for _, c := range pattern {
		if c == '\\' {
			slashes++
			continue
		}
		if c == '"' {
			out.WriteString(strings.Repeat("\\", 2*slashes+1))
		} else {
			out.WriteString(strings.Repeat("\\", slashes))
		}
		out.WriteRune(c)
		slashes = 0
	}
	if closingQuote {
		slashes *= 2
	}
	out.WriteString(strings.Repeat("\\", slashes))
	return out.String()
}
