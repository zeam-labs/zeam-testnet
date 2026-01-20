#!/bin/bash
# ZEAM CLI Build Script
# Builds standalone CLI binaries for all platforms (no desktop UI)
# For desktop app with UI, use: ./app/build.sh

set -e

VERSION=${1:-"dev"}
OUTPUT_DIR="dist"
BINARY_NAME="zeam"

echo "==================================="
echo "  ZEAM CLI Builder"
echo "==================================="
echo ""
echo "Building ZEAM CLI $VERSION for all platforms..."
echo "(For desktop app with UI, use: ./app/build.sh)"
echo ""

mkdir -p $OUTPUT_DIR

# Build targets
TARGETS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
)

for target in "${TARGETS[@]}"; do
    GOOS=${target%/*}
    GOARCH=${target#*/}

    output="$OUTPUT_DIR/${BINARY_NAME}-${GOOS}-${GOARCH}"
    if [ "$GOOS" = "windows" ]; then
        output="${output}.exe"
        # Windows cross-compile needs CGO for crypto
        echo "Building $GOOS/$GOARCH (with CGO)..."
        CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=$GOOS GOARCH=$GOARCH \
            go build -ldflags="-s -w -X main.Version=$VERSION" -o "$output" ./cmd/
    else
        echo "Building $GOOS/$GOARCH..."
        GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w -X main.Version=$VERSION" -o "$output" ./cmd/
    fi

    # Get file size
    size=$(ls -lh "$output" | awk '{print $5}')
    echo "  -> $output ($size)"
done

echo ""
echo "Build complete. Binaries in $OUTPUT_DIR/"
ls -la $OUTPUT_DIR/
