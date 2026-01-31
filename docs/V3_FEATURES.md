# HFDownloader v3.0 - New Features

This document covers the major new features introduced in HFDownloader v3.0.

---

## Table of Contents

- [HuggingFace Cache Compatibility](#huggingface-cache-compatibility)
- [Proxy Support](#proxy-support)
- [Mirror System](#mirror-system)
- [Smart Repository Analyzer](#smart-repository-analyzer)
- [Web UI Dashboard](#web-ui-dashboard)
- [Cache Management Commands](#cache-management-commands)

---

## HuggingFace Cache Compatibility

v3.0 adopts the official HuggingFace Hub cache structure as the default storage method, enabling full interoperability with Python libraries (transformers, diffusers, datasets, etc.).

### Cache Structure

```
~/.cache/huggingface/
├── hub/                                    # HF Cache (Source of Truth)
│   └── models--TheBloke--Mistral-7B-GGUF/
│       ├── refs/
│       │   └── main                        # Contains commit hash
│       ├── blobs/
│       │   └── <sha256>                    # Actual file content
│       └── snapshots/
│           └── <commit>/                   # Symlinks to blobs
│               └── model.gguf -> ../../blobs/<sha256>
│
├── models/                                 # Friendly View (Symlinks)
│   └── TheBloke/
│       └── Mistral-7B-GGUF/
│           └── model.gguf -> hub/.../snapshots/.../model.gguf
│
└── datasets/                               # Friendly View for datasets
```

### Benefits

- **Python Compatibility**: Models work directly with `transformers.AutoModel.from_pretrained()`
- **Blob Deduplication**: Same file content shared across revisions
- **Human-Readable Paths**: Browse models at `~/.cache/huggingface/models/`

### Environment Variables

| Variable | Purpose |
|----------|---------|
| `HF_HOME` | Override `~/.cache/huggingface` root |
| `HF_HUB_CACHE` | Override `hub/` directory specifically |
| `HF_TOKEN` | Authentication token |

---

## Proxy Support

Full proxy support with authentication for environments behind corporate firewalls, VPNs, or requiring traffic routing through specific servers.

### Supported Proxy Types

| Type | URL Format | Description |
|------|------------|-------------|
| HTTP | `http://host:port` | Standard HTTP proxy |
| HTTPS | `https://host:port` | TLS-encrypted proxy connection |
| SOCKS5 | `socks5://host:port` | SOCKS5 proxy (TCP tunneling) |
| SOCKS5h | `socks5h://host:port` | SOCKS5 with DNS resolution via proxy |

### CLI Usage

```bash
# Basic proxy
hfdownloader download meta-llama/Llama-2-7b --proxy http://proxy.corp.com:8080

# Proxy with authentication
hfdownloader download meta-llama/Llama-2-7b \
  --proxy http://proxy.corp.com:8080 \
  --proxy-user myuser \
  --proxy-pass mypassword

# SOCKS5 proxy (e.g., SSH tunnel)
hfdownloader download meta-llama/Llama-2-7b --proxy socks5://localhost:1080

# Disable environment proxy variables
hfdownloader download meta-llama/Llama-2-7b --no-env-proxy
```

### CLI Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--proxy` | `-x` | Proxy URL (http://, https://, socks5://) |
| `--proxy-user` | | Proxy authentication username |
| `--proxy-pass` | | Proxy authentication password |
| `--no-env-proxy` | | Ignore HTTP_PROXY/HTTPS_PROXY environment variables |

### Configuration File

Add proxy settings to `~/.config/hfdownloader.yaml`:

```yaml
proxy:
  url: http://proxy.corp.com:8080
  username: myuser
  password: mypassword
  no_proxy: localhost,.internal.com,10.0.0.0/8
  no_env_proxy: false
  insecure_skip_verify: false  # Only for testing!
```

Or in JSON format (`~/.config/hfdownloader.json`):

```json
{
  "proxy": {
    "url": "http://proxy.corp.com:8080",
    "username": "myuser",
    "password": "mypassword",
    "no_proxy": "localhost,.internal.com,10.0.0.0/8"
  }
}
```

### Proxy Bypass (NO_PROXY)

The `no_proxy` setting supports:

- **Exact hostnames**: `localhost`, `myserver.local`
- **Domain suffixes**: `.internal.com` (matches `*.internal.com`)
- **CIDR ranges**: `10.0.0.0/8`, `192.168.0.0/16`
- **Wildcard**: `*` (bypass all - direct connection)

### Proxy Commands

```bash
# Test proxy connectivity
hfdownloader proxy test --proxy http://proxy:8080
hfdownloader proxy test --proxy socks5://localhost:1080 --proxy-user user --proxy-pass pass

# Show current proxy configuration
hfdownloader proxy info
hfdownloader proxy info --json
```

### Environment Variables

Standard proxy environment variables are respected (unless `--no-env-proxy` is set):

| Variable | Purpose |
|----------|---------|
| `HTTP_PROXY` / `http_proxy` | Proxy for HTTP requests |
| `HTTPS_PROXY` / `https_proxy` | Proxy for HTTPS requests |
| `ALL_PROXY` / `all_proxy` | Fallback proxy for all protocols |
| `NO_PROXY` / `no_proxy` | Comma-separated bypass list |

### Priority Order

1. CLI flags (`--proxy`, `--proxy-user`, etc.)
2. Configuration file (`~/.config/hfdownloader.yaml`)
3. Environment variables (`HTTP_PROXY`, etc.)

---

## Mirror System

Sync your HuggingFace cache between locations - perfect for air-gapped environments, NAS backups, or sharing models across machines.

### Target Management

```bash
# Add a mirror target
hfdownloader mirror target add NAS /mnt/nas/hf-models
hfdownloader mirror target add USB /media/usb/hf-cache
hfdownloader mirror target add Office ssh://server.office.com:/data/models

# List configured targets
hfdownloader mirror target list

# Remove a target
hfdownloader mirror target remove NAS
```

### Sync Operations

```bash
# Compare local cache with target (dry-run)
hfdownloader mirror diff NAS

# Push local cache to target
hfdownloader mirror push NAS

# Push specific repos only
hfdownloader mirror push NAS --filter "Llama,GGUF"

# Pull from target to local cache
hfdownloader mirror pull NAS

# Sync with verification
hfdownloader mirror push NAS --verify

# Delete files on target that don't exist locally
hfdownloader mirror push NAS --delete
```

### Configuration

Targets are stored in `~/.config/hfdownloader/targets.yaml`:

```yaml
targets:
  NAS:
    path: /mnt/nas/hf-models
  USB:
    path: /media/usb/hf-cache
  Office:
    path: ssh://server.office.com:/data/models
```

---

## Smart Repository Analyzer

Analyze HuggingFace repositories before downloading to understand their structure, quantization options, and make informed decisions.

### Usage

```bash
# Analyze a repository
hfdownloader analyze TheBloke/Llama-2-7B-GGUF

# JSON output for scripting
hfdownloader analyze TheBloke/Llama-2-7B-GGUF --json
```

### Output Example

```
Repository: TheBloke/Llama-2-7B-GGUF
Type: model
Total Size: 45.2 GB (23 files)

Quantizations Available:
  Q2_K    2.8 GB  (smallest, lowest quality)
  Q3_K_S  3.2 GB
  Q3_K_M  3.5 GB
  Q4_0    4.0 GB
  Q4_K_S  4.2 GB
  Q4_K_M  4.4 GB  (recommended balance)
  Q5_0    4.8 GB
  Q5_K_S  5.0 GB
  Q5_K_M  5.2 GB
  Q6_K    5.8 GB
  Q8_0    7.2 GB  (highest quality)

Suggested Commands:
  # Download Q4_K_M (recommended):
  hfdownloader download TheBloke/Llama-2-7B-GGUF -F q4_k_m

  # Download all Q4 variants:
  hfdownloader download TheBloke/Llama-2-7B-GGUF -F q4
```

---

## Web UI Dashboard

A modern web interface for managing downloads, browsing cache, and configuring settings.

### Starting the Server

```bash
# Start with default settings (port 8080)
hfdownloader serve

# Custom port and address
hfdownloader serve --port 9000 --addr 0.0.0.0

# With authentication
hfdownloader serve --auth-user admin --auth-pass secret

# Custom CORS origins
hfdownloader serve --cors "http://localhost:3000,https://myapp.com"
```

### Features

- **Download Queue**: Start, pause, resume, and cancel downloads
- **Real-time Progress**: WebSocket-powered live updates
- **Cache Browser**: View and manage downloaded models/datasets
- **Mirror Management**: Configure targets and sync operations
- **Settings**: Configure all download options via UI

### API Endpoints

See [API.md](./API.md) for the complete REST API documentation.

---

## Cache Management Commands

### List Downloaded Repos

```bash
# List all repos in cache (from manifests)
hfdownloader list

# Scan cache structure (for repos without manifests)
hfdownloader list --scan

# Filter by type
hfdownloader list --type model
hfdownloader list --type dataset

# Sort options
hfdownloader list --sort size   # Largest first
hfdownloader list --sort date   # Newest first
hfdownloader list --sort name   # Alphabetical

# JSON output
hfdownloader list --json
```

### Repository Info

```bash
# Get detailed info about a cached repo
hfdownloader info meta-llama/Llama-2-7b

# JSON output
hfdownloader info meta-llama/Llama-2-7b --json
```

### Rebuild Cache

```bash
# Rebuild friendly view symlinks
hfdownloader rebuild

# Rebuild for specific repo
hfdownloader rebuild meta-llama/Llama-2-7b
```

---

## Upgrade Guide

### From v2.x to v3.0

1. **Default behavior changed**: v3.0 uses HF cache structure by default
2. **Legacy mode available**: Use `--legacy` for v2.x flat directory structure
3. **Output flag deprecated**: `--output` now requires `--legacy`

```bash
# v2.x behavior (flat structure)
hfdownloader download meta-llama/Llama-2-7b --legacy -o ./my-models

# v3.0 behavior (HF cache structure)
hfdownloader download meta-llama/Llama-2-7b
```

### Migration

Existing downloads in v2.x format will continue to work. To migrate to the new cache structure:

1. Re-download models with v3.0 (they'll use HF cache)
2. Or manually move files and run `hfdownloader rebuild`

---

## Configuration Reference

### Complete Config File Example

```yaml
# ~/.config/hfdownloader.yaml

# Authentication
token: hf_xxxxxxxxxxxxxxxxxxxxx

# Download settings
connections: 8           # Concurrent connections per file
max-active: 3           # Max files downloading at once
multipart-threshold: 32MiB
verify: size            # none, size, etag, sha256
retries: 4
backoff-initial: 400ms
backoff-max: 10s

# Custom endpoint (e.g., mirror)
endpoint: https://hf-mirror.com

# Proxy configuration
proxy:
  url: http://proxy.corp.com:8080
  username: myuser
  password: mypassword
  no_proxy: localhost,.internal.com
  no_env_proxy: false
  insecure_skip_verify: false
```

---

*Document Version: 3.0.0*
*Last Updated: January 2025*
