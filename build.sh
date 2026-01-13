#!/bin/bash
# Build script for HuggingFaceModelDownloader
# Builds fully static binaries for multiple platforms

set -e

# Get the script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Add Go bin to PATH (for goversioninfo and other Go tools)
export PATH="$PATH:$(go env GOPATH)/bin"

# Read version from VERSION file
VERSION=$(cat VERSION | tr -d '[:space:]')
if [ -z "$VERSION" ]; then
    echo "Error: VERSION file is empty or not found"
    exit 1
fi

echo "Building hfdownloader version: $VERSION"

# Output directory
OUTPUT_DIR="output"
mkdir -p "$OUTPUT_DIR"

# Main package path
MAIN_PKG="./cmd/hfdownloader"

# Ldflags for version injection and optimized binary
# Note: -s -w strips debug info (smaller binary but may trigger AV false positives on Windows)
LDFLAGS_STRIP="-s -w -X main.Version=${VERSION}"
LDFLAGS_NOSTRIP="-X main.Version=${VERSION}"

# Build targets: OS_ARCH
TARGETS=(
    "darwin_arm64"
    "darwin_amd64"
    "linux_amd64"
    "linux_arm64"
    "windows_amd64"
)

# Generate Windows version info if goversioninfo is available
generate_windows_versioninfo() {
    if ! command -v goversioninfo &> /dev/null; then
        echo "  Note: goversioninfo not found, skipping Windows metadata"
        echo "  Install with: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest"
        return 1
    fi
    
    # Parse version components (handle -dev suffix)
    local ver_clean="${VERSION%-*}"  # Remove -dev or similar suffix
    local major minor patch
    IFS='.' read -r major minor patch <<< "$ver_clean"
    major=${major:-0}
    minor=${minor:-0}
    patch=${patch:-0}
    
    cat > "${MAIN_PKG}/versioninfo.json" << EOF
{
    "FixedFileInfo": {
        "FileVersion": {
            "Major": ${major},
            "Minor": ${minor},
            "Patch": ${patch},
            "Build": 0
        },
        "ProductVersion": {
            "Major": ${major},
            "Minor": ${minor},
            "Patch": ${patch},
            "Build": 0
        },
        "FileFlagsMask": "3f",
        "FileFlags": "00",
        "FileOS": "040004",
        "FileType": "01",
        "FileSubType": "00"
    },
    "StringFileInfo": {
        "Comments": "HuggingFace Model Downloader - Download models from HuggingFace Hub",
        "CompanyName": "Open Source",
        "FileDescription": "HuggingFace Model Downloader",
        "FileVersion": "${VERSION}",
        "InternalName": "hfdownloader",
        "LegalCopyright": "Apache-2.0 License",
        "OriginalFilename": "hfdownloader.exe",
        "ProductName": "HuggingFace Model Downloader",
        "ProductVersion": "${VERSION}"
    },
    "VarFileInfo": {
        "Translation": {
            "LangID": "0409",
            "CharsetID": "04B0"
        }
    }
}
EOF
    
    echo "  Generating Windows version resource..."
    (cd "${MAIN_PKG}" && goversioninfo -o resource_windows_amd64.syso)
    return 0
}

# Cleanup Windows version info files
cleanup_windows_versioninfo() {
    rm -f "${MAIN_PKG}/versioninfo.json"
    rm -f "${MAIN_PKG}/resource_windows_amd64.syso"
}

# Build function
build() {
    local os_arch=$1
    local os=${os_arch%_*}
    local arch=${os_arch#*_}
    
    local output_name="hfdownloader_${os}_${arch}_${VERSION}"
    local ldflags="$LDFLAGS_STRIP"
    
    # Add .exe extension for Windows
    if [ "$os" = "windows" ]; then
        output_name="${output_name}.exe"
        # Don't strip Windows binaries - reduces AV false positives
        ldflags="$LDFLAGS_NOSTRIP"
    fi
    
    local output_path="${OUTPUT_DIR}/${output_name}"
    
    echo "Building for ${os}/${arch}..."
    
    # Build with CGO disabled for fully static binary
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
        -ldflags "$ldflags" \
        -trimpath \
        -o "$output_path" \
        "$MAIN_PKG"
    
    echo "  -> ${output_path}"
}

# Clean old builds (optional, uncomment if needed)
# echo "Cleaning old builds..."
# rm -f "${OUTPUT_DIR}"/hfdownloader_*

# Build all targets
echo ""
echo "Starting builds..."
echo "================================"

# Generate Windows version info before building
HAS_VERSIONINFO=false
if generate_windows_versioninfo; then
    HAS_VERSIONINFO=true
fi

for target in "${TARGETS[@]}"; do
    build "$target"
done

# Cleanup Windows version info files
if [ "$HAS_VERSIONINFO" = true ]; then
    cleanup_windows_versioninfo
fi

echo "================================"
echo ""
echo "Build complete! Binaries are in: ${OUTPUT_DIR}/"
echo ""
ls -lh "${OUTPUT_DIR}"/hfdownloader_*_${VERSION}* 2>/dev/null || true



