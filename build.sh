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
    
    # Create Windows application manifest
    cat > "${MAIN_PKG}/hfdownloader.exe.manifest" << 'MANIFEST'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity
    type="win32"
    name="HuggingFaceModelDownloader"
    version="1.0.0.0"
    processorArchitecture="amd64"/>
  <description>HuggingFace Model Downloader - Download AI models from HuggingFace Hub</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <!-- Windows 10/11 -->
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <!-- Windows 8.1 -->
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <!-- Windows 8 -->
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <!-- Windows 7 -->
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </windowsSettings>
  </application>
</assembly>
MANIFEST

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
        "Comments": "HuggingFace Model Downloader - Download AI models from HuggingFace Hub",
        "CompanyName": "HuggingFace Model Downloader Project",
        "FileDescription": "HuggingFace Model Downloader CLI Tool",
        "FileVersion": "${VERSION}",
        "InternalName": "hfdownloader",
        "LegalCopyright": "Copyright (c) 2024-2026 HuggingFace Model Downloader Contributors. Apache-2.0 License.",
        "LegalTrademarks": "",
        "OriginalFilename": "hfdownloader.exe",
        "PrivateBuild": "",
        "ProductName": "HuggingFace Model Downloader",
        "ProductVersion": "${VERSION}",
        "SpecialBuild": ""
    },
    "VarFileInfo": {
        "Translation": {
            "LangID": "0409",
            "CharsetID": "04B0"
        }
    },
    "ManifestPath": "hfdownloader.exe.manifest"
}
EOF
    
    echo "  Generating Windows version resource with manifest..."
    (cd "${MAIN_PKG}" && goversioninfo -o resource_windows_amd64.syso)
    return 0
}

# Cleanup Windows version info files
cleanup_windows_versioninfo() {
    rm -f "${MAIN_PKG}/versioninfo.json"
    rm -f "${MAIN_PKG}/hfdownloader.exe.manifest"
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



