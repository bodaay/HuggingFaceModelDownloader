#!/bin/bash

# HuggingFace Model Downloader - One-liner installer & runner
# Hosted as GitHub Gist, shortened via Cloudflare Worker: https://g.bodaay.io/hfd
#
# Usage:
#   bash <(curl -sSL https://g.bodaay.io/hfd) download MODEL        # Download a model
#   bash <(curl -sSL https://g.bodaay.io/hfd) -w                    # Start web UI (opens browser)
#   bash <(curl -sSL https://g.bodaay.io/hfd) serve --port 3000     # Start web UI on custom port
#   bash <(curl -sSL https://g.bodaay.io/hfd) -i                    # Install to /usr/local/bin
#   bash <(curl -sSL https://g.bodaay.io/hfd) -i -p ~/.local/bin    # Install to custom path

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

# Parse script-specific flags
install_mode=false
install_path="/usr/local/bin"
web_mode=false
web_port=8080
passthrough_args=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        -i|--install)
            install_mode=true
            shift
            ;;
        -p|--install-path)
            if [ -n "$2" ] && [ "${2:0:1}" != "-" ]; then
                install_path="$2"
                shift 2
            else
                err "Missing path argument for $1"
                exit 1
            fi
            ;;
        -w|--web)
            web_mode=true
            shift
            # Check for optional port
            if [ -n "$1" ] && [[ "$1" =~ ^[0-9]+$ ]]; then
                web_port="$1"
                shift
            fi
            ;;
        *)
            passthrough_args+=("$1")
            shift
            ;;
    esac
done

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

# Web mode: start server and open browser
if [ "$web_mode" = true ]; then
    info "Starting HuggingFace Downloader Web UI..."
    
    # Move to local binary
    local_binary="./${binary_name}"
    if [ -d "$local_binary" ]; then
        local_binary="./${binary_name}_bin"
    fi
    mv "$temp_binary" "$local_binary" 2>/dev/null || true
    
    # Determine how to open browser
    open_browser() {
        local url="$1"
        if command -v xdg-open &>/dev/null; then
            xdg-open "$url" &>/dev/null &
        elif command -v open &>/dev/null; then
            open "$url" &>/dev/null &
        elif command -v start &>/dev/null; then
            start "$url" &>/dev/null &
        fi
    }
    
    echo ""
    echo -e "${CYAN}╭────────────────────────────────────────────────────────╮${NC}"
    echo -e "${CYAN}│${NC}     🤗  ${GREEN}HuggingFace Downloader Web UI${NC}                  ${CYAN}│${NC}"
    echo -e "${CYAN}├────────────────────────────────────────────────────────┤${NC}"
    echo -e "${CYAN}│${NC}                                                        ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  Dashboard:  ${GREEN}http://localhost:${web_port}${NC}                      ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}                                                        ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  Press ${YELLOW}Ctrl+C${NC} to stop the server                     ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}                                                        ${CYAN}│${NC}"
    echo -e "${CYAN}╰────────────────────────────────────────────────────────╯${NC}"
    echo ""
    
    # Open browser after a short delay
    (sleep 1.5 && open_browser "http://localhost:${web_port}") &
    
    # Run the server
    exec "$local_binary" serve --port "$web_port" "${passthrough_args[@]}"
fi

# Run mode: execute with passed arguments
local_binary="./${binary_name}"

# Handle edge case: if a directory with binary name exists (e.g., Go package)
if [ -d "$local_binary" ]; then
    local_binary="./${binary_name}_bin"
    warn "Directory './${binary_name}' exists, using '${local_binary}' instead"
fi

# Check if we already have a local copy
if [ -f "$local_binary" ]; then
    # Compare versions if possible
    existing_version=$("$local_binary" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "")
    if [ "$existing_version" = "$latest_tag" ]; then
        info "Using existing ./${binary_name} (v${existing_version})"
        rm -f "$temp_binary"
    else
        warn "Updating ./${binary_name} from v${existing_version:-unknown} to v${latest_tag}"
        mv "$temp_binary" "$local_binary"
    fi
else
    mv "$temp_binary" "$local_binary"
    ok "Saved: ${local_binary}"
fi

# Run with passed arguments, or show help if none
if [ ${#passthrough_args[@]} -eq 0 ]; then
    exec "$local_binary" --help
else
    exec "$local_binary" "${passthrough_args[@]}"
fi
