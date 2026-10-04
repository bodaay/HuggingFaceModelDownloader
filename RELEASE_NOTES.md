# Release Notes - v3.4.0

> **Release Date:** October 2026
> **Export, Windows Links & Smarter Quantization Picking**

## Highlights

Copy what you already downloaded out as plain files for LM Studio, Ollama or
llama.cpp without re-downloading, a cache that works on Windows without
Developer Mode, download huge sharded models in batches, and pick EXL2/EXL3
bitrates that live on separate branches.

## Export & Flat Files (#83, #91)

- **`hfdownloader export <repo> <dest>`** copies a cached repo out as plain
  files in its own layout — nothing is re-downloaded, and the export is
  independent of the cache. `-F` exports only some weights (tokenizers and
  configs always come along), `-b` picks a revision, and `--mode hardlink`
  shares the cache's disk space instead of copying (same drive only).
- **Web UI: "Export as real files"** on any cached repo, copied to
  `<export-dir>/<owner>/<name>`. Enabled with `serve --export-dir <folder>`;
  without it the API refuses, so it can't write anywhere else.

## Windows: Hardlinks Instead of Missing Links

- Without Administrator/Developer Mode, Windows used to skip the cache's
  links entirely — files ended up only as `blobs/<sha256>`, invisible to
  Python, the HF CLI and you. hfdownloader now uses **hardlinks** there
  (no extra disk space). On drives without any links (FAT/exFAT) it copies,
  asking once first in an interactive terminal.
- New `--link-mode auto|symlink|hardlink|copy` (download and serve, config
  key `link-mode`).
- **`hfdownloader rebuild` repairs caches written by older Windows builds**
  (recreates the missing snapshot entries from the download manifest).

## Downloading

- **`--shards 1-100`** (or `1-50,120-185`) downloads only those shards of
  split files, so a huge model can be fetched in batches (#90).
- **Quantizations on branches (#94):** EXL3/EXL2 repos that keep each
  bitrate on its own branch (`turboderp/...-exl3`: `4.00bpw`,
  `SC_6.00bpw_H6_V6`; bartowski exl2: `4_25`, `6_5`) are listed with bits,
  head bits and size; `analyze` recommends one and `-b <branch>` downloads it
  (CLI, `analyze -i` and the web UI).
- **Weight-format choice for every model type** (safetensors, PyTorch, ONNX,
  TensorFlow, TFLite, Flax, OpenVINO, Rust) — e.g. whisper-tiny's
  recommended download is 148 MiB instead of 580 MiB of four formats.
- GGUF repos offer every mmproj precision; diffusers LoRAs are detected.

## Web UI & API

- Pause button now appears on jobs that waited in the queue; download speed
  is shown; skipped files show as "skipped".
- Saving settings no longer wipes other config keys (`cache-dir`,
  `stall-timeout`, ...), validates values (bad values used to break every
  later job) and writes the config file privately (0600).
- A second download of the same repo with different filters is its own job;
  deleting a repo's cache while it downloads is refused (409).
- Analyze and plan honor your proxy, return 404/401 for missing or gated
  repos, and no longer cut off slow analyses of huge repos.
- Request bodies are capped at 1 MiB; `--auth-user` requires `--auth-pass`.

## Security

- Revisions are validated wherever they become paths (download, export, web
  API): `..`-style revisions could read files outside the cache through the
  new export endpoint during this release cycle (fixed before release).

## Behavior Changes

- **Go 1.25 is now the minimum** for building from source (security fixes in
  `golang.org/x/net` require it; Go 1.24 is no longer supported upstream).
  Release binaries and the Docker image are built with Go 1.26.8.
- On Windows, cache entries are hardlinks instead of missing.
- Filtered GGUF downloads also skip large LFS `.imatrix` files.
- `list`/`info` show sizes as KiB/MiB/GiB.

## Internal

- `golang.org/x/net` v0.56.0 / `x/sys` v0.46.0: `govulncheck` finds no
  vulnerabilities; `staticcheck` clean; the codebase is `gofmt`ed.
- Docs (`docs/CLI.md`, `docs/API.md`, README) were checked flag by flag and
  endpoint by endpoint against the binary.

**Full Changelog**: https://github.com/bodaay/HuggingFaceModelDownloader/compare/v3.3.0...v3.4.0

---

# Release Notes - v3.3.0

> **Release Date:** October 2026
> **Download Reliability, Security & Smarter Selection**

## Highlights

Downloads no longer hang on stalled connections, every file of large repos is
listed, the web server rejects requests from other websites, and the analyzer
picks the right files for GGUF, diffusers, quantized and dataset repos.

## Download Reliability (#87, #88)

- **Stalled transfers recover.** A connection that stays open but stops sending
  data is abandoned and retried from where it stopped after `--stall-timeout`
  (default 60s; `0` disables). Previously such a download hung forever at ~90%.
- **Timeouts everywhere.** Dial, response-header and HTTP/2 health-check
  timeouts; SOCKS5 dials can no longer hang.
- **Smarter retries.** The retry budget resets whenever a retry makes progress;
  401/403/404 fail immediately with a clear "gated or private repo" message
  instead of being retried per file and per part; `Retry-After` and the Hub's
  rate-limit header are honored.
- **No more silent 100%.** Joining parts and SHA256 verification report
  progress (CLI, TUI, and a new activity line in the web UI) and can be
  cancelled.
- **Complete file listings.** Repo trees are listed recursively with
  pagination: folders with more than 1,000 files were silently truncated
  (`allenai/c4` planned 4,515 of 69,221 files). Also far fewer API calls.
- **Safer resumes and storage.** Parts written with a different connection
  count are discarded instead of corrupting the file; files that fail
  verification are removed; byte-identical files (e.g. Q2_K and Q2_K_L) are
  downloaded once; downloads are pinned to the resolved commit (with a
  fallback for mirrors that only accept branch names).
- **Web UI updates no longer dropped.** The WebSocket sent several messages in
  one frame, which the browser failed to parse, so jobs could look stuck.
- Pausing and quickly resuming a job no longer cancels it.

## Security

- **Cross-origin requests are rejected.** With no allowed origins configured,
  the server accepted every website's origin, so any page you visited could
  drive a running `hfdownloader serve`. Requests from other origins now get
  403; the web UI and curl/CLI clients are unaffected. New
  `serve --allow-origin` for reverse proxies.
- **Path traversal fixed.** `GET /api/cache/x/..%2F..%2F...` could read file
  names and refs outside the cache. Repo IDs are now validated wherever they
  become paths (CLI and server).

## Smarter Selection

- **Filters match full paths**, so folder names work (`-F unet`, `-F Q4_K_M/`,
  `-F cola/`). As always, only LFS files go through filters; tokenizers,
  configs and other small files are always downloaded.
- **Unmatched weight/data files of every format are skipped.** Previously only
  six extensions were, so `gpt2 -F safetensors` also pulled ONNX, TF, Flax,
  TFLite and Rust weights (4.74 GiB → 0.52 GiB), `-F train` on datasets
  pulled every split, and Falcon-180B `-F q4_k_m` pulled every old-style
  `.gguf-split` part (1,264 GiB → 101 GiB).
- **Web UI downloads only what you selected**, even when every item is
  ticked (SDXL: ~72 GiB → 6.5 GiB).
- **Exact matching for selections (#96).** `analyze -i` and every command the
  analyzer prints use `--exact`, so picking Q6_K no longer also downloads
  Q6_K_L / Q6_K_XL. A filter that matches nothing now warns.
- **GGUF (#86, #89).** Split shards are one quantization with their combined
  size (Qwen3-Coder-480B: 159 rows → 23); folder-named and custom quants get
  real names instead of "Unknown"; MTP draft models are optional companions
  like mmproj; `UD-`, TQ1_0/TQ2_0, MXFP4 and ARM `Q4_0_4_4` quants are
  recognized; one quant is recommended.
- **Diffusers.** One weight format per component (fp16 safetensors preferred;
  never Flax/ONNX/OpenVINO): SDXL base recommended 38.96 GiB → 6.46 GiB;
  repos that got no weights (segmind/tiny-sd) now work.
- **Model types.** Transformers repos that also ship ONNX exports are no longer
  labeled ONNX; GPTQ, AWQ, bitsandbytes, FP8, MLX (#81) and EXL3 (#94,
  single-repo layout) are detected from the repo's config. Large config files
  are now read completely (they were silently truncated).
- **Datasets.** Splits are recognized inside file names (`c4-train.00000`),
  configs are selectable, and the useless `-F default` is gone.
- **Web UI command fix.** The download wizard's copyable command used flags
  that don't exist (`-r`, `-d`, `-f`, `-e`); it now matches the CLI.

## Behavior Changes

- Filtered downloads no longer include unmatched weight/data files in other
  formats (ONNX, TF, Flax, TFLite, parquet of other splits, imatrix). Small
  files and tokenizers are unaffected; unfiltered downloads are unchanged.
- Web server requests from other origins are rejected (use `--allow-origin`).
- Repo IDs must be `owner/name` with letters, digits, `-`, `_`, `.`.

## Internal

- New `--stall-timeout` flag and config key; `progress.activity` in the jobs
  API; tests no longer touch the developer's real config; ~60 new tests,
  including fake-Hub end-to-end tests.
- Release binaries are built with Go 1.26.8 (`toolchain` in go.mod; previous
  releases used Go 1.24.0, missing later standard-library security fixes).
  The minimum Go version for building from source is unchanged (1.24).

**Full Changelog**: https://github.com/bodaay/HuggingFaceModelDownloader/compare/v3.2.0...v3.3.0

---

# Release Notes - v3.2.0

> **Release Date:** June 2026
> **Security, Reliability & Web-UI Hardening**

## Highlights

A hardening release: security and concurrency fixes for the web server, stronger
download integrity, a streaming/verified mirror, and a Web UI that downloads one
model at a time like the CLI.

## Security & Concurrency

- **WebSocket hub races fixed.** Slow-client eviction now runs under the write
  lock (was a concurrent map write under a read lock that could panic the hub
  and kill all live progress); the send channel is closed exactly once.
- **WebSocket origin checks.** `CheckOrigin` now validates the `Origin` header
  against the configured allow-list (same-origin allowed, cross-origin denied by
  default) to stop cross-site WebSocket hijacking.
- **Settings updates are race-free.** `POST /api/settings` takes a write lock and
  publishes the proxy config copy-on-write, so it no longer races in-flight
  downloads or `GET /api/settings`.
- Constant-time basic-auth comparison; checked `crypto/rand`; the WebSocket
  broadcast coalescer is now stopped on graceful shutdown.

## Reliability

- **Stronger verification.** Files are SHA256-verified whenever a content hash is
  known (covers multipart output); `--verify etag` now actually verifies instead
  of silently doing nothing. Per-file errors are aggregated.
- **Path-traversal guard.** Entries from the (operator-configurable, remote)
  repo tree that escape the repo root are rejected before any file is written.
- **Mirror push/pull** streams blobs with `io.Copy` (no whole-file-into-RAM) and
  `--verify` performs a real SHA256 integrity check. The shared copy/verify
  primitives now live in `pkg/hfdownloader`.
- Typed errors (`*APIError`/`*DownloadError`/`*VerificationError`) so
  `errors.Is/As` works against the documented sentinels.

## Web UI

- **One model at a time (#85).** Adding several models via Analyze used to start
  them all at once. Downloads now run serially (matching the CLI); extra models
  stay `queued` until the active one finishes. The setting formerly labeled
  "Concurrent downloads" is now **"Parallel files per model"**, which is what it
  actually controls.

## Internal / Docs

- Version is sourced from the build (`/api/health`, WebSocket init, web UI footer)
  instead of hardcoded strings.
- Removed dead, never-called blob-coordination helpers.
- `docs/API.md` documents the dismiss, mirror and cache rebuild/delete endpoints;
  removed non-existent `serve --cors` / `rebuild <repo>` examples; documented
  `analyze -i` and `download --exact`.

**Full Changelog**: https://github.com/bodaay/HuggingFaceModelDownloader/compare/v3.1.1...v3.2.0

---

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
