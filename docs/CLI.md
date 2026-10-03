# HFDownloader CLI Reference

Complete command-line reference for `hfdownloader`.

---

## Table of Contents

- [Installation](#installation)
- [Quick Reference](#quick-reference)
- [Global Flags](#global-flags)
- [Commands](#commands)
  - [download](#download)
  - [serve](#serve)
  - [analyze](#analyze)
  - [list](#list)
  - [info](#info)
  - [rebuild](#rebuild)
  - [export](#export)
  - [mirror](#mirror)
  - [proxy](#proxy)
  - [config](#config)
  - [version](#version)
- [Environment Variables](#environment-variables)
- [Configuration File](#configuration-file)
- [Examples](#examples)
- [Exit Codes](#exit-codes)

---

## Installation

```bash
# One-liner install (Linux/macOS/WSL)
bash <(curl -sSL https://g.bodaay.io/hfd) -i

# Or build from source
git clone https://github.com/bodaay/HuggingFaceModelDownloader
cd HuggingFaceModelDownloader
go build -o hfdownloader ./cmd/hfdownloader
```

---

## Quick Reference

```bash
# Download a model
hfdownloader download owner/model

# Download specific quantizations
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_k_m,q5_k_m

# Download a dataset
hfdownloader download facebook/flores --dataset

# Analyze before downloading
hfdownloader analyze owner/model

# Start web UI
hfdownloader serve

# List downloaded repos
hfdownloader list

# Show repo details
hfdownloader info Mistral-7B
```

---

## Global Flags

These flags work with all commands:

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--token` | `-t` | string | `$HF_TOKEN` | HuggingFace access token |
| `--json` | | bool | `false` | Emit machine-readable JSON events |
| `--quiet` | `-q` | bool | `false` | Minimal output |
| `--verbose` | `-v` | bool | `false` | Debug output |
| `--config` | | string | | Path to config file (JSON/YAML) |
| `--log-file` | | string | | Write logs to file |
| `--log-level` | | string | `info` | Log level: debug, info, warn, error |
| `--version` | | bool | `false` | Print the version (root command only) |

### Authentication

```bash
# Via flag
hfdownloader download meta-llama/Llama-2-7b -t hf_xxxxx

# Via environment variable (recommended)
export HF_TOKEN=hf_xxxxx
hfdownloader download meta-llama/Llama-2-7b
```

---

## Commands

### download

Download models or datasets from HuggingFace Hub.

```
hfdownloader download [REPO] [flags]
```

The `download` subcommand is required: `hfdownloader owner/name` fails with
`unknown command`. Running `hfdownloader` with no arguments prints help.

#### Repository Selection

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--repo` | `-r` | string | | Repository ID (owner/name) |
| `--dataset` | | bool | `false` | Treat as dataset |
| `--revision` | `-b` | string | `main` | Branch, tag, or commit |
| `--filters` | `-F` | strings | | Comma-separated LFS filters |
| `--exclude` | `-E` | strings | | Patterns to exclude |
| `--exact` | | bool | `false` | Match filters against whole name segments, not substrings (e.g. `-F q6_k` matches `Q6_K` but not `Q6_K_XL`) |
| `--append-filter-subdir` | | bool | `false` | Create subdirs per filter |

#### Performance

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--connections` | `-c` | int | `8` | Connections per file |
| `--max-active` | | int | `3` | Max concurrent downloads |
| `--multipart-threshold` | | string | `32MiB` | Min size for multipart |

#### Reliability

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--retries` | | int | `4` | Retry attempts (the count resets whenever an attempt makes progress) |
| `--backoff-initial` | | string | `400ms` | Initial retry delay |
| `--backoff-max` | | string | `10s` | Max retry delay |
| `--stall-timeout` | | string | `60s` | Retry a transfer that receives no data for this long (`0` disables) |
| `--link-mode` | | string | `auto` | How cache entries refer to downloaded data: `auto` (symlink, else hardlink, else copy), `symlink`, `hardlink`, `copy` |
| `--shards` | | string | | Only these shards of split files, e.g. `1-100` or `1-50,120-185` — download a large model in batches (files that aren't split are unaffected) |
| `--verify` | | string | `size` | Verification for files **without** a known SHA256: `none`, `size`, `etag`, `sha256` (see below) |
| `--stale-timeout` | | string | `5m` | Timeout for stale downloads |

#### Output

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | `~/.cache/huggingface` (or `$HF_HOME`) | HF cache directory (default layout) |
| `--local-dir` | | string | | Download real files (not HF cache symlinks) under this directory, in `<dir>/<owner>/<name>/` |
| `--endpoint` | | string | `https://huggingface.co` | Custom endpoint (mirrors) |
| `--no-manifest` | | bool | `false` | Don't write hfd.yaml manifest |
| `--no-friendly` | | bool | `false` | Don't create friendly symlinks |
| `--dry-run` | | bool | `false` | Plan only, no download |
| `--plan-format` | | string | `table` | Plan format: table, json (`--json` also prints JSON) |

**Verification.** Every file whose SHA256 is known in advance (all LFS files,
which includes every multipart download) is always SHA256-verified, whatever
`--verify` says — so `--verify none` only skips checks for small non-LFS files.
For those files, `size` checks the byte count, `etag`/`sha256` fetch the
server's hash and compare it (falling back to a size check if the server
exposes none), and `none` skips verification.

#### Proxy

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--proxy` | `-x` | string | | Proxy URL (http://, https://, socks5://) |
| `--proxy-user` | | string | | Proxy authentication username |
| `--proxy-pass` | | string | | Proxy authentication password |
| `--no-env-proxy` | | bool | `false` | Ignore HTTP_PROXY/HTTPS_PROXY env vars |

#### Flat-file output (v2.x compatibility)

Use these when you want real files at a user-specified path instead of the
HF cache's blobs + symlink layout. `--local-dir` is the preferred, non-legacy
name; `--legacy -o <dir>` continues to work for v2.x users and is not going
away.

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--local-dir` | | string | | Flat-file mode; download real files under this directory |
| `--legacy` | | bool | `false` | Enable v2.x flat directory structure (defaults to `Models/` or `Datasets/`) |
| `--output` | `-o` | string | | Output directory for `--legacy` mode (requires `--legacy`) |

`--local-dir <path>` and `--legacy -o <path>` are equivalent. They are
mutually exclusive on a single command line.

**Layout differs from `huggingface-cli`:** files are written to
`<path>/<owner>/<name>/`, not directly into `<path>`. For example
`--local-dir ./x` for `TheBloke/Mistral-7B-Instruct-v0.2-GGUF` writes
`./x/TheBloke/Mistral-7B-Instruct-v0.2-GGUF/<files>`. (To put a repo's files
directly into a folder of your choice, download to the cache and use
[`export`](#export).)

#### Examples

```bash
# Basic download
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF

# Filter syntax in repo name
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_k_m,q5_k_m

# Or use --filters flag
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF -F q4_k_m,q5_k_m

# Exclude files
hfdownloader download owner/repo -E ".md,.txt,fp16"

# Specific branch
hfdownloader download CompVis/stable-diffusion-v1-4 -b fp16

# Download dataset
hfdownloader download facebook/flores --dataset

# High-speed download
hfdownloader download owner/repo -c 16 --max-active 4

# Dry run (preview files)
hfdownloader download owner/repo --dry-run
hfdownloader download owner/repo --dry-run --plan-format json

# Use mirror (e.g., for China)
hfdownloader download owner/repo --endpoint https://hf-mirror.com

# Private/gated models
hfdownloader download meta-llama/Llama-2-7b -t hf_xxxxx

# Strict verification
hfdownloader download owner/repo --verify sha256

# Download via proxy
hfdownloader download owner/repo --proxy http://proxy:8080

# Download via authenticated SOCKS5 proxy
hfdownloader download owner/repo --proxy socks5://localhost:1080 \
  --proxy-user myuser --proxy-pass mypassword

# Put real files (not HF cache symlinks) under a directory of your choice
# (files land in ./my-model/owner/repo/)
hfdownloader download owner/repo --local-dir ./my-model
# Equivalent v2.x form (still supported)
hfdownloader download owner/repo --legacy -o ./my-model
```

---

### serve

Start HTTP server with Web UI, REST API, and WebSocket support.

```
hfdownloader serve [flags]
```

#### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--addr` | | string | `0.0.0.0` | Bind address |
| `--port` | `-p` | int | `8080` | Port |
| `--cache-dir` | | string | `~/.cache/huggingface` | Cache directory |
| `--local-dir` | | string | | Put the whole server in flat-file mode: every download writes real files into `<local-dir>/<owner>/<name>` instead of the HF cache |
| `--connections` | `-c` | int | `8` | Connections per file |
| `--max-active` | | int | `3` | Max concurrent file downloads (per job) |
| `--multipart-threshold` | | string | `32MiB` | Min size for multipart |
| `--verify` | | string | `size` | Verification mode for files without a known SHA256: `none`, `size`, `etag`, `sha256` |
| `--retries` | | int | `4` | Retry attempts (the count resets whenever an attempt makes progress) |
| `--endpoint` | | string | | Custom HF endpoint |
| `--auth-user` | | string | | Basic auth username (must be used with `--auth-pass`) |
| `--auth-pass` | | string | | Basic auth password (must be used with `--auth-user`) |
| `--allow-origin` | | strings | | Extra browser origin allowed to call the API (repeatable; `*` allows any). Other origins get 403 |
| `--export-dir` | | string | | Enable "Export as real files" in the web UI, writing to `<export-dir>/<owner>/<name>` |
| `--link-mode` | | string | config `link-mode`, else `auto` | Link mode for downloads: `auto`, `symlink`, `hardlink`, `copy` (see `download`) |
| `--models-dir` | | string | `./Models` | Legacy models directory |
| `--datasets-dir` | | string | `./Datasets` | Legacy datasets directory |

Flags not given on the command line fall back to the config file
(`~/.config/hfdownloader.json`/`.yaml`), then to the defaults above. The server
runs one download job (repo) at a time; additional jobs wait as `queued`.

#### Examples

```bash
# Start with defaults (port 8080)
hfdownloader serve

# Custom port
hfdownloader serve --port 3000

# With authentication
hfdownloader serve --auth-user admin --auth-pass secret123

# With HuggingFace token
hfdownloader serve -t hf_xxxxx

# Use mirror
hfdownloader serve --endpoint https://hf-mirror.com

# High-performance settings
hfdownloader serve -c 16 --max-active 8
```

#### Server Features

- **Web UI** at `http://localhost:8080`
  - Analyze page: auto-detect model type, show files/sizes
  - Jobs page: real-time download progress
  - Cache browser: view downloaded repos
- **REST API** at `/api/*` endpoints
- **WebSocket** at `/api/ws` for live updates

---

### analyze

Analyze a HuggingFace repository without downloading. Auto-detects whether it's a model or dataset.

```
hfdownloader analyze <repo> [flags]
```

#### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--interactive` | `-i` | bool | `false` | Launch interactive TUI to pick files/quantizations to download |
| `--dataset` | | bool | `false` | Analyze as a dataset repository |
| `--revision` | `-b` | string | `main` | Branch or revision to analyze |
| `--cache-dir` | | string | `~/.cache/huggingface` | Cache directory used when downloading from the interactive TUI |
| `--endpoint` | | string | | Custom HF endpoint |
| `--format` | | string | `text` | Output: text, json (`--json` also prints JSON) |

#### Auto-Detection

- Tries the model API first, falls back to the dataset API
- If the repo exists as both a model and a dataset, the command stops with
  `repository exists as both model and dataset`; pass `--dataset` to analyze
  the dataset

#### Detected Types

| Type | Detection |
|------|-----------|
| GGUF | `.gguf` files |
| Transformers | `config.json` with model architecture |
| Diffusers | `model_index.json` |
| LoRA | `adapter_config.json` |
| GPTQ | `quantize_config.json` |
| AWQ | AWQ config in `config.json` |
| ONNX | `.onnx` files or `onnx/` directory |
| Audio | Audio-specific configs |
| Vision | Vision-specific configs |
| Multimodal | Multiple modality configs |
| Dataset | Dataset configs |
| Generic | Anything else |

#### Examples

```bash
# Analyze model
hfdownloader analyze TheBloke/Mistral-7B-Instruct-v0.2-GGUF

# Analyze dataset
hfdownloader analyze facebook/flores

# JSON output
hfdownloader analyze owner/repo --format json

# Use mirror
hfdownloader analyze owner/repo --endpoint https://hf-mirror.com
```

#### Sample Output

Output varies by detected type; for a small Transformers repo:

```
Repository: hf-internal-testing/tiny-random-gpt2
Type:       transformers (Transformers model (safetensors))
Files:      10
Total Size: 11.9 MiB

Model Configuration:
  Vocab Size:      1000
  Precision:       fp32

Weight Files:
  NAME                                                   SIZE  FORMAT
  ---------------------------------------------  ------------  ------
  model.safetensors                                 443.2 KiB  safetensors
  pytorch_model.bin                                   3.4 MiB  pytorch_bin
...
Download Commands:
  Full:        hfdownloader download hf-internal-testing/tiny-random-gpt2
  Recommended: hfdownloader download hf-internal-testing/tiny-random-gpt2 -F safetensors --exact
```

---

### list

List downloaded models and datasets.

```
hfdownloader list [flags]
```

#### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | `~/.cache/huggingface` | Cache directory |
| `--type` | | string | | Filter: model, dataset |
| `--sort` | | string | `name` | Sort: name, size, date |
| `--format` | | string | `table` | Output: table, json |
| `--scan` | | bool | `false` | Scan cache structure instead of reading `hfd.yaml` manifests (includes repos downloaded by other tools) |

#### Examples

```bash
# List all
hfdownloader list

# Models only
hfdownloader list --type model

# Sort by size
hfdownloader list --sort size

# JSON output
hfdownloader list --format json

# Scan cache structure
hfdownloader list --scan
```

#### Sample Output

Sizes are shown in binary units (B, KiB, MiB, GiB).

```
TYPE     REPO                                    COMMIT   DOWNLOADED  FILES        SIZE  BRANCH
-------  --------------------------------------  -------  ----------  -----  ----------  ------
model    TheBloke/Mistral-7B-Instruct-v0.2-GGUF  41b61a3  2024-01-15      4     4.1 GiB  main
dataset  facebook/flores                         7d4c3b1  2024-01-10     12   128.0 MiB  main

Total: 2 repos
(Use --scan to include repos without manifests)
```

---

### info

Show detailed information about a downloaded repository.

```
hfdownloader info <repo> [flags]
```

#### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | `~/.cache/huggingface` | Cache directory |
| `--format` | | string | `text` | Output: text, json |

#### Examples

```bash
# Full repo name
hfdownloader info TheBloke/Mistral-7B-Instruct-v0.2-GGUF

# Partial match
hfdownloader info Mistral-7B

# JSON output
hfdownloader info Mistral-7B --format json
```

#### Sample Output

Sizes are shown in binary units (B, KiB, MiB, GiB). A partial name must match
exactly one downloaded repo.

```
Repository: TheBloke/Mistral-7B-Instruct-v0.2-GGUF
Type:       model
Branch:     main
Commit:     41b61a33a2483885c981aa79e0df6b32407ed873
Files:      3
Size:       4.1 GiB
Downloaded: 2024-01-15 10:30:45

Friendly path: /home/user/.cache/huggingface/models/TheBloke/Mistral-7B-Instruct-v0.2-GGUF
Cache path:    /home/user/.cache/huggingface/hub/models--TheBloke--Mistral-7B-Instruct-v0.2-GGUF

Command: hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF -F q4_k_m

Files:
  NAME                                                      SIZE  LFS
  --------------------------------------------------  ----------  ---
  config.json                                              1.2 KiB
  mistral-7b-instruct-v0.2.Q4_K_M.gguf                     4.1 GiB  yes
  README.md                                                8.5 KiB
```

---

### rebuild

Regenerate friendly view symlinks from hub cache.

```
hfdownloader rebuild [flags]
```

Use after downloading with the official HuggingFace Python library, or after manually modifying the cache. Also repairs caches written by older Windows builds (files only in `blobs/`) by recreating snapshot entries from the download manifest.

#### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | `~/.cache/huggingface` | Cache directory |
| `--clean` | | bool | `false` | Remove orphaned symlinks |
| `--write-script` | | bool | `false` | Write standalone rebuild.sh |

#### Examples

```bash
# Rebuild symlinks
hfdownloader rebuild

# Clean orphaned links
hfdownloader rebuild --clean

# Write standalone script
hfdownloader rebuild --write-script

# Machine-readable result
hfdownloader rebuild --json
```

#### Sample Output

By default `rebuild` prints a text summary:

```
Rebuilding friendly view from: /home/user/.cache/huggingface
Cleaning orphaned symlinks...

Rebuild complete:
  Repos scanned:     5
  Symlinks created:  23
  Symlinks updated:  2
  Orphans removed:   1
```

With the global `--json` flag it prints JSON instead (`orphans_removed`,
`snapshot_entries_repaired` and `errors` are omitted when zero/empty):

```json
{
  "repos_scanned": 5,
  "symlinks_created": 23,
  "symlinks_updated": 2,
  "orphans_removed": 1
}
```

---

### export

Export a downloaded repo as plain files — no re-download.

```
hfdownloader export <repo> <dest> [flags]
```

Writes the repo's files directly into `<dest>` (not `<dest>/<owner>/<name>`)
in the repo's own layout. On the same
drive files are **hardlinked** from the cache (real files, no extra disk
space); otherwise they are copied. Works for caches written without links
(older Windows builds) using the download manifest.

#### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | `~/.cache/huggingface` | Cache directory |
| `--revision` | `-b` | string | `main` | Branch, tag or commit to export |
| `--mode` | | string | `auto` | `auto` (hardlink, else copy), `hardlink`, `copy`, `symlink` |
| `--dataset` | | bool | `false` | The repo is a dataset |
| `--filters` | `-F` | strings | | Export only matching weight/data files (exact match); other files always exported |

#### Examples

```bash
hfdownloader export TheBloke/Mistral-7B-Instruct-v0.2-GGUF ~/lmstudio/models/TheBloke/Mistral-7B
hfdownloader export unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF ./qwen -F q4_k_m
hfdownloader export --dataset nyu-mll/glue ./glue --mode copy
```

---

### mirror

Sync HuggingFace cache between locations.

```
hfdownloader mirror <subcommand> [flags]
```

#### Subcommands

##### mirror target add

Add a named mirror target.

```
hfdownloader mirror target add <name> <path> [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--description` | `-d` | string | | Target description |

```bash
hfdownloader mirror target add office /mnt/nas/hfcache -d "Office NAS"
hfdownloader mirror target add usb /media/usb/hfcache
```

##### mirror target list

List configured targets.

```
hfdownloader mirror target list [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--format` | | string | `table` | Output: table, json |

```bash
hfdownloader mirror target list
```

##### mirror target remove

Remove a mirror target.

```
hfdownloader mirror target remove <name>
```

```bash
hfdownloader mirror target remove usb
```

##### mirror diff

Show differences between local and target.

```
hfdownloader mirror diff <target> [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | config `cache-dir`, else `~/.cache/huggingface` | Local cache |
| `--format` | | string | `table` | Output: table, json |
| `--repo` | | string | | Filter by repo name (case-insensitive substring; one value) |

```bash
hfdownloader mirror diff office
hfdownloader mirror diff office --repo Mistral
```

##### mirror push

Push local repos to target.

```
hfdownloader mirror push <target> [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cache-dir` | | string | config `cache-dir`, else `~/.cache/huggingface` | Local cache |
| `--repo` | | string | | Filter by repo name (case-insensitive substring; one value) |
| `--dry-run` | | bool | `false` | Preview only |
| `--verify` | | bool | `false` | Verify SHA256 after copy |
| `--delete` | | bool | `false` | Delete repos in the target that are not in the local cache |
| `--force` | | bool | `false` | Re-copy incomplete or outdated repos |

There is no `--filter` flag; use `--repo`.

```bash
# Preview
hfdownloader mirror push office --dry-run

# Push all
hfdownloader mirror push office

# Push specific repo
hfdownloader mirror push office --repo Mistral-7B

# With verification
hfdownloader mirror push office --verify
```

##### mirror pull

Pull repos from target to local.

```
hfdownloader mirror pull <target> [flags]
```

Same flags as `mirror push`, except that `--delete` removes **local** repos
that are not in the target.

```bash
hfdownloader mirror pull office
hfdownloader mirror pull office --repo Llama
```

---

### proxy

Manage and test proxy configuration.

```
hfdownloader proxy <subcommand> [flags]
```

#### Subcommands

##### proxy test

Test proxy connectivity by making a test request.

```
hfdownloader proxy test [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--proxy` | `-x` | string | | Proxy URL (required) |
| `--proxy-user` | | string | | Proxy username |
| `--proxy-pass` | | string | | Proxy password |
| `--url` | | string | `https://huggingface.co/api/whoami` | URL to test |
| `--timeout` | | string | `30s` | Connection timeout |

```bash
# Test HTTP proxy
hfdownloader proxy test --proxy http://proxy.corp.com:8080

# Test with authentication
hfdownloader proxy test --proxy http://proxy.corp.com:8080 \
  --proxy-user myuser --proxy-pass mypassword

# Test SOCKS5 proxy
hfdownloader proxy test --proxy socks5://localhost:1080

# Test against specific URL
hfdownloader proxy test --proxy http://proxy:8080 --url https://example.com
```

##### proxy info

Show current proxy configuration from environment variables.

```
hfdownloader proxy info [flags]
```

```bash
# Show proxy info
hfdownloader proxy info

# JSON output
hfdownloader proxy info --json
```

#### Sample Output

```
Proxy Configuration:

Environment Variables:
  HTTP_PROXY:    http://proxy.corp.com:8080
  HTTPS_PROXY:   http://proxy.corp.com:8080
  NO_PROXY:      localhost,.internal.com

Effective Proxy: http://proxy.corp.com:8080
```

#### Using Proxy with Downloads

Proxy settings can be specified via CLI flags, config file, or environment variables.

##### CLI Flags

```bash
# Basic proxy
hfdownloader download meta-llama/Llama-2-7b --proxy http://proxy:8080

# With authentication
hfdownloader download meta-llama/Llama-2-7b \
  --proxy http://proxy:8080 \
  --proxy-user myuser \
  --proxy-pass mypassword

# SOCKS5 proxy
hfdownloader download meta-llama/Llama-2-7b --proxy socks5://localhost:1080

# Ignore environment proxy
hfdownloader download meta-llama/Llama-2-7b --no-env-proxy
```

##### Configuration File

```yaml
# ~/.config/hfdownloader.yaml
proxy:
  url: http://proxy.corp.com:8080
  username: myuser
  password: mypassword
  no_proxy: localhost,.internal.com,10.0.0.0/8
  no_env_proxy: false
  insecure_skip_verify: false
```

##### Environment Variables

| Variable | Description |
|----------|-------------|
| `HTTP_PROXY` / `http_proxy` | Proxy for HTTP requests |
| `HTTPS_PROXY` / `https_proxy` | Proxy for HTTPS requests |
| `ALL_PROXY` / `all_proxy` | Fallback for all protocols |
| `NO_PROXY` / `no_proxy` | Comma-separated bypass list |

##### Supported Proxy Types

| Type | URL Format | Description |
|------|------------|-------------|
| HTTP | `http://host:port` | Standard HTTP proxy |
| HTTPS | `https://host:port` | TLS-encrypted proxy |
| SOCKS5 | `socks5://host:port` | SOCKS5 TCP tunneling |
| SOCKS5h | `socks5h://host:port` | SOCKS5 with remote DNS |

##### NO_PROXY Patterns

The `no_proxy` setting supports:

- Exact hostnames: `localhost`, `myserver.local`
- Domain suffixes: `.internal.com` (matches `*.internal.com`)
- CIDR ranges: `10.0.0.0/8`, `192.168.0.0/16`
- Wildcard: `*` (bypass all)

---

### config

Manage configuration file.

```
hfdownloader config <subcommand>
```

#### Subcommands

##### config init

Create default configuration file.

```
hfdownloader config init [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--force` | `-f` | bool | `false` | Overwrite existing |
| `--yaml` | | bool | `false` | Create YAML instead of JSON |

```bash
hfdownloader config init
hfdownloader config init --yaml
hfdownloader config init -f
```

##### config show

Display current configuration.

```bash
hfdownloader config show
```

##### config path

Print configuration file path.

```bash
hfdownloader config path
# Output: /home/user/.config/hfdownloader.json
```

Prints the first of `hfdownloader.json`, `.yaml`, `.yml` that exists in
`~/.config/`, or the JSON path if none does.

#### Default Configuration

Written by `config init`:

```json
{
  "backoff-initial": "400ms",
  "backoff-max": "10s",
  "cache-dir": "",
  "connections": 8,
  "link-mode": "auto",
  "max-active": 3,
  "multipart-threshold": "32MiB",
  "retries": 4,
  "stall-timeout": "60s",
  "token": "",
  "verify": "size"
}
```

---

### version

Show version information.

```
hfdownloader version [flags]
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--short` | `-s` | bool | `false` | Version number only |

```bash
hfdownloader version
# hfdownloader 3.3.0
#   Go:       go1.25.0
#   OS/Arch:  darwin/arm64
#   Commit:   abc123
#   Built:    2024-01-15T10:30:00Z

hfdownloader version -s
# 3.3.0
```

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `HF_TOKEN` | HuggingFace access token |
| `HF_HOME` | Override `~/.cache/huggingface` root |
| `HF_HUB_CACHE` | Override just the `hub/` directory |
| `HTTP_PROXY` / `http_proxy` | Proxy for HTTP requests |
| `HTTPS_PROXY` / `https_proxy` | Proxy for HTTPS requests |
| `ALL_PROXY` / `all_proxy` | Fallback proxy for all protocols |
| `NO_PROXY` / `no_proxy` | Comma-separated proxy bypass list |

```bash
export HF_TOKEN=hf_xxxxx
export HF_HOME=/mnt/data/huggingface

# Proxy configuration
export HTTPS_PROXY=http://proxy.corp.com:8080
export NO_PROXY=localhost,.internal.com
```

---

## Configuration File

Configuration is loaded from the first of:
1. `--config` flag path (honored by `download` only)
2. `~/.config/hfdownloader.json`
3. `~/.config/hfdownloader.yaml`
4. `~/.config/hfdownloader.yml`

Command-line flags always win over the file. Besides the keys below, the file
may hold `cache-dir`, `stall-timeout`, `link-mode` and (for `serve`)
`export-dir`. Saving settings in the web UI rewrites only the keys it manages
(`token`, `connections`, `max-active`, `multipart-threshold`, `verify`,
`retries`, `endpoint`, `proxy`) and keeps everything else.

### JSON Format

```json
{
  "token": "hf_xxxxx",
  "connections": 8,
  "max-active": 3,
  "multipart-threshold": "32MiB",
  "verify": "size",
  "retries": 4,
  "backoff-initial": "400ms",
  "backoff-max": "10s",
  "endpoint": "",
  "proxy": {
    "url": "http://proxy.corp.com:8080",
    "username": "myuser",
    "password": "mypassword",
    "no_proxy": "localhost,.internal.com",
    "no_env_proxy": false,
    "insecure_skip_verify": false
  }
}
```

### YAML Format

```yaml
token: hf_xxxxx
connections: 8
max-active: 3
multipart-threshold: 32MiB
verify: size
retries: 4
backoff-initial: 400ms
backoff-max: 10s
endpoint: ""

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

## Examples

### Download Workflows

```bash
# 1. Analyze → Select → Download
hfdownloader analyze TheBloke/Mistral-7B-Instruct-v0.2-GGUF
hfdownloader download TheBloke/Mistral-7B-Instruct-v0.2-GGUF:q4_k_m

# 2. Download entire model
hfdownloader download meta-llama/Llama-3-8B-Instruct

# 3. Download with filters and excludes
hfdownloader download owner/repo -F safetensors -E ".md,fp16"

# 4. High-speed download
hfdownloader download owner/repo -c 16 --max-active 8

# 5. Resume interrupted download
hfdownloader download owner/repo  # Automatically resumes
```

### Server Workflows

```bash
# 1. Basic server
hfdownloader serve

# 2. Production server
hfdownloader serve \
  --port 8080 \
  --auth-user admin \
  --auth-pass secure123 \
  -t hf_xxxxx

# 3. Mirror server
hfdownloader serve --endpoint https://hf-mirror.com
```

### Cache Management

```bash
# View downloads
hfdownloader list --sort size

# Get details
hfdownloader info Mistral-7B

# Rebuild after Python downloads
hfdownloader rebuild --clean

# Mirror to backup
hfdownloader mirror target add backup /mnt/backup/hf
hfdownloader mirror push backup --verify
```

### CI/CD Integration

```bash
# JSON output for parsing
hfdownloader download owner/repo --json 2>&1 | jq '.event'

# Dry run for planning
hfdownloader download owner/repo --dry-run --plan-format json

# Quiet mode for scripts
hfdownloader download owner/repo -q
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (also: bare `hfdownloader` printing help, `serve` shut down with Ctrl+C) |
| 1 | Any error — including invalid arguments/flags, unknown commands, failed downloads, and a download interrupted with Ctrl+C |

The error is printed to stderr as `error: <message>`.

---

## Signal Handling

- `SIGINT` (Ctrl+C): Stops the download (`error: context canceled`, exit code 1);
  partially downloaded data is kept for resuming. `serve` shuts down gracefully
  and exits 0.
- `SIGTERM`: Same as SIGINT

Interrupted downloads can be resumed by running the same command again.

---

## See Also

- [V3 Features Documentation](V3_FEATURES.md)
- [REST API Documentation](API.md)
- [Main README](../README.md)
- [GitHub Issues](https://github.com/bodaay/HuggingFaceModelDownloader/issues)
