# HuggingFaceModelDownloader — v2.0.0 (breaking)

This is a **clean, breaking** redesign of the CLI and library. Compared to v1.x:
- Old flags are removed. The CLI is simplified and consistent.
- New engine adds **retries with backoff**, **dry-run plans**, **config file** support,
  **non-LFS verification policies**, **multipart threshold**, **overall concurrency limit**,
  and **structured progress** with `--json` output.

## CLI

```bash
hfdownloader download [REPO] [flags]
```

- `REPO`: `owner/name` or `owner/name:filter1,filter2`

**Key flags**
- `-o, --output` destination (default: `Storage`)
- `-b, --revision` branch/tag/sha (default: `main`)
- `--dataset` treat repo as a dataset
- `-F, --filters` comma-separated LFS filters (if omitted, parsed from `REPO` suffix)
- `--append-filter-subdir` put each filter in its own subdir
- `-c, --connections` per-file range connections (default: `8`)
- `--max-active` max files downloading at once (default: `GOMAXPROCS`)
- `--multipart-threshold` only range-download files ≥ threshold (default: `32MiB`)
- `--verify` non-LFS verification: `none|size|etag|sha256` (LFS verifies sha when provided unless `none`)
- `--retries`, `--backoff-initial`, `--backoff-max` retry policy
- `--dry-run` (with `--plan-format table|json`) to print plan only
- `--resume` (default **true**), `--overwrite` (mutually exclusive behavior)
- `-t, --token` or `HF_TOKEN` for gated repos
- `--json`, `--quiet`, `--verbose`
- `--config` path to JSON config (if not set, `~/.config/hfdownloader.json` is used when present)

**Examples**
```bash
# Model with filters
HF_TOKEN=xxxx hfdownloader download TheBloke/vicuna-13b-v1.3.0-GGML:q4_0,q5_0 \
  --append-filter-subdir -o ./Models -c 8 --max-active 3

# Dataset
hfdownloader download facebook/flores --dataset -o ./Datasets

# Plan only
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_0 --dry-run --plan-format json

# Stricter verification
hfdownloader download myorg/myrepo --verify etag
```

## Library

New API:
```go
err := hfdownloader.Download(context.Background(), hfdownloader.Options{
    Repo: "owner/name",
    OutputDir: "Storage",
    // set flags as needed...
    Progress: func(ev hfdownloader.ProgressEvent) { ... },
})
```

Removed v1.x symbols:
- `DownloadModel` and globals like `NumConnections`, `RequiresAuth`, `AuthToken`.
Use `Options` + `Download` instead.

## Config

`hfdownloader generate-config` writes an example to `~/.config/hfdownloader.json`.  
Download honors `--config` or the default file if present (values act as flag defaults unless overridden on the CLI).

## Notes

- ETag/sha metadata saved to `.hfdownloader.meta.json` under the repo folder improves skip decisions in future runs.
- Range requests require `Accept-Ranges: bytes` on the resolved URL. If unsupported, the tool falls back to single GET (based on the plan’s capability check).
