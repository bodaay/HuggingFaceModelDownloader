// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func TestBuildCommandString_CmdRoundTrip(t *testing.T) {
	probe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HFD_PATTERN_VALUE", "must-not-expand")
	patterns := append(pathPatternRegressionCases(), "%HFD_PATTERN_VALUE%")
	for _, useRegex := range []bool{false, true} {
		t.Run("regex="+strconv.FormatBool(useRegex), func(t *testing.T) {
			job := hfdownloader.Job{Repo: "o/r", Includes: patterns, Excludes: patterns, Regex: useRegex}
			cfg := hfdownloader.Settings{Concurrency: 8, MaxActiveDownloads: 3, Verify: "size"}
			command := buildCommandStringForShell(nil, job, cfg, cmdShell)
			script := pathPatternArgumentForShell(probe, true, cmdShell) +
				" " + pathPatternArgumentForShell("-test.run=^TestPathPatternArgumentProbe$", true, cmdShell) +
				" -- " + strings.TrimPrefix(command, "hfdownloader ")
			child := exec.Command("cmd.exe")
			// cmd.exe /s /c needs its own outer command-string quoting rather
			// than os/exec's default native-argument escaping.
			child.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /v:off /s /c "` + script + `"`}
			child.Env = append(os.Environ(), "HFD_PATH_PATTERN_PROBE=1")
			out, err := child.CombinedOutput()
			if err != nil {
				t.Fatalf("replay cmd.exe command: %v; output: %s", err, out)
			}
			var args []string
			if err := json.NewDecoder(strings.NewReader(string(out))).Decode(&args); err != nil {
				t.Fatalf("decode probe: %v; output: %s", err, out)
			}
			assertPathPatternCommandArgs(t, args, patterns, useRegex)
		})
	}
}
