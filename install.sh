#!/bin/sh
set -e

# homebutler installer
# Usage: curl -fsSL https://raw.githubusercontent.com/Higangssh/homebutler/main/install.sh | sh

REPO="Higangssh/homebutler"
INSTALL_DIR="/usr/local/bin"
BINARY="homebutler"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info() { printf "${GREEN}▸${NC} %s\n" "$1"; }
warn() { printf "${YELLOW}▸${NC} %s\n" "$1"; }
error() { printf "${RED}✗${NC} %s\n" "$1" >&2; exit 1; }

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux|darwin) ;;
    *) error "Unsupported OS: $OS (supported: linux, darwin)" ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH (supported: amd64, arm64)" ;;
esac

info "Detected platform: ${OS}/${ARCH}"

# Get latest version
info "Fetching latest version..."
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"//;s/".*//')
if [ -z "$VERSION" ]; then
    error "Failed to fetch latest version"
fi
info "Latest version: ${VERSION}"

# Download
FILENAME="homebutler_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

info "Downloading ${FILENAME}..."
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

if ! curl -fsSL "$URL" -o "${TMPDIR}/${FILENAME}"; then
    error "Download failed: ${URL}"
fi

# Extract
tar xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"

if [ ! -f "${TMPDIR}/${BINARY}" ]; then
    error "Binary not found in archive"
fi

# Install — try standard locations in order
install_binary() {
    local src="${TMPDIR}/${BINARY}"
    
    # 1. /usr/local/bin (standard, may need sudo)
    if [ -w "/usr/local/bin" ]; then
        mv "$src" "/usr/local/bin/${BINARY}"
        INSTALL_DIR="/usr/local/bin"
        return 0
    fi
    
    # 2. Try with sudo
    if command -v sudo >/dev/null 2>&1; then
        info "Installing to /usr/local/bin (requires sudo)..."
        if sudo mv "$src" "/usr/local/bin/${BINARY}" 2>/dev/null; then
            INSTALL_DIR="/usr/local/bin"
            return 0
        fi
    fi
    
    # 3. Homebrew bin (macOS)
    if [ -d "/opt/homebrew/bin" ] && [ -w "/opt/homebrew/bin" ]; then
        mv "$src" "/opt/homebrew/bin/${BINARY}"
        INSTALL_DIR="/opt/homebrew/bin"
        return 0
    fi
    
    # 4. ~/.local/bin (user-local, XDG)
    mkdir -p "$HOME/.local/bin"
    mv "$src" "$HOME/.local/bin/${BINARY}"
    INSTALL_DIR="$HOME/.local/bin"
    
    # Auto-add to PATH in all existing rc files
    for rc in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc"; do
        if [ -f "$rc" ]; then
            if ! grep -qF '.local/bin' "$rc" 2>/dev/null; then
                echo 'export PATH="$PATH:$HOME/.local/bin"' >> "$rc"
                info "Added ~/.local/bin to PATH in $(basename $rc)"
            fi
        fi
    done
    return 0
}

install_binary
chmod +x "${INSTALL_DIR}/${BINARY}"

# Verify
if command -v homebutler >/dev/null 2>&1; then
    info "Installed to ${INSTALL_DIR}/${BINARY}"
    printf "\n"
    homebutler version
    printf "\n"
    info "Run 'homebutler help' to get started"
else
    warn "Installed to ${INSTALL_DIR}/${BINARY}"
    warn "${INSTALL_DIR} is not in your PATH. Add it:"
    printf "\n"
    printf "  export PATH=\$PATH:${INSTALL_DIR}\n"
    printf "\n"
fi
