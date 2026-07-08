#!/bin/sh
# shellcheck disable=SC3040
set -eu

if ! command -v go >/dev/null 2>&1; then
    if [ ! -f /tmp/go/bin/go ]; then
        echo "Installing Go..."
        curl -sL https://go.dev/dl/go1.24.linux-amd64.tar.gz | tar -C /tmp -xz
    fi
    export PATH="/tmp/go/bin:$PATH"
fi

BUILD_DIR="$(cd "$(dirname "$0")/.." && pwd)/build"
BIN_DIR="${BUILD_DIR}/bin"
mkdir -p "${BIN_DIR}"

echo "Building cognitiveosd..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o "${BIN_DIR}/cognitiveosd" ./cmd/cognitiveosd
echo "  -> ${BIN_DIR}/cognitiveosd"
