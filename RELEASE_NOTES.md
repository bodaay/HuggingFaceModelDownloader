# Release Notes - v3.0.0

> **Release Date:** January 2026
> **The HuggingFace-Native Release**

## Highlights

Version 3.0.0 is a major release that brings **full HuggingFace CLI compatibility**. Your downloads are now stored in the standard HuggingFace cache structure, making them instantly accessible to Transformers, Diffusers, and any other HuggingFace-based tools.

---

## New Features

### HuggingFace Cache Structure (Default)

Downloads now go directly to `~/.cache/huggingface/hub/` - the same location used by `huggingface_hub`, Transformers, and other HuggingFace tools.

```bash
# Download a model - it's immediately available to Transformers
hfdownloader download microsoft/DialoGPT-medium

# Use it directly in Python - no copying needed!
from transformers import AutoModel
model = AutoModel.from_pretrained("microsoft/DialoGPT-medium")
```

### Dual-Layer Storage

Get the best of both worlds:
- **HuggingFace cache**: Content-addressable blobs for deduplication
- **Human-readable symlinks**: Easy file browsing at `~/.cache/huggingface/hub/models--{owner}--{repo}/snapshots/{revision}/`

### Multi-Revision Support

Download specific branches, tags, or commits:

```bash
# Download a specific branch
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF --revision main

# Download a specific tag
hfdownloader download owner/repo --revision v1.0.0
```

### Model Analysis

Understand models before downloading with the new `analyze` command:

```bash
hfdownloader analyze microsoft/DialoGPT-medium
```

Shows:
- Model architecture and framework
- File types and sizes
- Quantization formats available
- Recommended filters for your use case

### Enhanced Web UI

- **Revision picker** - Select branches/tags from a dropdown
- **Model analysis** - Analyze before downloading
- **Cache browser** - Explore your local HuggingFace cache
- **Authentication** - Secure your web server with `--auth-user` and `--auth-pass`

### Web Authentication

Secure your web server when exposing to networks:

```bash
hfdownloader serve --auth-user admin --auth-pass secret
```

### Legacy Mode

Still need the old flat directory structure? Use `--legacy`:

```bash
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF --legacy -o ./models
```

---

## Docker

Docker images are now automatically published to GitHub Container Registry:

```bash
# Pull the image
docker pull ghcr.io/bodaay/huggingfacemodeldownloader:3.0.0

# Run with HuggingFace cache mount
docker run --rm -p 8080:8080 \
  -v ~/.cache/huggingface:/home/hfdownloader/.cache/huggingface \
  ghcr.io/bodaay/huggingfacemodeldownloader:3.0.0 serve
```

---

## Quick Start

```bash
# Analyze a model (no download)
bash <(curl -sSL https://g.bodaay.io/hfd) analyze TheBloke/Mistral-7B-Instruct-v0.2-GGUF

# Download Q4_K_M quantization only
bash <(curl -sSL https://g.bodaay.io/hfd) download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_k_m

# Start web UI with authentication
bash <(curl -sSL https://g.bodaay.io/hfd) serve --auth-user admin --auth-pass secret

# Install permanently
bash <(curl -sSL https://g.bodaay.io/hfd) -i
```

---

## Migration from V2

V3 uses a different storage structure by default. Your V2 downloads remain intact.

**Option 1**: Keep using legacy mode for existing workflows
```bash
hfdownloader download owner/repo --legacy -o ./models
```

**Option 2**: Re-download to HuggingFace cache (recommended)
```bash
# New downloads go to ~/.cache/huggingface/hub/ by default
hfdownloader download owner/repo
```

---

## Breaking Changes

- **Default storage location changed**: Downloads now go to `~/.cache/huggingface/hub/` instead of `./Models/`
- **Directory structure changed**: Uses HuggingFace blob/symlink structure instead of flat files
- **`-o` flag behavior changed**: In V3 mode, sets `HF_HOME`; in legacy mode, sets direct output path

---

## Full Changelog

### New Features
- HuggingFace cache structure as default storage
- `analyze` command for model inspection
- `--revision` flag for multi-branch downloads
- Web UI revision picker
- Web UI cache browser
- Web UI authentication (`--auth-user`, `--auth-pass`)
- GitHub Actions for automated releases
- GitHub Container Registry for Docker images

### Improvements
- Dual-layer storage (blobs + symlinks)
- Better progress display
- Improved error messages

### Legacy Support
- `--legacy` flag for V2-style flat directory structure
- Existing V2 workflows continue to work

---

---

**Full Changelog**: https://github.com/bodaay/HuggingFaceModelDownloader/compare/v2.3.3...v3.0.0
