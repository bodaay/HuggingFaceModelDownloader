# HFDownloader REST API Reference

Complete REST API documentation for `hfdownloader serve`.

---

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Base URL](#base-url)
- [Endpoints](#endpoints)
  - [Health Check](#health-check)
  - [Downloads](#downloads)
  - [Jobs](#jobs)
  - [Settings](#settings)
  - [Analyzer](#analyzer)
  - [Cache](#cache)
- [WebSocket API](#websocket-api)
- [Error Handling](#error-handling)
- [Examples](#examples)

---

## Overview

Start the server:

```bash
hfdownloader serve --port 8080
```

The server provides:
- **REST API** for download management
- **WebSocket** for real-time progress updates
- **Web UI** at the root URL

### Content Types

- Request bodies: `application/json`, at most **1 MiB** (larger bodies are
  rejected with `400` and details `http: request body too large`)
- Response bodies: `application/json`
- WebSocket messages: JSON

---

## Authentication

### Basic Authentication (Optional)

Enable with server flags:

```bash
hfdownloader serve --auth-user admin --auth-pass secret123
```

All requests require the `Authorization` header:

```http
Authorization: Basic YWRtaW46c2VjcmV0MTIz
```

### HuggingFace Token

For private/gated models, provide via:

1. Server flag: `hfdownloader serve -t hf_xxxxx`
2. Settings API: `POST /api/settings`

---

## Base URL

```
http://localhost:8080/api
```

---

## Endpoints

### Health Check

Check server status.

#### GET /api/health

**Response** `200 OK`

```json
{
  "status": "ok",
  "version": "3.3.0",
  "time": "2024-01-15T10:30:00Z"
}
```

`version` is the running build's version (`dev` for unversioned builds).

**Example**

```bash
curl http://localhost:8080/api/health
```

---

### Downloads

#### POST /api/download

Start a new download job.

**Request Body**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `repo` | string | **Yes** | | Repository ID (owner/name) |
| `revision` | string | No | `main` | Branch, tag, or commit |
| `dataset` | boolean | No | `false` | Treat as dataset |
| `filters` | string[] | No | `[]` | File filter patterns |
| `excludes` | string[] | No | `[]` | Exclude patterns |
| `exactMatch` | boolean | No | `false` | Match filters against whole name segments instead of substrings (`q6_k` matches `Q6_K` but not `Q6_K_XL`), like CLI `--exact` |
| `appendFilterSubdir` | boolean | No | `false` | Accepted, but ignored by server downloads |
| `dryRun` | boolean | No | `false` | Plan only (returns the `/api/plan` response) |

Where files are written is decided by the server (`--cache-dir`, or
`--local-dir` for flat files) and cannot be set per request.

**Filter Syntax**

Filters can be embedded in repo name:

```json
{ "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_k_m,q5_k_m" }
```

Or as separate field:

```json
{
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "filters": ["q4_k_m", "q5_k_m"]
}
```

**Response** `202 Accepted` (New job)

```json
{
  "id": "a1b2c3d4e5f6",
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "revision": "main",
  "filters": ["q4_k_m"],
  "outputDir": "/home/user/.cache/huggingface",
  "status": "queued",
  "progress": {
    "totalFiles": 0,
    "completedFiles": 0,
    "totalBytes": 0,
    "downloadedBytes": 0,
    "bytesPerSecond": 0
  },
  "createdAt": "2024-01-15T10:30:00Z"
}
```

Fields that are empty are omitted from job objects: `isDataset`, `filters`,
`excludes`, `flat` (true when the server runs with `--local-dir`),
`exactMatch`, `error`, `startedAt`, `endedAt`, `files`, and
`progress.activity`. `outputDir` is the cache root (or the `--local-dir`
directory in flat mode).

The server downloads one job at a time; further jobs stay `queued` until the
current one finishes.

**Response** `200 OK` (Existing job)

A request is a duplicate when a `queued` or `running` job has the same `repo`,
`revision`, `dataset`, `filters` and `excludes` (in any order) and
`exactMatch`. The existing job is returned instead of starting a new one:

```json
{
  "job": { /* job object */ },
  "message": "Download already in progress"
}
```

**Examples**

```bash
# Basic download
curl -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{"repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF"}'

# With filters
curl -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{
    "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
    "filters": ["q4_k_m", "q5_k_m"]
  }'

# Dataset download
curl -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{"repo": "facebook/flores", "dataset": true}'

# Specific revision
curl -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{"repo": "CompVis/stable-diffusion-v1-4", "revision": "fp16"}'
```

---

#### POST /api/plan

Get download plan without starting download.

**Request Body**

Same as `/api/download` (`dryRun` is implied).

**Response** `200 OK`

```json
{
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "revision": "main",
  "files": [
    {
      "path": "config.json",
      "size": 1024,
      "lfs": false
    },
    {
      "path": "mistral-7b.Q4_K_M.gguf",
      "size": 4368438272,
      "lfs": true
    }
  ],
  "totalSize": 4368439296,
  "totalFiles": 2
}
```

Hub errors map to `404` (repo/revision not found), `401` (gated, private or
non-existent repo the Hub refuses to reveal), `504` (timeout) or `502` (other
upstream errors), with `"error": "Failed to scan repository"`.

**Example**

```bash
curl -X POST http://localhost:8080/api/plan \
  -H "Content-Type: application/json" \
  -d '{"repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF", "filters": ["q4_k_m"]}'
```

---

### Jobs

#### GET /api/jobs

List all download jobs.

**Response** `200 OK`

```json
{
  "jobs": [
    {
      "id": "a1b2c3d4e5f6",
      "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
      "revision": "main",
      "filters": ["q4_k_m"],
      "outputDir": "/home/user/.cache/huggingface",
      "status": "running",
      "progress": {
        "totalFiles": 3,
        "completedFiles": 1,
        "totalBytes": 4500000000,
        "downloadedBytes": 1500000000,
        "bytesPerSecond": 50000000,
        "activity": "Verifying model.Q4_K_M.gguf (45%)"
      },
      "createdAt": "2024-01-15T10:30:00Z",
      "startedAt": "2024-01-15T10:30:01Z",
      "files": [
        {
          "path": "config.json",
          "totalBytes": 1024,
          "downloaded": 1024,
          "status": "complete"
        },
        {
          "path": "mistral-7b.Q4_K_M.gguf",
          "totalBytes": 4368438272,
          "downloaded": 1500000000,
          "status": "verifying"
        }
      ]
    }
  ],
  "count": 1
}
```

**Example**

```bash
curl http://localhost:8080/api/jobs
```

---

#### GET /api/jobs/{id}

Get specific job details.

**Path Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | string | Job ID |

**Response** `200 OK`

```json
{
  "id": "a1b2c3d4e5f6",
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "status": "running",
  /* ... full job object ... */
}
```

**Response** `404 Not Found`

```json
{
  "error": "Job not found"
}
```

**Example**

```bash
curl http://localhost:8080/api/jobs/a1b2c3d4e5f6
```

---

#### DELETE /api/jobs/{id}

Cancel a queued, running or paused job.

**Response** `200 OK`

```json
{
  "success": true,
  "message": "Job cancelled"
}
```

**Response** `404 Not Found`

```json
{
  "error": "Job not found or already completed"
}
```

**Example**

```bash
curl -X DELETE http://localhost:8080/api/jobs/a1b2c3d4e5f6
```

---

#### POST /api/jobs/{id}/pause

Pause a running job.

**Response** `200 OK`

```json
{
  "success": true,
  "message": "Job paused"
}
```

**Response** `404 Not Found`

```json
{
  "error": "Job not found or not running"
}
```

**Example**

```bash
curl -X POST http://localhost:8080/api/jobs/a1b2c3d4e5f6/pause
```

---

#### POST /api/jobs/{id}/resume

Resume a paused job.

**Response** `200 OK`

```json
{
  "success": true,
  "message": "Job resumed"
}
```

**Response** `404 Not Found`

```json
{
  "error": "Job not found or not paused"
}
```

**Note**: Only `paused` jobs can be resumed (and only `running` jobs paused).
Resumed jobs restart from `queued` status. Progress is reset, but
already-downloaded files are automatically skipped.

**Example**

```bash
curl -X POST http://localhost:8080/api/jobs/a1b2c3d4e5f6/resume
```

---

### Job Status Lifecycle

```
queued ─────► running ─────► completed
                │
                ├─────► failed
                │
                ├─────► cancelled
                │
                └─────► paused ─────► queued ─► running ─► ...
```

| Status | Description |
|--------|-------------|
| `queued` | Waiting to start |
| `running` | Download in progress |
| `paused` | Paused by user |
| `completed` | Finished successfully |
| `failed` | Error occurred |
| `cancelled` | Cancelled by user |

While a job is `running`, `progress.activity` describes anything other than
bytes flowing — assembling a multipart file, verifying its SHA256, or retrying
a stalled or dropped transfer (e.g. `"Verifying model.gguf (45%)"`). It is
omitted when empty.

`progress.bytesPerSecond` is computed by the server (a moving average sampled
about once a second) and is reset to `0` when the job ends.

Per-file `status` values: `pending` (planned), `active` (downloading),
`assembling` (joining multipart chunks), `verifying` (hashing), `complete`.
Files that were already present and skipped are also reported as `complete`.
(`skipped` and `error` are reserved values that the server does not currently
emit.)

---

### Settings

#### GET /api/settings

Get current server settings.

**Response** `200 OK`

```json
{
  "token": "********mnop",
  "cacheDir": "/home/user/.cache/huggingface",
  "connections": 8,
  "maxActive": 3,
  "multipartThreshold": "32MiB",
  "verify": "size",
  "retries": 4,
  "endpoint": "https://hf-mirror.com",
  "storageMode": "cache",
  "exportDir": "/srv/exports",
  "linkMode": "auto",
  "proxy": {
    "url": "http://proxy.corp.com:8080",
    "username": "myuser",
    "noProxy": "localhost,.internal.com",
    "noEnvProxy": false,
    "insecureSkipVerify": false
  },
  "configFile": "/home/user/.config/hfdownloader.json",
  "targetsFile": "/home/user/.config/hfdownloader/targets.yaml"
}
```

| Field | Description |
|-------|-------------|
| `token` | Masked: `********` + last 4 characters. Omitted when no token is set |
| `cacheDir` | HF cache root in use (read-only) |
| `storageMode` | `cache` (HF cache layout) or `local` (server started with `--local-dir`); read-only |
| `localDir` | The `--local-dir` directory; omitted unless `storageMode` is `local` |
| `exportDir` | Where `POST /api/cache/export` writes; omitted when export is disabled |
| `linkMode` | `auto`, `symlink`, `hardlink` or `copy`; omitted when not configured (= `auto`) |
| `endpoint` | Custom Hub endpoint; omitted when using the default |
| `proxy` | Proxy settings (never includes the password); omitted when no proxy URL is set. Inner fields are omitted when empty/false |
| `configFile` | Config file the server reads and saves to |
| `targetsFile` | Mirror targets file |

**Example**

```bash
curl http://localhost:8080/api/settings
```

---

#### POST /api/settings

Update server settings.

**Request Body**

All fields are optional; only the fields present are changed.

| Field | Type | Validation | Description |
|-------|------|------------|-------------|
| `token` | string | | HuggingFace token (`""` clears it) |
| `connections` | integer | 1–64 | Connections per file |
| `maxActive` | integer | 1–32 | Max concurrent file downloads per job |
| `multipartThreshold` | string | size such as `32MiB`, `512KB`, `1GiB` | Min size for multipart |
| `verify` | string | `none`, `size`, `etag`, `sha256` | Verification for files without a known SHA256 |
| `retries` | integer | 0–20 | Retry attempts (`0` is accepted but leaves the value unchanged) |
| `endpoint` | string | `http://` or `https://` URL, or `""` | Custom Hub endpoint (`""` restores the default) |
| `proxy` | object | | `url`, `username`, `password`, `noProxy`, `noEnvProxy`, `insecureSkipVerify`; only fields present are changed. An empty `url` removes the proxy |

An invalid value rejects the whole request with `400`:

```json
{ "error": "Invalid settings", "details": "connections must be between 1 and 64" }
```

**Security Restrictions**
- `cacheDir`, `localDir`, `exportDir`, `linkMode`, `modelsDir` and
  `datasetsDir` cannot be changed via API

**Persistence**

Changes apply immediately to new jobs and are saved to the config file
(`configFile`). Only the keys the API manages are rewritten (`token`,
`connections`, `max-active`, `multipart-threshold`, `verify`, `retries`,
`endpoint`, `proxy`); other keys in the file (`cache-dir`, `backoff-*`,
`link-mode`, ...) are kept.

**Response** `200 OK`

```json
{
  "success": true,
  "message": "Settings saved"
}
```

If the config file cannot be written, the settings still apply in memory and
the message is `"Settings updated (warning: could not persist to config file)"`.

**Example**

```bash
curl -X POST http://localhost:8080/api/settings \
  -H "Content-Type: application/json" \
  -d '{
    "token": "hf_xxxxx",
    "connections": 16,
    "maxActive": 8
  }'
```

---

### Analyzer

#### GET /api/analyze/{repo}

Analyze a HuggingFace repository.

**Path Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `repo` | string | Repository ID (owner/name) |

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `dataset` | boolean | Force dataset type |
| `revision` | string | Branch/tag to analyze |

**Response** `200 OK` (Determined type)

```json
{
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "is_dataset": false,
  "type": "gguf",
  "type_description": "GGUF Model",
  "file_count": 12,
  "total_size": 4500000000,
  "total_size_human": "4.2 GiB",
  "commit": "41b61a33a2483885c981aa79e0df6b32407ed873",
  "branch": "main",
  "refs": [
    {"name": "main", "type": "branch", "commit": "abc123..."}
  ],
  "files": [
    {
      "path": "config.json",
      "name": "config.json",
      "size": 1024,
      "size_human": "1.0 KiB",
      "is_lfs": false,
      "directory": "."
    },
    {
      "path": "mistral-7b.Q4_K_M.gguf",
      "name": "mistral-7b.Q4_K_M.gguf",
      "size": 4368438272,
      "size_human": "4.1 GiB",
      "is_lfs": true,
      "sha256": "abc123...",
      "directory": "."
    }
  ],
  "gguf": {
    "model_name": "Mistral-7B-Instruct-v0.2",
    "quantizations": [
      {
        "name": "Q4_K_M",
        "file": { /* FileInfo */ },
        "quality": 4,
        "quality_stars": "★★★★☆",
        "estimated_ram": 4905066496,
        "estimated_ram_human": "4.6 GiB",
        "description": "Good balance of quality and size"
      }
    ]
  },
  "analyzed_at": "2024-01-15T10:30:00Z"
}
```

Errors: `404` when the repo is not found as a model or dataset, `401` for
gated/private repos without access, `504` on timeout (60 s), `502` for other
upstream errors — all with `"error": "Analysis failed"`.

**Response** `200 OK` (Needs selection)

Intended for a repo that exists as both a model and a dataset:

```json
{
  "needsSelection": true,
  "repo": "owner/name",
  "message": "This repository exists as both a model and a dataset. Please select which one you want to analyze.",
  "options": ["model", "dataset"]
}
```

> **Note:** in the current build this response is not reached: such repos
> fail with `502` `"Analysis failed"` (details
> `fetch file tree: repository exists as both model and dataset`). Add
> `?dataset=true` to analyze the dataset.

**Examples**

```bash
# Analyze model
curl http://localhost:8080/api/analyze/TheBloke/Mistral-7B-Instruct-v0.2-GGUF

# Force dataset
curl "http://localhost:8080/api/analyze/facebook/flores?dataset=true"

# Specific revision
curl "http://localhost:8080/api/analyze/owner/repo?revision=v1.0"
```

---

### Detected Model Types

| Type | Description | Key Fields |
|------|-------------|------------|
| `gguf` | GGUF quantized model | `gguf.quantizations` |
| `transformers` | Transformers model | `transformers.architecture` |
| `diffusers` | Diffusers pipeline | `diffusers.pipeline_type` |
| `lora` | LoRA adapter | `lora.base_model` |
| `gptq` | GPTQ quantized | `quantized.bits` |
| `awq` | AWQ quantized | `quantized.bits` |
| `quantized` | Other quantized model | `quantized.bits` |
| `onnx` | ONNX model | `onnx.models` |
| `audio` | Audio model | `audio.task` |
| `vision` | Vision model | `vision.task` |
| `multimodal` | Multimodal model | `multimodal.modalities` |
| `dataset` | Dataset | `dataset.formats` |
| `generic` | Anything else | |

---

### Cache

#### GET /api/cache

List cached repositories.

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `type` | string | Filter: `model`, `dataset` |
| `search` | string | Case-insensitive substring match on `owner/name` |

**Response** `200 OK`

```json
{
  "repos": [
    {
      "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
      "owner": "TheBloke",
      "name": "Mistral-7B-Instruct-v0.2-GGUF",
      "type": "model",
      "path": "/home/user/.cache/huggingface/hub/models--TheBloke--Mistral-7B-Instruct-v0.2-GGUF",
      "friendlyPath": "/home/user/.cache/huggingface/models/TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
      "size": 4368439584,
      "sizeHuman": "4.1 GiB",
      "fileCount": 4,
      "branch": "main",
      "commit": "41b61a3",
      "downloaded": "2024-01-15",
      "downloadStatus": "filtered",
      "manifest": {
        "branch": "main",
        "commit": "41b61a33a2483885c981aa79e0df6b32407ed873",
        "downloaded": "2024-01-15 10:30",
        "command": "hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF -F q4_k_m",
        "totalSize": 4368439584,
        "totalFiles": 4,
        "isFiltered": true,
        "filters": "q4_k_m"
      }
    }
  ],
  "stats": {
    "totalModels": 1,
    "totalDatasets": 0,
    "totalSize": 4368439584,
    "totalSizeHuman": "4.1 GiB",
    "totalFiles": 4
  },
  "cacheDir": "/home/user/.cache/huggingface"
}
```

`repos` is `null` (not `[]`) when nothing matches. `stats` covers the repos
that matched the filters. `downloadStatus` is `complete`, `filtered` (the
manifest's command used `-F`) or `unknown` (no `hfd.yaml` manifest, e.g.
downloaded by another tool, in which case `manifest` is omitted).

**Examples**

```bash
# List all
curl http://localhost:8080/api/cache

# Models only
curl "http://localhost:8080/api/cache?type=model"

# Search
curl "http://localhost:8080/api/cache?search=mistral"
```

---

#### GET /api/cache/{repo}

Get cached repository details.

**Path Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `repo` | string | Repository ID (owner/name) |

**Response** `200 OK`

Same fields as a `GET /api/cache` entry (except `downloaded`), plus
`snapshots` (snapshot commit hashes) and `files`. A model is looked up first,
then a dataset with the same name.

```json
{
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "owner": "TheBloke",
  "name": "Mistral-7B-Instruct-v0.2-GGUF",
  "type": "model",
  "path": "/home/user/.cache/huggingface/hub/models--TheBloke--Mistral-7B-Instruct-v0.2-GGUF",
  "friendlyPath": "/home/user/.cache/huggingface/models/TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "size": 4368439584,
  "sizeHuman": "4.1 GiB",
  "fileCount": 4,
  "branch": "main",
  "commit": "41b61a3",
  "downloadStatus": "filtered",
  "snapshots": ["41b61a33a2483885c981aa79e0df6b32407ed873"],
  "files": [
    { "name": "config.json", "size": 31, "sizeHuman": "31 B", "isLfs": false },
    { "name": "mistral-7b-instruct-v0.2.Q4_K_M.gguf", "size": 4368439296, "sizeHuman": "4.1 GiB", "isLfs": true }
  ],
  "manifest": { /* as in GET /api/cache */ }
}
```

`isLfs` is a heuristic here (files larger than 10 MiB).

**Response** `404 Not Found`

```json
{
  "error": "Repository not found in cache"
}
```

**Example**

```bash
curl http://localhost:8080/api/cache/TheBloke/Mistral-7B-Instruct-v0.2-GGUF
```

#### POST /api/cache/rebuild

Regenerate the friendly view symlinks from the hub cache.

Request body (optional):

```json
{ "clean": true }
```

`clean` (default `false`) also removes orphaned symlinks.

```bash
curl -X POST http://localhost:8080/api/cache/rebuild -d '{"clean":true}'
```

Response:

```json
{
  "success": true,
  "reposScanned": 5,
  "symlinksCreated": 23,
  "symlinksUpdated": 2,
  "orphansRemoved": 1,
  "message": "Created 23 symlinks, updated 2"
}
```

`orphansRemoved` and `errors` are omitted when zero/empty; `message` is
`"Friendly view is up to date"` when nothing changed.

#### POST /api/cache/export

Export a cached repo as plain files (hardlinked from the cache when on the
same drive, else copied) into `<export-dir>/<owner>/<name>`. Disabled unless
the server was started with `--export-dir` (or `export-dir` in the config
file) — returns 400 otherwise.

Request body (`type` defaults to `model`, `revision` to `main`; `filters`
select weight/data files by exact match, like CLI `export -F`; other files are
always exported):

```json
{ "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF", "type": "model", "revision": "main", "filters": ["q4_k_m"] }
```

Response `200 OK`:

```json
{ "commit": "41b61a3...", "dest": "/exports/TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "files": 4, "hardlinked": 4, "copied": 0, "symlinked": 0, "unchanged": 0, "bytes": 4368439584 }
```

`fromBlobs` (exported from a cache without snapshot links) and `missingBlob`
are included only when set. Errors: `400` (export disabled, invalid body or
repo id), `404` (repo/revision not in the cache), `500` (other failures).

#### DELETE /api/cache/{repo}

Delete a cached repository (blobs, snapshots and friendly-view symlinks). The
repo path is validated to stay inside the cache directory.

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `type` | string | `dataset` to delete a dataset; anything else (or omitted) means model |

Returns `409 Conflict` while the repo has a `queued`, `running` or `paused`
job (cancel it first), `404` if it is not in the cache, and `400` for an
invalid repo id.

```bash
curl -X DELETE http://localhost:8080/api/cache/TheBloke/Mistral-7B-Instruct-v0.2-GGUF
curl -X DELETE "http://localhost:8080/api/cache/facebook/flores?type=dataset"
```

Response `200 OK`:

```json
{ "success": true, "message": "Deleted TheBloke/Mistral-7B-Instruct-v0.2-GGUF from cache" }
```

---

### Jobs (continued)

#### POST /api/jobs/{id}/dismiss

Permanently remove a finished job from the list so it does not reappear on
refresh. Only jobs in a terminal state (`completed`, `failed`, `cancelled`,
`paused`) may be dismissed; dismissing a `queued`/`running` job returns `409`
(cancel it first), an unknown id returns `404`.

Response `200 OK`: `{"success": true, "message": "Job dismissed"}`

```bash
curl -X POST http://localhost:8080/api/jobs/a1b2c3d4e5f6/dismiss
```

---

### Mirror

Manage named mirror targets and copy repos between cache directories.

#### GET /api/mirror/targets

List configured mirror targets.

#### POST /api/mirror/targets

Add a target.

```json
{ "name": "office", "path": "/mnt/nas/hf-cache", "description": "optional" }
```

#### DELETE /api/mirror/targets/{name}

Remove a target by name.

#### POST /api/mirror/diff

Show which repos differ between the local cache and a target.

```json
{ "target": "office", "repoFilter": "" }
```

#### POST /api/mirror/push  •  POST /api/mirror/pull

Copy repos to (`push`) or from (`pull`) a target. Same request body for both:

```json
{
  "target": "office",
  "repoFilter": "",
  "dryRun": false,
  "verify": false,
  "deleteExtra": false,
  "force": false
}
```

`verify` performs a full SHA256 integrity check of each copied blob (slower).

```bash
curl -X POST http://localhost:8080/api/mirror/push -d '{"target":"office","verify":true}'
```

> Note: a large push/pull runs synchronously and may exceed the HTTP server
> write timeout; for very large transfers prefer the `mirror` CLI command.

---

## WebSocket API

Real-time updates via WebSocket connection.

### Connection

```
ws://localhost:8080/api/ws
```

### Connection Parameters

| Parameter | Value |
|-----------|-------|
| Read buffer | 1024 bytes |
| Write buffer | 1024 bytes |
| Max message | 512 KB |
| Ping interval | 30 seconds |
| Read timeout | 60 seconds |
| Write timeout | 10 seconds |

### Message Format

All messages are JSON:

```json
{
  "type": "message_type",
  "data": { /* payload */ }
}
```

### Message Types

#### init

Sent immediately upon connection.

```json
{
  "type": "init",
  "data": {
    "jobs": [ /* all current jobs */ ],
    "version": "3.3.0"
  }
}
```

#### job_update

Broadcast when a job's status or progress changes. `data` is a full job object
(same shape as `GET /api/jobs/{id}`, empty fields omitted). Progress updates
for a job are coalesced to at most one message every 250 ms; status changes to
`completed`, `failed`, `cancelled` or `paused` are sent immediately.

```json
{
  "type": "job_update",
  "data": {
    "id": "a1b2c3d4e5f6",
    "repo": "owner/model",
    "status": "running",
    "progress": {
      "totalFiles": 5,
      "completedFiles": 2,
      "totalBytes": 5000000000,
      "downloadedBytes": 2000000000,
      "bytesPerSecond": 50000000
    },
    "files": [
      {
        "path": "model.bin",
        "totalBytes": 5000000000,
        "downloaded": 2000000000,
        "status": "active"
      }
    ]
  }
}
```

`init` and `job_update` are the only message types the server sends. Messages
sent by the client are read and ignored. A dismissed job produces no message;
clients should drop it locally (or re-fetch `GET /api/jobs`).

### JavaScript Example

```javascript
const ws = new WebSocket('ws://localhost:8080/api/ws');

ws.onopen = () => {
  console.log('Connected');
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  switch (msg.type) {
    case 'init':
      console.log('Jobs:', msg.data.jobs);
      break;
    case 'job_update':
      const job = msg.data;
      const percent = (job.progress.downloadedBytes / job.progress.totalBytes * 100).toFixed(1);
      console.log(`${job.repo}: ${percent}%`);
      break;
  }
};

ws.onerror = (err) => console.error('WebSocket error:', err);
ws.onclose = () => console.log('Disconnected');
```

### Python Example

```python
import asyncio
import websockets
import json

async def monitor():
    uri = "ws://localhost:8080/api/ws"
    async with websockets.connect(uri) as ws:
        async for message in ws:
            msg = json.loads(message)
            if msg["type"] == "job_update":
                job = msg["data"]
                progress = job["progress"]
                if progress["totalBytes"] > 0:
                    pct = progress["downloadedBytes"] / progress["totalBytes"] * 100
                    print(f"{job['repo']}: {pct:.1f}%")

asyncio.run(monitor())
```

---

## Error Handling

### Error Response Format

```json
{
  "error": "Error message",
  "details": "Additional details (optional)"
}
```

### HTTP Status Codes

| Code | Meaning | Usage |
|------|---------|-------|
| 200 | OK | Successful request; duplicate `POST /api/download` (existing job returned) |
| 202 | Accepted | New download job created |
| 204 | No Content | CORS preflight (`OPTIONS`) |
| 400 | Bad Request | Invalid JSON or input, invalid settings values, request body over 1 MiB (`details: "http: request body too large"`), export disabled |
| 401 | Unauthorized | Basic auth required/failed (plain-text body); also analyze/plan of a gated, private or unknown repo the Hub refuses to reveal |
| 403 | Forbidden | Cross-origin browser request from an origin not allowed (see [CORS](#cors)) |
| 404 | Not Found | Unknown job, target or cached repo; analyze/plan of a repo or revision that does not exist; cancel/pause/resume of a job in the wrong state |
| 409 | Conflict | Dismissing a `queued`/`running` job; deleting a cached repo that has a `queued`/`running`/`paused` job |
| 500 | Server Error | Internal error (e.g. cache I/O failure) |
| 502 | Bad Gateway | Analyze/plan: other upstream (Hub) errors |
| 504 | Gateway Timeout | Analyze/plan: Hub request timed out |

### Common Errors

**Invalid Repository**
```json
{
  "error": "Invalid repo format",
  "details": "Expected owner/name"
}
```

**Job Not Found**
```json
{
  "error": "Job not found"
}
```

**Analysis Failed** (`404`)
```json
{
  "error": "Analysis failed",
  "details": "fetch file tree: repository not found as model or dataset: owner/name"
}
```

**Cross-origin request** (`403`)
```json
{
  "error": "Cross-origin request blocked",
  "details": "origin \"https://evil.example\" is not allowed; start the server with --allow-origin https://evil.example to permit it"
}
```

---

## Examples

### Complete Download Workflow

```bash
# 1. Analyze repository
curl http://localhost:8080/api/analyze/TheBloke/Mistral-7B-Instruct-v0.2-GGUF

# 2. Start download with specific quantization
curl -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{"repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF", "filters": ["q4_k_m"]}'

# 3. Monitor progress
curl http://localhost:8080/api/jobs

# 4. Get specific job
curl http://localhost:8080/api/jobs/a1b2c3d4e5f6
```

### Pause/Resume Workflow

```bash
# Start download
JOB_ID=$(curl -s -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{"repo": "owner/large-model"}' | jq -r '.id')

# Pause
curl -X POST "http://localhost:8080/api/jobs/$JOB_ID/pause"

# Resume later
curl -X POST "http://localhost:8080/api/jobs/$JOB_ID/resume"
```

### Real-time Monitoring Script

```bash
#!/bin/bash
# monitor.sh - Monitor download progress

JOB_ID=$1

while true; do
  STATUS=$(curl -s "http://localhost:8080/api/jobs/$JOB_ID")
  JOB_STATUS=$(echo "$STATUS" | jq -r '.status')

  if [ "$JOB_STATUS" = "completed" ] || [ "$JOB_STATUS" = "failed" ]; then
    echo "Job $JOB_STATUS"
    break
  fi

  DOWNLOADED=$(echo "$STATUS" | jq '.progress.downloadedBytes')
  TOTAL=$(echo "$STATUS" | jq '.progress.totalBytes')

  if [ "$TOTAL" -gt 0 ]; then
    PCT=$(echo "scale=1; $DOWNLOADED * 100 / $TOTAL" | bc)
    echo "Progress: $PCT%"
  fi

  sleep 2
done
```

### Integration with jq

```bash
# Get all running jobs
curl -s http://localhost:8080/api/jobs | jq '.jobs[] | select(.status == "running")'

# Get total download speed
curl -s http://localhost:8080/api/jobs | jq '[.jobs[].progress.bytesPerSecond] | add'

# List repos being downloaded
curl -s http://localhost:8080/api/jobs | jq -r '.jobs[].repo'
```

---

## CORS

Browser requests are checked against an origin policy, for the REST API and
the WebSocket alike:

- **No `Origin` header** (curl, scripts, the CLI): allowed.
- **Same origin** (the web UI served by this server): allowed.
- **Any other origin**: rejected with `403 Forbidden` before the request is
  handled, unless allowed with `serve --allow-origin`. Rejecting (rather than
  only omitting CORS headers) matters because browsers still *send* simple
  cross-site POST/DELETE requests; CORS only hides the response.

### Headers

Sent for allowed requests that carry an `Origin` header:

| Header | Value |
|--------|-------|
| `Access-Control-Allow-Origin` | The request's origin (same origin or allowed via `--allow-origin`) |
| `Access-Control-Allow-Methods` | GET, POST, PUT, DELETE, OPTIONS |
| `Access-Control-Allow-Headers` | Content-Type, Authorization |
| `Access-Control-Max-Age` | 86400 |

### Configuration

Allow extra origins, e.g. when a reverse proxy serves the UI under another
host name, with the repeatable `--allow-origin` flag (`*` allows any origin):

```bash
hfdownloader serve --allow-origin https://hfd.example.com
```

---

## Rate Limiting

No rate limiting is implemented. For production deployments, consider using a reverse proxy (nginx, Caddy) with rate limiting.

---

## Security Considerations

1. **Token Protection**: HF token is masked in API responses
2. **Directory Lock**: Output directories cannot be changed via API
3. **Basic Auth**: Optional authentication for all endpoints
4. **Origin policy**: Requests from other websites are rejected (see CORS)
5. **Input Validation**: Repository format and settings values are validated
6. **Size Limits**: Request bodies are capped at 1 MiB; incoming WebSocket
   messages at 512 KB

---

## See Also

- [CLI Reference](CLI.md)
- [Main README](../README.md)
- [GitHub Issues](https://github.com/bodaay/HuggingFaceModelDownloader/issues)
