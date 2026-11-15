#!/usr/bin/env bash
# Build the distributable mousemover.exe into dist/ (Git Bash).
# Usage: ./build.sh
# Keep the flags in sync with build.ps1.
set -euo pipefail

cd "$(dirname "$0")"

export CGO_ENABLED=0
export GOOS=windows
export GOARCH=amd64

echo '==> go vet ./...'
go vet ./...

echo '==> go test ./...'
go test ./...

mkdir -p dist

echo '==> go build'
go build -ldflags "-s -w -H=windowsgui" -o dist/mousemover.exe ./cmd/mousemover

echo "Built: $(pwd)/dist/mousemover.exe"
