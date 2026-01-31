#!/bin/bash

# HuggingFace Model Downloader - One-liner installer & runner
# Hosted as GitHub Gist, shortened via Cloudflare Worker: https://g.bodaay.io/hfd
#
# Usage:
#   bash <(curl -sSL https://g.bodaay.io/hfd) analyze MODEL -i      # Analyze model (interactive)
#   bash <(curl -sSL https://g.bodaay.io/hfd) download MODEL        # Download a model
#   bash <(curl -sSL https://g.bodaay.io/hfd) serve                 # Start web UI
#   bash <(curl -sSL https://g.bodaay.io/hfd) serve --port 3000     # Start web UI on custom port
#   bash <(curl -sSL https://g.bodaay.io/hfd) install               # Install to /usr/local/bin
#   bash <(curl -sSL https://g.bodaay.io/hfd) install ~/.local/bin  # Install to custom path

set -e

# Colors (disabled if NO_COLOR is set or not a terminal)
if [ -t 1 ] && [ -z "$NO_COLOR" ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    CYAN='\033[0;36m'
    NC='\033[0m' # No Color
else
    RED='' GREEN='' YELLOW='' CYAN='' NC=''
fi

info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# Detect OS and architecture
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | tr '[:upper:]' '[:lower:]')

# Normalize architecture names
case "$arch" in
    x86_64)  arch="amd64" ;;
    aarch64) arch="arm64" ;;
    armv7l)  arch="arm" ;;
esac

# GitHub repo and release info
repo="bodaay/HuggingFaceModelDownloader"
binary_name="hfdownloader"

# Check for install command (must be first argument)
install_mode=false
install_path="/usr/local/bin"

if [ "$1" = "install" ]; then
    install_mode=true
    shift
    # Optional: custom install path as second argument
    if [ -n "$1" ] && [ "${1:0:1}" != "-" ]; then
        install_path="$1"
        shift
    fi
fi

# All remaining args pass through to hfdownloader
passthrough_args=("$@")

# Fetch latest release tag
info "Fetching latest release..."
latest_tag=$(curl --silent --fail "https://api.github.com/repos/$repo/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$latest_tag" ]; then
    err "Could not fetch latest release tag from GitHub"
    exit 1
fi

info "Latest version: $latest_tag"

# Build download URL
url="https://github.com/${repo}/releases/download/${latest_tag}/${binary_name}_${os}_${arch}_${latest_tag}"
temp_binary="/tmp/${binary_name}_$$"

# Download binary
info "Downloading ${binary_name} for ${os}/${arch}..."
if ! curl -fSL -o "$temp_binary" "$url" 2>/dev/null; then
    err "Download failed from: $url"
    err "Check if binary exists for your platform: ${os}/${arch}"
    rm -f "$temp_binary"
    exit 1
fi
chmod +x "$temp_binary"
ok "Downloaded successfully"

# Install mode: copy to system bin
if [ "$install_mode" = true ]; then
    info "Installing to ${install_path}..."
    
    # Create directory if it doesn't exist
    if [ ! -d "$install_path" ]; then
        if ! mkdir -p "$install_path" 2>/dev/null; then
            info "Requesting sudo to create $install_path..."
            sudo mkdir -p "$install_path"
        fi
    fi
    
    # Move binary to install path
    target="${install_path}/${binary_name}"
    if ! mv "$temp_binary" "$target" 2>/dev/null; then
        info "Requesting sudo to install to $install_path..."
        sudo mv "$temp_binary" "$target"
        sudo chmod +x "$target"
    fi
    
    ok "Installed: $target"
    
    # Check if in PATH
    if command -v "$binary_name" &>/dev/null; then
        ok "${binary_name} is in your PATH. Run: ${binary_name} --help"
else
        warn "${install_path} is not in your PATH."
        echo "    Add this to your shell profile:"
        echo "    export PATH=\"${install_path}:\$PATH\""
    fi
    
    # Show version
    "$target" --version 2>/dev/null || true
    exit 0
fi

# Cleanup function for temp binary
cleanup() {
    rm -f "$temp_binary" 2>/dev/null || true
}
trap cleanup EXIT

# Run mode: execute with passed arguments directly from temp binary
# Use 'install' command to install permanently
if [ ${#passthrough_args[@]} -eq 0 ]; then
    exec "$temp_binary" --help
else
    exec "$temp_binary" "${passthrough_args[@]}"
fi
