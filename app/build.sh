#!/bin/bash
# ZEAM Desktop App Build Script
# Builds the Go backend and Tauri desktop app

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==================================="
echo "  ZEAM Desktop App Builder"
echo "==================================="
echo ""

# Parse arguments
TARGET=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --windows)
            TARGET="windows"
            shift
            ;;
        --linux)
            TARGET="linux"
            shift
            ;;
        --macos)
            TARGET="macos"
            shift
            ;;
        --macos-intel)
            TARGET="macos-intel"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: ./build.sh [--windows|--linux|--macos|--macos-intel]"
            exit 1
            ;;
    esac
done

# Detect OS and architecture if no target specified
if [ -z "$TARGET" ]; then
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)
    case $OS in
        darwin) TARGET="macos" ;;
        linux) TARGET="linux" ;;
        *) TARGET="linux" ;;
    esac
fi

# Set target triple and Go env vars based on target
case $TARGET in
    windows)
        TARGET_TRIPLE="x86_64-pc-windows-gnu"
        GOOS="windows"
        GOARCH="amd64"
        EXT=".exe"
        RUST_TARGET="x86_64-pc-windows-gnu"
        ;;
    linux)
        TARGET_TRIPLE="x86_64-unknown-linux-gnu"
        GOOS="linux"
        GOARCH="amd64"
        EXT=""
        RUST_TARGET=""
        ;;
    macos)
        ARCH=$(uname -m)
        if [ "$ARCH" = "arm64" ] || [ "$ARCH" = "aarch64" ]; then
            TARGET_TRIPLE="aarch64-apple-darwin"
            GOARCH="arm64"
            RUST_TARGET="aarch64-apple-darwin"
        else
            TARGET_TRIPLE="x86_64-apple-darwin"
            GOARCH="amd64"
            RUST_TARGET="x86_64-apple-darwin"
        fi
        GOOS="darwin"
        EXT=""
        ;;
    macos-intel)
        TARGET_TRIPLE="x86_64-apple-darwin"
        GOOS="darwin"
        GOARCH="amd64"
        EXT=""
        RUST_TARGET="x86_64-apple-darwin"
        ;;
esac

echo "Building for: $TARGET ($TARGET_TRIPLE)"
echo ""

# Step 1: Build Go backend
echo "[1/3] Building ZEAM Go backend..."
cd "$ROOT_DIR"

BACKEND_NAME="zeam-backend-$TARGET_TRIPLE$EXT"
mkdir -p "$SCRIPT_DIR/src-tauri/binaries"

if [ "$TARGET" = "windows" ]; then
    # Cross-compile for Windows requires CGO with mingw
    CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=$GOOS GOARCH=$GOARCH \
        go build -o "$SCRIPT_DIR/src-tauri/binaries/$BACKEND_NAME" ./cmd/
else
    GOOS=$GOOS GOARCH=$GOARCH go build -o "$SCRIPT_DIR/src-tauri/binaries/$BACKEND_NAME" ./cmd/
fi

chmod +x "$SCRIPT_DIR/src-tauri/binaries/$BACKEND_NAME"
echo "  - Built $BACKEND_NAME ($(du -h "$SCRIPT_DIR/src-tauri/binaries/$BACKEND_NAME" | cut -f1))"

# Step 2: Prepare resources
echo "[2/3] Preparing resources..."
mkdir -p "$SCRIPT_DIR/src-tauri/resources"
mkdir -p "$SCRIPT_DIR/src-tauri/icons"

# Copy OEWN dictionary
if [ -f "$ROOT_DIR/oewn.json" ]; then
    cp "$ROOT_DIR/oewn.json" "$SCRIPT_DIR/src-tauri/resources/"
    echo "  - Copied oewn.json ($(du -h "$ROOT_DIR/oewn.json" | cut -f1))"
else
    echo "  ! Warning: oewn.json not found - download from build server"
fi

# Create placeholder icons if they don't exist
if [ ! -f "$SCRIPT_DIR/src-tauri/icons/32x32.png" ]; then
    echo "  - Creating placeholder icons..."
    cd "$SCRIPT_DIR/src-tauri/icons"
    echo 'iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAALklEQVR4AWP4////HQMDAwP////HAAEoNCoQDQB6gWgASIPRUOADRkOBZAAwHAEAH1cIHkGPQ7EAAAAASUVORK5CYII=' | base64 -d > 32x32.png
    echo 'iVBORw0KGgoAAAANSUhEUgAAAIAAAACACAYAAADDPmHLAAAAMElEQVR4Ae3BMQEAAADCIPuntsEO4AUAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB4Nf8AyAABm9UbswAAAABJRU5ErkJggg==' | base64 -d > 128x128.png
    cp 128x128.png 128x128@2x.png
    cp 128x128.png icon.icns
    cp 128x128.png icon.ico
fi

# Step 3: Build Tauri app
echo "[3/3] Building Tauri app..."
cd "$SCRIPT_DIR/src-tauri"

# Check if cargo tauri is installed
if ! command -v cargo-tauri &> /dev/null; then
    echo "  Installing tauri-cli..."
    cargo install tauri-cli
fi

if [ -n "$RUST_TARGET" ]; then
    cargo tauri build --target "$RUST_TARGET"
else
    cargo tauri build
fi

# Step 4: Copy installer to standard output location
echo "[4/4] Copying installer to dist/installers..."
INSTALLER_DIR="$ROOT_DIR/dist/installers"
mkdir -p "$INSTALLER_DIR"

# Find and copy the installer
case $TARGET in
    windows)
        INSTALLER=$(find "$SCRIPT_DIR/src-tauri/target" -name "*.exe" -path "*/bundle/nsis/*" 2>/dev/null | head -1)
        if [ -n "$INSTALLER" ]; then
            cp "$INSTALLER" "$INSTALLER_DIR/"
            echo "  - Copied $(basename "$INSTALLER")"
        fi
        ;;
    linux)
        # Copy AppImage and deb
        for pkg in $(find "$SCRIPT_DIR/src-tauri/target" -name "*.AppImage" -o -name "*.deb" 2>/dev/null); do
            cp "$pkg" "$INSTALLER_DIR/"
            echo "  - Copied $(basename "$pkg")"
        done
        ;;
    macos|macos-intel)
        # Copy dmg
        DMG=$(find "$SCRIPT_DIR/src-tauri/target" -name "*.dmg" 2>/dev/null | head -1)
        if [ -n "$DMG" ]; then
            cp "$DMG" "$INSTALLER_DIR/"
            echo "  - Copied $(basename "$DMG")"
        fi
        ;;
esac

echo ""
echo "==================================="
echo "  Build Complete!"
echo "==================================="
echo ""
echo "Installer output:"
ls -lh "$INSTALLER_DIR/"
echo ""
