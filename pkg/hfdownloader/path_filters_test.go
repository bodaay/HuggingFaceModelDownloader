// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package hfdownloader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPlanRepo_PathSelection(t *testing.T) {
	all := items(
		"README.md|small", "BF16/tokenizer.json", "FP8/config.json|small",
		"GGUF/model-Q4_K_M.gguf", "GGUF/model-Q8_0.gguf", "GGUF/model-Q6_K.gguf",
		"GGUF/mmproj-Q8_0.gguf", "GGUF/tokenizer.json|small", "GGUF/model-001.gguf",
		"GGUF/other.ninfer", "GGUF-NInfer/model-Q8_0.ninfer", "other/model-Q8_0.gguf",
	)
	var nodes []hfNode
	for _, it := range all {
		n := hfNode{Type: "file", Path: it.RelativePath, Size: 10}
		if it.LFS {
			n.LFS = &hfLfsInfo{Size: 10}
		}
		nodes = append(nodes, n)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/o/r/revision/main":
			json.NewEncoder(w).Encode(map[string]string{"sha": "main"})
		case "/api/models/o/r/tree/main":
			json.NewEncoder(w).Encode(nodes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cases := []struct {
		name string
		job  Job
		want []string
	}{
		{"no path selection preserves everything", Job{}, selectedPaths(all)},
		{"literal includes are strict and case insensitive", Job{Includes: []string{"gguf/model-q8_0.gguf", "gguf/mmproj-q8_0.gguf"}},
			[]string{"GGUF/mmproj-Q8_0.gguf", "GGUF/model-Q8_0.gguf"}},
		{"folder includes every file type but not similarly named folders", Job{Includes: []string{"GGUF/"}},
			[]string{"GGUF/mmproj-Q8_0.gguf", "GGUF/model-001.gguf", "GGUF/model-Q4_K_M.gguf", "GGUF/model-Q6_K.gguf", "GGUF/model-Q8_0.gguf", "GGUF/other.ninfer", "GGUF/tokenizer.json"}},
		{"literal patterns are not regexes", Job{Includes: []string{`.gguf$`}}, nil},
		{"exclusion overrides inclusion regardless of exact mode", Job{Includes: []string{"GGUF/model-Q"}, Excludes: []string{"q4_k", "q6_k"}, ExactMatch: true},
			[]string{"GGUF/model-Q8_0.gguf"}},
		{"legacy exclude applies to both small and LFS files", Job{Excludes: []string{"tokenizer", ".GGUF", ".ninfer", "config"}},
			[]string{"README.md"}},
		{"regex selects directory and quants and excludes companion", Job{Regex: true, Includes: []string{`^gguf/.*(q4_k_m|q8_0).*\.gguf$`}, Excludes: []string{`/mmproj-`}},
			[]string{"GGUF/model-Q4_K_M.gguf", "GGUF/model-Q8_0.gguf"}},
		{"repeated regex includes combine with OR", Job{Regex: true, Includes: []string{`^README\.md$`, `^GGUF/model-Q8_0\.gguf$`}},
			[]string{"GGUF/model-Q8_0.gguf", "README.md"}},
		{"regex commas are preserved", Job{Regex: true, Includes: []string{`^GGUF/model-\d{1,3}\.gguf$`}},
			[]string{"GGUF/model-001.gguf"}},
		{"regex escapes are not lowercased", Job{Regex: true, Includes: []string{`^GGUF/\S+-Q8_0\.gguf$`}},
			[]string{"GGUF/mmproj-Q8_0.gguf", "GGUF/model-Q8_0.gguf"}},
		{"regex can explicitly opt into case sensitivity", Job{Regex: true, Includes: []string{`(?-i)^gguf/`}}, nil},
		{"regex exclusion alone applies to every file", Job{Regex: true, Excludes: []string{`(?i)^(GGUF|BF16|FP8|other)/`, `\.ninfer$`}},
			[]string{"README.md"}},
		{"path includes intersect legacy weight selection", Job{Includes: []string{"GGUF/"}, Filters: []string{"q4_k_m"}, ExactMatch: true},
			[]string{"GGUF/model-Q4_K_M.gguf", "GGUF/other.ninfer", "GGUF/tokenizer.json"}},
		{"regex mode does not reinterpret weight filters", Job{Regex: true, Includes: []string{`^GGUF/.*\.gguf$`}, Filters: []string{"q4_k_m|q8_0"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := tc.job
			job.Repo = "o/r"
			plan, err := PlanRepo(context.Background(), job, Settings{Endpoint: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			if got := selectedPaths(plan.Items); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("selected paths = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestPathSelection_InvalidRegexBeforeRequests(t *testing.T) {
	for _, field := range []string{"include", "exclude"} {
		for _, download := range []bool{false, true} {
			name := field + "/plan"
			if download {
				name = field + "/download"
			}
			t.Run(name, func(t *testing.T) {
				var requests atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
				}))
				defer srv.Close()
				job := Job{Repo: "o/r", Regex: true}
				if field == "include" {
					job.Includes = []string{"["}
				} else {
					job.Excludes = []string{"["}
				}
				cfg := Settings{Endpoint: srv.URL, CacheDir: t.TempDir()}
				var err error
				if download {
					err = Download(context.Background(), job, cfg, nil)
				} else {
					_, err = PlanRepo(context.Background(), job, cfg)
				}
				if err == nil || !strings.Contains(err.Error(), "invalid "+field+" regex") {
					t.Fatalf("error = %v; want invalid %s regex", err, field)
				}
				if got := requests.Load(); got != 0 {
					t.Errorf("made %d requests before rejecting regex", got)
				}
			})
		}
	}
}
