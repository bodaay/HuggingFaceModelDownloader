#!/bin/bash
# Build script for HuggingFaceModelDownloader
# Builds fully static binaries for multiple platforms

set -e

# Get the script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

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
LDFLAGS="-s -w -X main.Version=${VERSION}"

# Build targets: OS_ARCH
TARGETS=(
    "darwin_arm64"
    "darwin_amd64"
    "linux_amd64"
    "linux_arm64"
    "windows_amd64"
)

# Build function
build() {
    local os_arch=$1
    local os=${os_arch%_*}
    local arch=${os_arch#*_}
    
    local output_name="hfdownloader_${os}_${arch}_${VERSION}"
    
    # Add .exe extension for Windows
    if [ "$os" = "windows" ]; then
        output_name="${output_name}.exe"
    fi
    
    local output_path="${OUTPUT_DIR}/${output_name}"
    
    echo "Building for ${os}/${arch}..."
    
    # Build with CGO disabled for fully static binary
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
        -ldflags "$LDFLAGS" \
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

for target in "${TARGETS[@]}"; do
    build "$target"
done

echo "================================"
echo ""
echo "Build complete! Binaries are in: ${OUTPUT_DIR}/"
echo ""
ls -lh "${OUTPUT_DIR}"/hfdownloader_*_${VERSION}* 2>/dev/null || true


