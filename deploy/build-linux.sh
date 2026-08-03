#!/usr/bin/env bash
# 在开发机或 CI 上交叉编译 Linux amd64 二进制
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p bin
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/server ./cmd/server
echo "OK: bin/server (linux/amd64)"
