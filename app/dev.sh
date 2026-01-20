#!/bin/bash
# ZEAM Development Mode
# Runs the Go backend and opens the UI

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
PORT=${1:-8080}

echo "==================================="
echo "  ZEAM Development Mode"
echo "==================================="
echo ""

# Build if needed
if [ ! -f "$ROOT_DIR/zeam" ]; then
    echo "[BUILD] Building ZEAM..."
    cd "$ROOT_DIR"
    go build -o zeam ./cmd/
fi

# Check for OEWN
if [ ! -f "$ROOT_DIR/oewn.json" ]; then
    echo "ERROR: oewn.json not found in $ROOT_DIR"
    exit 1
fi

echo "[START] Starting ZEAM on port $PORT..."
cd "$ROOT_DIR"
./zeam -port "$PORT" -ui app/ui &
ZEAM_PID=$!

# Wait for server to be ready
echo "[WAIT] Waiting for server..."
for i in {1..30}; do
    if curl -s "http://localhost:$PORT/health" > /dev/null 2>&1; then
        break
    fi
    sleep 1
done

echo ""
echo "==================================="
echo "  ZEAM is running!"
echo "==================================="
echo ""
echo "  UI:     http://localhost:$PORT"
echo "  API:    http://localhost:$PORT/health"
echo ""
echo "Press Ctrl+C to stop"
echo ""

# Wait for Ctrl+C
trap "kill $ZEAM_PID 2>/dev/null; exit 0" INT TERM
wait $ZEAM_PID
