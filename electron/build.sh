#!/bin/bash

set -euo pipefail

[ "$(bun --version)" = "1.4.0" ] || { echo "Bun 1.4.0 is required" >&2; exit 1; }
cd "$(dirname "$0")/.."
version=$(node scripts/build-version.mjs)
mkdir -p electron/dist
node scripts/build-version.mjs --json > electron/dist/build-source.json
cd electron
echo "Building New API Electron App..."

echo "Step 1: Building frontend..."
cd ../frontend
bun install --frozen-lockfile
bun run build
bun ../scripts/prepare-frontend-embed.mjs
cd ../electron

echo "Step 2: Building Go backend..."
cd ..

if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "Building for macOS..."
    CGO_ENABLED=1 go build -ldflags="-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o new-api
    cd electron
    bun install --frozen-lockfile
    bun run build:mac -- --publish never
elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo "Building for Linux..."
    CGO_ENABLED=1 go build -ldflags="-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o new-api
    cd electron
    bun install --frozen-lockfile
    bun run build:linux -- --publish never
elif [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" || "$OSTYPE" == "win32" ]]; then
    echo "Building for Windows..."
    CGO_ENABLED=1 go build -ldflags="-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o new-api.exe
    cd electron
    bun install --frozen-lockfile
    bun run build:win -- --publish never
else
    echo "Unknown OS, building for current platform..."
    CGO_ENABLED=1 go build -ldflags="-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o new-api
    cd electron
    bun install --frozen-lockfile
    bun run build -- --publish never
fi

echo "Build complete! Check electron/dist/ for output."
