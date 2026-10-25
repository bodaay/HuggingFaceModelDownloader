// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func TestDownloadCmd_PathSelection(t *testing.T) {
	paths := []string{
		"README.md", "BF16/tokenizer.json", "GGUF/config.json",
		"GGUF/model-Q4_K_M.gguf", "GGUF/model-Q8_0.gguf",
		"GGUF/model-001.gguf", "GGUF/model-0001.gguf", "GGUF/file,comma.gguf",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/revision/") {
			json.NewEncoder(w).Encode(map[string]string{"sha": "main"})
			return
		}
		var nodes []map[string]any
		for _, path := range paths {
			node := map[string]any{"type": "file", "path": path, "size": 10}
			if strings.HasSuffix(path, ".gguf") {
				node["lfs"] = map[string]any{"size": 10}
			}
			nodes = append(nodes, node)
		}
		json.NewEncoder(w).Encode(nodes)
	}))
	defer srv.Close()

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"comma lists and repeated includes use OR", []string{"-i", "Q4_K_M,README.md", "--include", "q8_0"},
			[]string{"GGUF/model-Q4_K_M.gguf", "GGUF/model-Q8_0.gguf", "README.md"}},
		{"legacy excludes retain CSV and repeated flag behavior", []string{"-i", ".gguf", "-E", `"file,comma",Q4_K_M`, "-E", "0001"},
			[]string{"GGUF/model-001.gguf", "GGUF/model-Q8_0.gguf"}},
		{"regex before patterns", []string{"--regex", "-i", `^GGUF/model-\d{1,3}\.gguf$`},
			[]string{"GGUF/model-001.gguf"}},
		{"regex after patterns", []string{"-i", `^GGUF/model-\d{1,3}\.gguf$`, "--regex"},
			[]string{"GGUF/model-001.gguf"}},
		{"regex excludes retain quantifier commas", []string{"-i", `^GGUF/.*\.gguf$`, "-E", `model-\d{1,3}\.gguf$`, "-E", "Q4_K_M", "--regex"},
			[]string{"GGUF/file,comma.gguf", "GGUF/model-0001.gguf", "GGUF/model-Q8_0.gguf"}},
		{"repeated regex includes use OR", []string{"-i", `^README\.md$`, "-i", `^GGUF/model-Q8_0\.gguf$`, "--regex"},
			[]string{"GGUF/model-Q8_0.gguf", "README.md"}},
		{"regex commas are literal outside quantifiers too", []string{"-i", `^GGUF/file,comma\.gguf$`, "--regex"},
			[]string{"GGUF/file,comma.gguf"}},
		{"explicit regex false restores CSV", []string{"--regex=false", "-i", "Q4_K_M,Q8_0"},
			[]string{"GGUF/model-Q4_K_M.gguf", "GGUF/model-Q8_0.gguf"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDownloadCmd(context.Background(), &RootOpts{})
			args := []string{"o/r", "--endpoint", srv.URL, "--dry-run", "--plan-format", "json"}
			cmd.SetArgs(append(args, tc.args...))
			var err error
			output := captureOutput(func() { err = cmd.Execute() })
			if err != nil {
				t.Fatal(err)
			}
			var plan hfdownloader.Plan
			if err := json.Unmarshal([]byte(output), &plan); err != nil {
				t.Fatalf("decode plan: %v; output: %s", err, output)
			}
			var got []string
			for _, it := range plan.Items {
				got = append(got, it.RelativePath)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("selected paths = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestDownloadCmd_InvalidPathPatterns(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer srv.Close()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-i", "[", "--regex"}, "invalid include regex"},
		{[]string{"-E", "[", "--regex"}, "invalid exclude regex"},
		{[]string{"-i", `"`}, "invalid --include"},
		{[]string{"-E", `"`}, "invalid --exclude"},
	} {
		cmd := newDownloadCmd(context.Background(), &RootOpts{})
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		args := []string{"o/r", "--endpoint", srv.URL, "--dry-run"}
		cmd.SetArgs(append(args, tc.args...))
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args %v: error = %v; want %s", tc.args, err, tc.want)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("made %d requests for invalid path patterns", n)
	}
}

func TestBuildCommandString_PathPatternsRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	for _, useRegex := range []bool{false, true} {
		patterns := []string{`^GGUF/\S{1,3}\.gguf$`, "file,comma", `a"b`, "don't", "$(literal)", ""}
		job := hfdownloader.Job{Repo: "o/r", Includes: patterns, Excludes: patterns, Regex: useRegex}
		cfg := hfdownloader.Settings{Concurrency: 8, MaxActiveDownloads: 3, Verify: "size"}
		command := buildCommandStringForShell(nil, job, cfg, posixShell)
		// Tokenize the actual manifest command with a shell, without running it.
		out, err := exec.Command(sh, "-c", "set -- "+command+"; printf '%s\\000' \"$@\"").Output()
		if err != nil {
			t.Fatalf("tokenize %s: %v", command, err)
		}
		args := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		cmd := newDownloadCmd(context.Background(), &RootOpts{})
		if err := cmd.ParseFlags(args[2:]); err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{"include", "exclude"} {
			raw, err := cmd.Flags().GetStringArray(flag)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parsePathPatterns(raw, useRegex)
			if err != nil || !reflect.DeepEqual(got, patterns) {
				t.Errorf("regex=%t, %s round trip = %q, %v; want %q", useRegex, flag, got, err, patterns)
			}
		}
		if got, _ := cmd.Flags().GetBool("regex"); got != useRegex {
			t.Errorf("regex round trip = %t; want %t", got, useRegex)
		}
	}
}

// The helper runs as a native executable so PowerShell's native argument
// passing is exercised, not just its parsing of strings in a script.
func TestPathPatternArgumentProbe(t *testing.T) {
	if os.Getenv("HFD_PATH_PATTERN_PROBE") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			if err := json.NewEncoder(os.Stdout).Encode(os.Args[i+1:]); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("missing probe argument separator")
}

func pathPatternRegressionCases() []string {
	return []string{
		`^GGUF/don't[.]gguf$`, `model[1]`, `^GGUF/\S{1,3}\.gguf$`,
		"file,comma", `a"b`, `a\"b`, "$(literal)", "`literal`", "", "001",
		"a b\\", "trailing\\", "a&b|c", "!literal!",
	}
}

func assertPathPatternCommandArgs(t *testing.T, args, patterns []string, useRegex bool) {
	t.Helper()
	if len(args) == 0 || args[0] != "download" {
		t.Fatalf("unexpected saved command arguments: %q", args)
	}
	cmd := newDownloadCmd(context.Background(), &RootOpts{})
	if err := cmd.ParseFlags(args[1:]); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"include", "exclude"} {
		raw, err := cmd.Flags().GetStringArray(flag)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parsePathPatterns(raw, useRegex)
		if err != nil || !reflect.DeepEqual(got, patterns) {
			t.Errorf("regex=%t, %s round trip = %q, %v; want %q", useRegex, flag, got, err, patterns)
		}
	}
}

func TestBuildCommandString_PowerShellRoundTrip(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell unavailable")
	}
	probe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	patterns := pathPatternRegressionCases()
	for _, mode := range []struct {
		name  string
		shell commandShell
	}{
		{"Standard", powerShell},
		{"Legacy", legacyPowerShell},
	} {
		for _, useRegex := range []bool{false, true} {
			t.Run(mode.name+"/regex="+strconv.FormatBool(useRegex), func(t *testing.T) {
				job := hfdownloader.Job{Repo: "o/r", Includes: patterns, Excludes: patterns, Regex: useRegex}
				cfg := hfdownloader.Settings{Concurrency: 8, MaxActiveDownloads: 3, Verify: "size"}
				command := buildCommandStringForShell(nil, job, cfg, mode.shell)
				script := "$PSNativeCommandArgumentPassing='" + mode.name + "'; & " +
					pathPatternArgumentForShell(probe, true, mode.shell) +
					" " + pathPatternArgumentForShell("-test.run=^TestPathPatternArgumentProbe$", true, mode.shell) +
					" -- " + strings.TrimPrefix(command, "hfdownloader ")
				child := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", script)
				child.Env = append(os.Environ(), "HFD_PATH_PATTERN_PROBE=1")
				out, err := child.CombinedOutput()
				if err != nil {
					t.Fatalf("replay PowerShell command: %v; output: %s", err, out)
				}
				var args []string
				if err := json.NewDecoder(strings.NewReader(string(out))).Decode(&args); err != nil {
					t.Fatalf("decode probe: %v; output: %s", err, out)
				}
				assertPathPatternCommandArgs(t, args, patterns, useRegex)
			})
		}
	}
}

func TestShellForParent(t *testing.T) {
	for _, tc := range []struct {
		parent   string
		fallback commandShell
		want     commandShell
	}{
		{`C:\Program Files\PowerShell\7\pwsh.exe`, cmdShell, powerShell},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, cmdShell, legacyPowerShell},
		{`C:\Windows\System32\CMD.EXE`, cmdShell, cmdShell},
		{`C:\Program Files\Git\usr\bin\bash.exe`, cmdShell, posixShell},
		{"/usr/bin/pwsh", posixShell, powerShell},
		{"/usr/bin/bash", posixShell, posixShell},
		{"", cmdShell, cmdShell},
	} {
		if got := shellForParent(tc.parent, tc.fallback); got != tc.want {
			t.Errorf("shellForParent(%q) = %v; want %v", tc.parent, got, tc.want)
		}
	}
}
