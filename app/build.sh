#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TAURI_DIR="$SCRIPT_DIR/src-tauri"

TARGET="${1:-current}"

case "$TARGET" in
    current)
        case "$(uname -s)-$(uname -m)" in
            Linux-x86_64)   GOOS=linux;  GOARCH=amd64; RUST_TARGET=x86_64-unknown-linux-gnu;  DAEMON=zeam-daemon ;;
            Linux-aarch64)  GOOS=linux;  GOARCH=arm64; RUST_TARGET=aarch64-unknown-linux-gnu;  DAEMON=zeam-daemon ;;
            Darwin-arm64)   GOOS=darwin; GOARCH=arm64; RUST_TARGET=aarch64-apple-darwin;        DAEMON=zeam-daemon ;;
            Darwin-x86_64)  GOOS=darwin; GOARCH=amd64; RUST_TARGET=x86_64-apple-darwin;         DAEMON=zeam-daemon ;;
            *)              echo "Unsupported platform"; exit 1 ;;
        esac
        ;;
    windows-x64)  GOOS=windows; GOARCH=amd64; RUST_TARGET=x86_64-pc-windows-gnu;   DAEMON=zeam-daemon.exe ;;
    macos-arm64)  GOOS=darwin;  GOARCH=arm64; RUST_TARGET=aarch64-apple-darwin;      DAEMON=zeam-daemon ;;
    macos-x64)    GOOS=darwin;  GOARCH=amd64; RUST_TARGET=x86_64-apple-darwin;       DAEMON=zeam-daemon ;;
    linux-x64)    GOOS=linux;   GOARCH=amd64; RUST_TARGET=x86_64-unknown-linux-gnu;  DAEMON=zeam-daemon ;;
    *)            echo "Unknown target: $TARGET"; exit 1 ;;
esac

SIDECAR_NAME="zeam-daemon-${RUST_TARGET}"
if [ "$GOOS" = "windows" ]; then
    SIDECAR_NAME="${SIDECAR_NAME}.exe"
fi

echo "=== Building ZEAM for $RUST_TARGET ==="

echo ""
echo "--- Go daemon ---"
cd "$ROOT_DIR"
GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 go build -o "$TAURI_DIR/binaries/$SIDECAR_NAME" ./cmd/
echo "    -> src-tauri/binaries/$SIDECAR_NAME ($(du -h "$TAURI_DIR/binaries/$SIDECAR_NAME" | cut -f1))"

echo ""
echo "--- Tauri app ---"
cd "$TAURI_DIR"

if [ "$TARGET" = "current" ]; then
    cargo tauri build
else
    cargo tauri build --target "$RUST_TARGET"
fi

echo ""
echo "=== Build complete ==="
echo "Bundles in: $TAURI_DIR/target/release/bundle/ (or target/$RUST_TARGET/release/bundle/)"
