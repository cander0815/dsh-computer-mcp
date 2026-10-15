#!/bin/sh
set -eu
cd "$(dirname "$0")"
GO_BIN="${GO_BIN:-go}"
if [ "$($GO_BIN version | awk '{print $3}')" != go1.22.12 ]; then
    echo "兼容构建必须使用官方 Go 1.22.12。"
    exit 1
fi
mkdir -p dist
for target in darwin/amd64 darwin/arm64 windows/amd64 linux/amd64; do
    platform="${target%/*}"
    arch="${target#*/}"
    label="$platform"
    suffix=""
    [ "$platform" != darwin ] || label=macos
    [ "$platform" != windows ] || suffix=.exe
    output="dist/dsh-computer-mcp-$label-$arch$suffix"
    CGO_ENABLED=0 GOOS="$platform" GOARCH="$arch" "$GO_BIN" build -trimpath -ldflags='-s -w' -o "$output" .
    if [ "$platform" = darwin ]; then
        python3 patch-macho.py "$output"
        codesign --force --sign - "$output"
    fi
done
