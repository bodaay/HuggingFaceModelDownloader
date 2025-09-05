# HuggingFaceModelDownloader · **v2.0.0**

Fast, resilient, **resumable** CLI (and Go library) for downloading **models** and **datasets** from the Hugging Face Hub—now with a clean flag set, a colorful TUI, structured JSON events, retry/backoff, and **filesystems‑only resume** (no progress files).

> v2.0.0 is a **breaking** redesign of the CLI and library. The old README summarized that this major version simplifies flags, adds dry‑run plans, verification options, multipart thresholding, and JSON output. This document is the definitive, up‑to‑date guide for v2.0.0.&#x20;

---

## Highlights

* **Resumable by default**

  * **LFS** files: verified by **SHA‑256** (when provided by the repo).
  * **Non‑LFS** files: verified by **size**.
  * Large files use **multipart range downloads** with per‑part resume.
* **Beautiful live TUI**

  * Auto‑adapts to terminal width/height; smart truncation; per‑file bars, speeds, ETA.
  * Colorful when supported; graceful plain‑text fallback (`TERM=dumb` or `NO_COLOR=1`).
* **Robust + fast cancellation**

  * Ctrl‑C (SIGINT) or SIGTERM aborts **immediately** across goroutines; second Ctrl‑C exits.
* **Structured progress events** (`--json`) for CI/logging.
* **Practical controls**

  * Overall concurrency and per‑file connection limits.
  * Retry with exponential backoff.
  * Verification policy for non‑LFS files: `none | size | etag | sha256`.
  * Dry‑run “plan” mode (table or JSON).
* **No progress/meta files**

  * Skip decisions are made **only** from what’s on disk (checksums/sizes).
    *Note: the previous README mentioned saving an `.hfdownloader.meta.json`. v2.0.0 no longer persists such files; resume is purely filesystem‑based.*&#x20;

---

## Installation

### From source (Go 1.21+)

```bash
git clone https://github.com/bodaay/HuggingFaceModelDownloader
cd HuggingFaceModelDownloader
go build -o hfdownloader .
# optional:
# go install .   # installs into your $GOBIN
```

### Requirements

* Go **1.21+**
* macOS / Linux / Windows (modern terminals support the live TUI; otherwise it falls back to plain text)

---

## Quick start

```bash
# Public model
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF -o ./Models

# Private or gated repo (requires token)
HF_TOKEN=xxxx hfdownloader download owner/private-model -o ./Models

# Filter LFS artifacts by name; append a subdir for each filter
hfdownloader download TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q5_0 \
  --append-filter-subdir -o ./Models -c 8 --max-active 3

# Dataset mode
hfdownloader download facebook/flores --dataset -o ./Datasets

# Plan only (no downloads), pretty-printed JSON
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_0 --dry-run --plan-format json
```

Tip: You can include filters in `REPO` via `owner/name:filter1,filter2` or pass `-F, --filters`.

---

## CLI

```bash
hfdownloader download [REPO] [flags]
# REPO: owner/name  or  owner/name:filter1,filter2
```

### Common flags

* **Destination**

  * `-o, --output` **Storage** — base folder for downloads
* **What to fetch**

  * `-r, --repo` *(optional if REPO positional is present)*
  * `--dataset` — treat repo as dataset instead of model
  * `-b, --revision` **main** — branch/tag/sha
  * `-F, --filters` — comma‑separated LFS name filters (`q4_0,q5_0`)
  * `--append-filter-subdir` — put each filter’s files under its own subfolder
* **Speed and parallelism**

  * `-c, --connections` **8** — per‑file HTTP range connections
  * `--max-active` **GOMAXPROCS** — max concurrent files
  * `--multipart-threshold` **32MiB** — only multipart files ≥ threshold
* **Reliability**

  * `--retries` **4** — retry attempts per request/part
  * `--backoff-initial` **400ms**, `--backoff-max` **10s**
  * `--verify` **size** — non‑LFS verification: `none | size | etag | sha256`
* **Planning / logging**

  * `--dry-run` — plan only (no downloads)
  * `--plan-format` **table** — `table | json` (with `--dry-run`)
  * `--json` — emit machine‑readable events
  * `-q, --quiet` — reduce console noise
  * `-v, --verbose` — more diagnostics
* **Auth & config**

  * `-t, --token` — Hugging Face token (or `HF_TOKEN` env)
  * `--config` — path to JSON config (defaults to `~/.config/hfdownloader.json` if present)

> The old README showed a larger flag surface and described resume/overwrite toggles and metadata persistence; v2.0.0 deliberately simplifies this—**resume is always on**, and skip decisions are based on your files only (no metadata saved).&#x20;

---

## Live TUI (default)

* Header: repo, revision, dataset flag, output dir, connections, verification, retries.
* Global progress bar with **%**, **bytes**, **speed**, **ETA**.
* Per‑file rows: **status** (▶/✓/•/×), **filename** (middle‑ellipsis), **bar**, **bytes**, **speed**, **ETA**.
* Auto‑adjusts to width/height; plain text fallback when colors/ANSI aren’t available.

*Disable color*: set `NO_COLOR=1` or run in non‑TTY pipelines.
*JSON mode*: `--json` bypasses the TUI for structured events (see below).

---

## JSON events (for CI/logging)

The downloader emits typed events:

* `scan_start`
* `plan_item` *(one per file; includes size and whether it’s LFS)*
* `file_start`
* `file_progress` *(bytes/total, periodic for multipart)*
* `retry` *(attempt #, message)*
* `file_done` *(includes `message: "skip (...)"` when a file is skipped)*
* `error`
* `done`

Example:

```json
{"time":"2025-09-05T18:42:10Z","event":"scan_start","repo":"owner/name","revision":"main","message":"scanning repo"}
{"time":"2025-09-05T18:42:11Z","event":"plan_item","path":"model-q4_0.gguf","total":4227858432,"repo":"owner/name","revision":"main"}
{"time":"2025-09-05T18:42:29Z","event":"file_done","path":"model-q4_0.gguf","repo":"owner/name","revision":"main"}
{"time":"2025-09-05T18:42:29Z","event":"done","message":"download complete (downloaded 1, skipped 12)","repo":"owner/name","revision":"main"}
```

> **Skip lines**: each file prints **at most one** “skip (…)” per run. Deduplication is enforced internally.

---

## Resume & verification (how it decides to skip)

* **LFS files (SHA available)** → compute local **SHA‑256** and compare to the repo’s SHA.

  * If equal → **skip** (`file_done` with `skip (sha256 match)`).
  * If different (even if size matches) → **re‑download**.
* **Non‑LFS / SHA unknown** → compare **file size**.

  * If equal → **skip** (`skip (size match)`).
  * If different → **re‑download**.
* **Multipart parts** → each range part downloads to `path.part-00`, `path.part-01`, …; matching‑length parts are **not re‑fetched** and the file is assembled when all parts are present.

---

## Cancellation & signals

* **SIGINT/SIGTERM**: fast, cooperative cancellation—no new work is scheduled, all goroutines exit promptly, and ongoing HTTP calls are context‑bound.
* **SIGKILL (9)**: cannot be intercepted by any program; the OS terminates immediately.

---

## Examples

**Download GGUF variants into separate subfolders**

```bash
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_0,q5_0 \
  --append-filter-subdir -o ./Models --connections 8 --max-active 3
```

**Stricter verification (non‑LFS)**

```bash
hfdownloader download owner/name --verify etag
```

**Datasets**

```bash
hfdownloader download huggingface/awesome-dataset --dataset -o ./Datasets
```

**Plan first, then run**

```bash
hfdownloader download owner/name:q4_0 --dry-run --plan-format json
hfdownloader download owner/name:q4_0
```

---

## Configuration file

If `--config` is not provided, the tool will read `~/.config/hfdownloader.json` when present and use values as **defaults** (CLI flags still override).

Example:

```json
{
  "output": "Storage",
  "connections": 8,
  "max-active": 3,
  "multipart-threshold": "32MiB",
  "verify": "size",
  "retries": 4,
  "backoff-initial": "400ms",
  "backoff-max": "10s",
  "token": "hf_xxx"
}
```

---

## Library usage (Go)

```go
package main

import (
  "context"
  "log"

  "github.com/bodaay/HuggingFaceModelDownloader/hfdownloader"
)

func main() {
  job := hfdownloader.Job{
    Repo:      "TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_0",
    Revision:  "main",
    IsDataset: false,
    Filters:   []string{"q4_0"},
    // AppendFilterSubdir: true, // optional
  }

  cfg := hfdownloader.Settings{
    OutputDir:          "Storage",
    Concurrency:        8,
    MaxActiveDownloads: 3,
    MultipartThreshold: "32MiB",
    Verify:             "size",    // none|size|etag|sha256
    Retries:            4,
    BackoffInitial:     "400ms",
    BackoffMax:         "10s",
    Token:              "",        // or os.Getenv("HF_TOKEN")
  }

  progress := func(ev hfdownloader.ProgressEvent) {
    switch ev.Event {
    case "file_done":
      if strings.HasPrefix(ev.Message, "skip") {
        log.Printf("skip: %s (%s)", ev.Path, ev.Message)
      } else {
        log.Printf("done: %s", ev.Path)
      }
    case "retry":
      log.Printf("retry %s: attempt %d: %s", ev.Path, ev.Attempt, ev.Message)
    }
  }

  if err := hfdownloader.Download(context.Background(), job, cfg, progress); err != nil {
    log.Fatal(err)
  }
}
```

**Events** you can handle: `scan_start`, `plan_item`, `file_start`, `file_progress`, `retry`, `file_done`, `error`, `done`.

---

## Troubleshooting

* **401 Unauthorized**
  Provide a token: `-t TOKEN` or `HF_TOKEN=...`. Some repos require auth/acceptance.
* **403 Forbidden (terms)**
  Visit the repo page and accept terms, then retry.
* **Range requests disabled**
  Multipart falls back to a single GET automatically; downloads still work.
* **Slow throughput**
  Increase `--connections` and `--max-active` gradually; ensure disk/FS and network can keep up.
* **Repeated “skip” lines**
  v2.0.0 emits **at most one** “skip (…)” per file **per run**. If you still see duplicates, check for duplicate paths in the upstream tree or path collisions on Windows.

---

## Why v2?

* Cleaner mental model (one **download** command, sensible defaults).
* Filesystem‑only resume—**reliable and transparent**; no “state” files to corrupt.
* JSON events and a TUI that looks great everywhere.
* Strong cancellation story for real‑world, long‑running downloads.

> The prior README’s CLI outline and examples helped guide this cleanup; this version documents the finalized v2 surface and behavior.&#x20;

---

## License

Apache‑2.0 (see `LICENSE`).

---

## Acknowledgements

Thanks to the HF community and tooling ecosystem—this project tries to be a pragmatic drop‑in for everyday model & dataset fetching.
