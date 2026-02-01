#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TAURI_DIR="$SCRIPT_DIR/src-tauri"

case "$(uname -s)-$(uname -m)" in
    Linux-x86_64)   TRIPLE=x86_64-unknown-linux-gnu ;;
    Linux-aarch64)  TRIPLE=aarch64-unknown-linux-gnu ;;
    Darwin-arm64)   TRIPLE=aarch64-apple-darwin ;;
    Darwin-x86_64)  TRIPLE=x86_64-apple-darwin ;;
    *)              echo "Unsupported platform"; exit 1 ;;
esac

SIDECAR="$TAURI_DIR/binaries/zeam-daemon-${TRIPLE}"

echo "=== Building Go daemon ==="
cd "$ROOT_DIR"
go build -o "$SIDECAR" ./cmd/
echo "    -> $SIDECAR"

echo ""
echo "=== Starting Tauri dev ==="
cd "$TAURI_DIR"
cargo tauri dev
