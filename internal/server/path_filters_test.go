// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import "testing"

// Downloads made with strict includes must not appear as complete repos in
// the cache browser, which derives selection status from the saved command.
func TestParseCommandFilters_PathIncludes(t *testing.T) {
	for _, command := range []string{
		`hfdownloader download o/r -i 'GGUF/model.gguf'`,
		`hfdownloader download o/r -i=GGUF/model.gguf`,
		`hfdownloader download o/r --include GGUF/model.gguf`,
		`hfdownloader download o/r --include=GGUF/model.gguf`,
	} {
		filtered, pattern := parseCommandFilters(command)
		if !filtered || pattern != "GGUF/model.gguf" {
			t.Errorf("%s: filtered=%t, pattern=%q", command, filtered, pattern)
		}
	}
	command := `hfdownloader download o/r --regex -i '^GGUF/.*\.gguf$'`
	if filtered, pattern := parseCommandFilters(command); !filtered || pattern != `^GGUF/.*\.gguf$` {
		t.Errorf("regex include: filtered=%t, pattern=%q", filtered, pattern)
	}
}
