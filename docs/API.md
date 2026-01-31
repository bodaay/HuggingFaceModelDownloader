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

- Request bodies: `application/json`
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
  "version": "3.0.0",
  "time": "2024-01-15T10:30:00Z"
}
```

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
| `appendFilterSubdir` | boolean | No | `false` | Create filter subdirs |
| `dryRun` | boolean | No | `false` | Plan only |

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
  "isDataset": false,
  "filters": ["q4_k_m"],
  "excludes": [],
  "outputDir": "/home/user/.cache/huggingface/hub",
  "status": "queued",
  "progress": {
    "totalFiles": 0,
    "completedFiles": 0,
    "totalBytes": 0,
    "downloadedBytes": 0,
    "bytesPerSecond": 0
  },
  "error": "",
  "createdAt": "2024-01-15T10:30:00Z",
  "startedAt": null,
  "endedAt": null,
  "files": []
}
```

**Response** `200 OK` (Existing job)

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

Same as `/api/download`.

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
      "isDataset": false,
      "filters": ["q4_k_m"],
      "excludes": [],
      "outputDir": "/home/user/.cache/huggingface/hub",
      "status": "running",
      "progress": {
        "totalFiles": 3,
        "completedFiles": 1,
        "totalBytes": 4500000000,
        "downloadedBytes": 1500000000,
        "bytesPerSecond": 50000000
      },
      "error": "",
      "createdAt": "2024-01-15T10:30:00Z",
      "startedAt": "2024-01-15T10:30:01Z",
      "endedAt": null,
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
          "status": "downloading"
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

Cancel a running or queued job.

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

**Note**: Resumed jobs restart from `queued` status. Progress is reset, but already-downloaded files are automatically skipped.

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

---

### Settings

#### GET /api/settings

Get current server settings.

**Response** `200 OK`

```json
{
  "token": "********mnop",
  "cacheDir": "/home/user/.cache/huggingface/hub",
  "connections": 8,
  "maxActive": 3,
  "multipartThreshold": "32MiB",
  "verify": "size",
  "retries": 4,
  "endpoint": ""
}
```

**Note**: Token is masked for security (shows `********` + last 4 characters).

**Example**

```bash
curl http://localhost:8080/api/settings
```

---

#### POST /api/settings

Update server settings.

**Request Body**

| Field | Type | Description |
|-------|------|-------------|
| `token` | string | HuggingFace token |
| `connections` | integer | Connections per file |
| `maxActive` | integer | Max concurrent downloads |
| `multipartThreshold` | string | Min size for multipart |
| `verify` | string | Verification: none, size, sha256 |
| `retries` | integer | Retry attempts |

**Security Restrictions**
- `cacheDir` cannot be changed via API
- `modelsDir` cannot be changed via API
- `datasetsDir` cannot be changed via API

**Response** `200 OK`

```json
{
  "success": true,
  "message": "Settings updated"
}
```

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
  "branch": "main",
  "refs": [
    {"name": "main", "type": "branch", "commit": "abc123..."}
  ],
  "files": [
    {
      "name": "config.json",
      "path": "config.json",
      "size": 1024,
      "lfs": false
    },
    {
      "name": "mistral-7b.Q4_K_M.gguf",
      "path": "mistral-7b.Q4_K_M.gguf",
      "size": 4368438272,
      "lfs": true,
      "sha256": "abc123..."
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

**Response** `200 OK` (Needs selection)

When a repo exists as both model and dataset:

```json
{
  "needsSelection": true,
  "repo": "owner/name",
  "message": "This repository exists as both a model and a dataset. Please select which one you want to analyze.",
  "options": ["model", "dataset"]
}
```

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
| `gptq` | GPTQ quantized | `gptq.bits` |
| `awq` | AWQ quantized | `awq.bits` |
| `onnx` | ONNX model | `onnx.models` |
| `audio` | Audio model | `audio.task` |
| `vision` | Vision model | `vision.task` |
| `multimodal` | Multimodal model | `multimodal.modalities` |
| `dataset` | Dataset | `dataset.formats` |

---

### Cache

#### GET /api/cache

List cached repositories.

**Query Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `type` | string | Filter: model, dataset |

**Response** `200 OK`

```json
{
  "repos": [
    {
      "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
      "type": "model",
      "path": "/home/user/.cache/huggingface/hub/models--TheBloke--Mistral-7B-GGUF"
    },
    {
      "repo": "facebook/flores",
      "type": "dataset",
      "path": "/home/user/.cache/huggingface/hub/datasets--facebook--flores"
    }
  ],
  "count": 2,
  "cacheDir": "/home/user/.cache/huggingface/hub"
}
```

**Examples**

```bash
# List all
curl http://localhost:8080/api/cache

# Models only
curl "http://localhost:8080/api/cache?type=model"
```

---

#### GET /api/cache/{repo}

Get cached repository details.

**Path Parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `repo` | string | Repository ID (owner/name) |

**Response** `200 OK`

```json
{
  "repo": "TheBloke/Mistral-7B-Instruct-v0.2-GGUF",
  "type": "model",
  "path": "/home/user/.cache/huggingface/hub/models--TheBloke--Mistral-7B-GGUF",
  "snapshots": ["main", "v1.0"]
}
```

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
    "version": "3.0.0"
  }
}
```

#### job_update

Broadcast when job status changes.

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
        "status": "downloading"
      }
    ]
  }
}
```

#### event

General event notifications.

```json
{
  "type": "event",
  "data": {
    "event": "download_complete",
    "jobId": "a1b2c3d4e5f6"
  }
}
```

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
| 200 | OK | Successful GET, existing job |
| 202 | Accepted | New job created |
| 204 | No Content | CORS preflight |
| 400 | Bad Request | Invalid input |
| 401 | Unauthorized | Auth required/failed |
| 404 | Not Found | Resource not found |
| 500 | Server Error | Internal error |

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

**Analysis Failed**
```json
{
  "error": "Analysis failed",
  "details": "404: repository not found"
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

### Headers

| Header | Value |
|--------|-------|
| `Access-Control-Allow-Origin` | Configured origins or `*` |
| `Access-Control-Allow-Methods` | GET, POST, PUT, DELETE, OPTIONS |
| `Access-Control-Allow-Headers` | Content-Type, Authorization |
| `Access-Control-Max-Age` | 86400 |

### Configuration

CORS origins are configured via server settings (code modification required for custom origins).

---

## Rate Limiting

No rate limiting is implemented. For production deployments, consider using a reverse proxy (nginx, Caddy) with rate limiting.

---

## Security Considerations

1. **Token Protection**: HF token is masked in API responses
2. **Directory Lock**: Output directories cannot be changed via API
3. **Basic Auth**: Optional authentication for all endpoints
4. **CORS**: Configurable origin restrictions
5. **Input Validation**: Repository format validation
6. **File Size Limits**: WebSocket messages limited to 512 KB

---

## See Also

- [CLI Reference](CLI.md)
- [Main README](../README.md)
- [GitHub Issues](https://github.com/bodaay/HuggingFaceModelDownloader/issues)
