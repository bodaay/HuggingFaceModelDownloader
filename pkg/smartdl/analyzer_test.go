// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package smartdl

import (
	"net/http"
	"testing"
	"time"
)

func TestNewAnalyzer(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		analyzer := NewAnalyzer(AnalyzerOptions{})
		if analyzer.endpoint != defaultEndpoint {
			t.Errorf("endpoint = %q, want %q", analyzer.endpoint, defaultEndpoint)
		}
		if analyzer.client == nil {
			t.Error("client should not be nil")
		}
	})

	t.Run("custom endpoint", func(t *testing.T) {
		analyzer := NewAnalyzer(AnalyzerOptions{Endpoint: "https://hf-mirror.com"})
		if analyzer.endpoint != "https://hf-mirror.com" {
			t.Errorf("endpoint = %q", analyzer.endpoint)
		}
	})

	t.Run("endpoint trailing slash removed", func(t *testing.T) {
		analyzer := NewAnalyzer(AnalyzerOptions{Endpoint: "https://huggingface.co/"})
		if analyzer.endpoint != "https://huggingface.co" {
			t.Errorf("endpoint = %q, trailing slash not removed", analyzer.endpoint)
		}
	})

	t.Run("with token", func(t *testing.T) {
		analyzer := NewAnalyzer(AnalyzerOptions{Token: "hf_test_token"})
		if analyzer.token != "hf_test_token" {
			t.Errorf("token = %q", analyzer.token)
		}
	})

	t.Run("custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 60 * time.Second}
		analyzer := NewAnalyzer(AnalyzerOptions{HTTPClient: customClient})
		if analyzer.client != customClient {
			t.Error("custom client not used")
		}
	})
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TiB"},
		{4 * 1024 * 1024 * 1024, "4.0 GiB"},
		{int64(4.5 * 1024 * 1024 * 1024), "4.5 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := humanSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestPathEscapeAll(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"path/to/file", "path/to/file"},
		{"path with spaces", "path%20with%20spaces"},
		{"特殊字符", "%E7%89%B9%E6%AE%8A%E5%AD%97%E7%AC%A6"},
		{"file[1].txt", "file%5B1%5D.txt"},
		{"path/file name.txt", "path/file%20name.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := pathEscapeAll(tt.input)
			if result != tt.expected {
				t.Errorf("pathEscapeAll(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAnalyzer_DetectType(t *testing.T) {
	analyzer := NewAnalyzer(AnalyzerOptions{})

	tests := []struct {
		name      string
		files     []FileInfo
		isDataset bool
		expected  RepoType
	}{
		{
			name:      "dataset",
			files:     []FileInfo{{Name: "data.parquet"}},
			isDataset: true,
			expected:  TypeDataset,
		},
		{
			name: "GGUF model",
			files: []FileInfo{
				{Name: "model.Q4_K_M.gguf", Path: "model.Q4_K_M.gguf"},
				{Name: "config.json", Path: "config.json"},
			},
			expected: TypeGGUF,
		},
		{
			name: "Diffusers model",
			files: []FileInfo{
				{Name: "model_index.json", Path: "model_index.json"},
				{Name: "unet/diffusion_pytorch_model.safetensors", Path: "unet/diffusion_pytorch_model.safetensors"},
			},
			expected: TypeDiffusers,
		},
		{
			name: "LoRA adapter",
			files: []FileInfo{
				{Name: "adapter_config.json", Path: "adapter_config.json"},
				{Name: "adapter_model.safetensors", Path: "adapter_model.safetensors"},
			},
			expected: TypeLoRA,
		},
		{
			name: "GPTQ model",
			files: []FileInfo{
				{Name: "quantize_config.json", Path: "quantize_config.json"},
				{Name: "model.safetensors", Path: "model.safetensors"},
			},
			expected: TypeGPTQ,
		},
		{
			name: "ONNX model",
			files: []FileInfo{
				{Name: "model.onnx", Path: "model.onnx"},
				{Name: "config.json", Path: "config.json"},
			},
			expected: TypeONNX,
		},
		{
			name: "Transformers safetensors",
			files: []FileInfo{
				{Name: "config.json", Path: "config.json"},
				{Name: "model.safetensors", Path: "model.safetensors"},
			},
			expected: TypeTransformers,
		},
		{
			name: "Transformers bin",
			files: []FileInfo{
				{Name: "config.json", Path: "config.json"},
				{Name: "pytorch_model.bin", Path: "pytorch_model.bin"},
			},
			expected: TypeTransformers,
		},
		{
			name: "Generic",
			files: []FileInfo{
				{Name: "README.md", Path: "README.md"},
			},
			expected: TypeGeneric,
		},
		{
			name: "Config without weights",
			files: []FileInfo{
				{Name: "config.json", Path: "config.json"},
			},
			expected: TypeGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.detectType(tt.files, tt.isDataset)
			if result != tt.expected {
				t.Errorf("detectType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAnalyzer_TreeURL(t *testing.T) {
	analyzer := NewAnalyzer(AnalyzerOptions{Endpoint: "https://huggingface.co"})

	tests := []struct {
		name      string
		repo      string
		isDataset bool
		revision  string
		prefix    string
		expected  string
	}{
		{
			name:     "model main",
			repo:     "TheBloke/Mistral-7B",
			revision: "main",
			expected: "https://huggingface.co/api/models/TheBloke/Mistral-7B/tree/main",
		},
		{
			name:      "dataset main",
			repo:      "facebook/flores",
			isDataset: true,
			revision:  "main",
			expected:  "https://huggingface.co/api/datasets/facebook/flores/tree/main",
		},
		{
			name:     "with prefix",
			repo:     "owner/repo",
			revision: "main",
			prefix:   "subdir",
			expected: "https://huggingface.co/api/models/owner/repo/tree/main/subdir",
		},
		{
			name:     "special revision",
			repo:     "owner/repo",
			revision: "v1.0.0",
			expected: "https://huggingface.co/api/models/owner/repo/tree/v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.treeURL(tt.repo, tt.isDataset, tt.revision, tt.prefix)
			if result != tt.expected {
				t.Errorf("treeURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAnalyzer_RepoURL(t *testing.T) {
	analyzer := NewAnalyzer(AnalyzerOptions{Endpoint: "https://huggingface.co"})

	tests := []struct {
		name      string
		repo      string
		isDataset bool
		expected  string
	}{
		{
			name:     "model",
			repo:     "TheBloke/Mistral-7B",
			expected: "https://huggingface.co/TheBloke/Mistral-7B",
		},
		{
			name:      "dataset",
			repo:      "facebook/flores",
			isDataset: true,
			expected:  "https://huggingface.co/datasets/facebook/flores",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.repoURL(tt.repo, tt.isDataset)
			if result != tt.expected {
				t.Errorf("repoURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAnalyzer_RawURL(t *testing.T) {
	analyzer := NewAnalyzer(AnalyzerOptions{Endpoint: "https://huggingface.co"})

	tests := []struct {
		name      string
		repo      string
		isDataset bool
		revision  string
		path      string
		expected  string
	}{
		{
			name:     "model config",
			repo:     "owner/repo",
			revision: "main",
			path:     "config.json",
			expected: "https://huggingface.co/owner/repo/raw/main/config.json",
		},
		{
			name:      "dataset file",
			repo:      "owner/dataset",
			isDataset: true,
			revision:  "main",
			path:      "data/train.parquet",
			expected:  "https://huggingface.co/datasets/owner/dataset/raw/main/data/train.parquet",
		},
		{
			name:     "nested path",
			repo:     "owner/repo",
			revision: "v1.0",
			path:     "deep/nested/file.txt",
			expected: "https://huggingface.co/owner/repo/raw/v1.0/deep/nested/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.rawURL(tt.repo, tt.isDataset, tt.revision, tt.path)
			if result != tt.expected {
				t.Errorf("rawURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAnalyzer_AddAuth(t *testing.T) {
	t.Run("with token", func(t *testing.T) {
		analyzer := NewAnalyzer(AnalyzerOptions{Token: "hf_test_token"})
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		analyzer.addAuth(req)

		if auth := req.Header.Get("Authorization"); auth != "Bearer hf_test_token" {
			t.Errorf("Authorization = %q", auth)
		}
		if ua := req.Header.Get("User-Agent"); ua != "hfdownloader/3" {
			t.Errorf("User-Agent = %q", ua)
		}
	})

	t.Run("without token", func(t *testing.T) {
		analyzer := NewAnalyzer(AnalyzerOptions{})
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		analyzer.addAuth(req)

		if auth := req.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization should be empty, got %q", auth)
		}
		if ua := req.Header.Get("User-Agent"); ua != "hfdownloader/3" {
			t.Errorf("User-Agent = %q", ua)
		}
	})
}

func TestAnalyzer_DetectType_Priority(t *testing.T) {
	analyzer := NewAnalyzer(AnalyzerOptions{})

	// GGUF should have priority over transformers
	t.Run("GGUF over transformers", func(t *testing.T) {
		files := []FileInfo{
			{Name: "config.json", Path: "config.json"},
			{Name: "model.safetensors", Path: "model.safetensors"},
			{Name: "model.Q4_K_M.gguf", Path: "model.Q4_K_M.gguf"},
		}
		result := analyzer.detectType(files, false)
		if result != TypeGGUF {
			t.Errorf("expected GGUF, got %q", result)
		}
	})

	// Diffusers should have priority over transformers
	t.Run("Diffusers over transformers", func(t *testing.T) {
		files := []FileInfo{
			{Name: "config.json", Path: "config.json"},
			{Name: "model.safetensors", Path: "model.safetensors"},
			{Name: "model_index.json", Path: "model_index.json"},
		}
		result := analyzer.detectType(files, false)
		if result != TypeDiffusers {
			t.Errorf("expected Diffusers, got %q", result)
		}
	})

	// LoRA should have priority over transformers
	t.Run("LoRA over transformers", func(t *testing.T) {
		files := []FileInfo{
			{Name: "config.json", Path: "config.json"},
			{Name: "adapter_config.json", Path: "adapter_config.json"},
			{Name: "adapter_model.safetensors", Path: "adapter_model.safetensors"},
		}
		result := analyzer.detectType(files, false)
		if result != TypeLoRA {
			t.Errorf("expected LoRA, got %q", result)
		}
	})
}
