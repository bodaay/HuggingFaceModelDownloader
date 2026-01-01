# Release Notes - v2.3.3

> **Release Date:** December 31, 2025

## 🎉 Community Features

This release incorporates contributions and suggestions from the community:

### ✨ New Features

#### 🚫 Exclude Patterns (`--exclude`, `-E`)
Exclude specific files from downloads. Thanks to **@jeroenkroese** ([#41](https://github.com/bodaay/HuggingFaceModelDownloader/pull/41))!

```bash
# Exclude markdown and ONNX files
hfdownloader download TheBloke/Mistral-7B-GGUF -E .md,onnx

# Exclude full precision models
hfdownloader download owner/repo -E fp16,fp32
```

#### 🌍 Custom Endpoint (`--endpoint`)
Use HuggingFace mirrors or enterprise endpoints. Thanks to **@windtail** ([#38](https://github.com/bodaay/HuggingFaceModelDownloader/pull/38))!

```bash
# Use China mirror
hfdownloader download owner/repo --endpoint https://hf-mirror.com

# Use enterprise endpoint
hfdownloader serve --endpoint https://your-enterprise.com/hf
```

#### 🐳 Docker Support
Run hfdownloader in containers. Thanks to **@cdeving** ([#50](https://github.com/bodaay/HuggingFaceModelDownloader/pull/50))!

```bash
# Build Docker image
docker build -t hfdownloader .

# Run CLI in container
docker run --rm -v ./models:/data hfdownloader download TheBloke/Mistral-7B-GGUF -o /data

# Run web server in container
docker run --rm -p 8080:8080 -v ./models:/data hfdownloader serve --models-dir /data/Models
```

---

## 📋 Changes

- Added `--exclude` / `-E` flag to CLI download command
- Added `--endpoint` flag to both download and serve commands
- Added `Excludes` field to Job struct
- Added `Endpoint` field to Settings struct
- Created `Dockerfile` with multi-stage build
- Updated API to support `excludes` in download requests
- Updated settings endpoint to include custom endpoint config
- **Web UI:** Added "Exclude" input field to Model and Dataset download forms
- **Web UI:** Updated version display to v2.3.1

---

## 🙏 Acknowledgments

Special thanks to the community members whose PRs inspired these features:

| Contributor | PR | Feature |
|-------------|-----|---------|
| **[@jeroenkroese](https://github.com/jeroenkroese)** | [#41](https://github.com/bodaay/HuggingFaceModelDownloader/pull/41) | Exclude file patterns |
| **[@windtail](https://github.com/windtail)** | [#38](https://github.com/bodaay/HuggingFaceModelDownloader/pull/38) | Custom HuggingFace endpoint |
| **[@cdeving](https://github.com/cdeving)** | [#50](https://github.com/bodaay/HuggingFaceModelDownloader/pull/50) | Docker support |

Also thanks to the community for bug reports and PRs that helped identify issues in v2.3.0:
- URL escaping fix (related to #60)
- TUI speed improvements (related to #59)
- API 400 fixes (related to #58)

---

# Release Notes - v2.3.0

> **Release Date:** December 31, 2025

## 🎉 Highlights

This is a **major release** introducing a brand new **Web UI**, complete project restructuring, and numerous bug fixes. The project has been reorganized into a clean, modular architecture following Go best practices.

---

## ✨ New Features

### 🌐 Web Interface
- **Beautiful Terminal-Noir themed Web UI** for managing downloads
- Real-time progress updates via WebSocket
- Separate pages for downloading **Models** and **Datasets**
- Per-file progress bars with live status updates
- Settings management (connections, retries, verification mode)
- Job deduplication - prevents duplicate downloads of the same repo

### 🚀 One-Liner Web Mode
Start the web UI instantly with:
```bash
bash <(curl -sSL https://g.bodaay.io/hfd) -w
```
Automatically opens your browser to `http://localhost:8080`

### 🔧 New CLI Commands
- `hfdownloader serve` - Start the web server
- `hfdownloader version` - Show version information
- `hfdownloader config` - Manage configuration

### 📦 Reusable Go Package
The downloader is now available as an importable package:
```go
import "github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
```

---

## 🐛 Bug Fixes

### Fixed: "error: tree API failed: 400 Bad Request"
- Repository paths with slashes (e.g., `Qwen/Qwen3-0.6B`) were being incorrectly URL-escaped
- Now correctly handles repo IDs without double-escaping the slash

### Fixed: TUI Speed/ETA Display Jumping Around
- Implemented **Exponential Moving Average (EMA)** for smooth speed calculations
- Added minimum time interval (50ms) before recalculating speed
- Both per-file and overall speeds are now stable and readable

### Fixed: TUI Total File Size Fluctuating
- File totals no longer get overwritten with incorrect values during progress updates
- Now only updates total if a valid value is provided

### Fixed: Downloads Appearing Stuck
- Removed blocking HEAD requests during repository scanning
- Large repos (90+ files) now start downloading within seconds instead of minutes
- Assumed LFS files support range requests (they always do on HuggingFace)

### Fixed: Web UI Progress Not Updating
- Added `progressReader` wrapper for real-time progress during single-file downloads
- Progress events now use correct `Downloaded` field (cumulative bytes)
- UI throttled to 10fps to prevent DOM thrashing

---

## 🏗️ Architecture Changes

### Project Structure
The codebase has been completely reorganized:

```
├── cmd/hfdownloader/     # CLI entry point
├── internal/
│   ├── cli/              # CLI commands (Cobra)
│   ├── server/           # Web server & API
│   ├── tui/              # Terminal UI
│   └── assets/           # Embedded web assets
├── pkg/hfdownloader/     # Reusable download library
└── scripts/              # Installation scripts
```

### Security Improvements
- **Output path is server-controlled** - Cannot be changed via API or Web UI
- Separate directories for models (`./Models/`) and datasets (`./Datasets/`)
- Token is never logged or exposed in API responses

### Testing
- Comprehensive unit tests for `JobManager`, API handlers, and WebSocket
- Integration tests for end-to-end download flows
- Test coverage for job deduplication and cancellation

---

## 📊 Performance Improvements

| Improvement | Before | After |
|-------------|--------|-------|
| Large repo scan (90+ files) | 5+ minutes | ~2 seconds |
| Progress update frequency | 1 second | 200ms |
| Speed display stability | Jumpy/erratic | Smooth (EMA) |
| Web UI responsiveness | Laggy | Throttled 10fps |

---

## 🔄 Breaking Changes

- Main package moved from `hfdownloader/` to `pkg/hfdownloader/`
- CLI now uses Cobra commands instead of flags-only
- `main.go` replaced with `cmd/hfdownloader/main.go`
- Old `makefile` replaced with `build.sh`

---

## 📥 Installation

### One-Liner (Recommended)
```bash
# Install to /usr/local/bin
bash <(curl -sSL https://g.bodaay.io/hfd) -i

# Start Web UI
bash <(curl -sSL https://g.bodaay.io/hfd) -w

# Download a model directly
bash <(curl -sSL https://g.bodaay.io/hfd) download TheBloke/Mistral-7B-GGUF
```

### From Source
```bash
git clone https://github.com/bodaay/HuggingFaceModelDownloader
cd HuggingFaceModelDownloader
go build -o hfdownloader ./cmd/hfdownloader
```

---

## 📋 Full Changelog

**New Files:**
- `cmd/hfdownloader/main.go` - New CLI entry point
- `internal/server/*` - Complete web server implementation
- `internal/assets/*` - Embedded web UI (HTML/CSS/JS)
- `pkg/hfdownloader/*` - Modular download library
- `build.sh` - Cross-platform build script

**Modified:**
- `scripts/gist_gethfd.sh` - Added `-w` flag for web mode
- `README.md` - Updated documentation with web UI info
- `go.mod` - Added new dependencies (Cobra, Gorilla WebSocket)

**Removed:**
- `hfdownloader/` - Moved to `pkg/hfdownloader/`
- `main.go` - Replaced by `cmd/hfdownloader/main.go`
- `makefile` - Replaced by `build.sh`

