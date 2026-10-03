// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A config larger than one network read (large VLM configs are ~90 KB) used
// to be truncated by a single Body.Read and then dropped as invalid JSON.
func TestFetchFile_ReadsWholeFile(t *testing.T) {
	big := map[string]interface{}{"quantization_config": map[string]interface{}{"quant_method": "awq"}}
	for i := 0; i < 5000; i++ {
		big["padding_"+strings.Repeat("x", 10)+string(rune('a'+i%26))+strings.Repeat("y", i%7)] = i
	}
	body, _ := json.Marshal(big)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < len(body); i += 1000 { // many small writes
			w.Write(body[i:min(i+1000, len(body))])
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()

	a := NewAnalyzer(AnalyzerOptions{Endpoint: srv.URL, HTTPClient: srv.Client()})
	got, err := a.fetchFile(context.Background(), "o/r", false, "main", "config.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(body) {
		t.Fatalf("read %d of %d bytes", len(got), len(body))
	}
}

// EXL3's quantization_config.json is tens of MB; its settings come first.
func TestDecodeJSONHead_Truncated(t *testing.T) {
	head := `{"quant_method": "exl3", "version": "1.4.6", "bits": 2.51, "head_bits": 6, "calibration": {"rows": 250}, "tensor_storage": {"model.layers.0.self_attn.q_proj": {"stored_tensors": {"a": [1,2,3], "b": [4,5`
	m, err := decodeJSONHead(strings.NewReader(head))
	if err != nil {
		t.Fatal(err)
	}
	if m["quant_method"] != "exl3" || m["bits"] != 2.51 || m["head_bits"] != 6.0 {
		t.Errorf("decoded %v", m)
	}
	if _, ok := m["tensor_storage"]; ok {
		t.Error("truncated member should not be returned")
	}
	q := analyzeQuantized(map[string]interface{}{"quantization_config.json": m})
	if q == nil || q.Method != "exl3" || q.BitsPerWeight != 2.51 || q.HeadBits != 6 {
		t.Errorf("analyzeQuantized from head = %+v", q)
	}
}
