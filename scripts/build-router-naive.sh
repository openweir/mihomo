#!/usr/bin/env bash
# Build the self-contained Linux raw asset consumed by the OpenWrt installer.
set -euo pipefail
: "${VERSION:?release version required}" "${OPENWEIR_ZIG:?path to Zig required}"
[[ $("$OPENWEIR_ZIG" version) == 0.15.2 ]]
case "${GOARCH:?target architecture required}" in
  arm64) export OPENWEIR_MUSL_TARGET=aarch64-linux-musl GOARM64=v8.0 ;;
  amd64) export OPENWEIR_MUSL_TARGET=x86_64-linux-musl GOAMD64=v1 ;;
  *) echo 'Static Naive supports arm64 and amd64' >&2; exit 1 ;;
esac
export GOOS=linux CGO_ENABLED=1 CGO_LDFLAGS_ALLOW='-l:libcronet\.a'
export CC="$(cd "$(dirname "$0")" && pwd)/zig-cc.py"
go build -mod=readonly -trimpath -tags with_gvisor,with_naive,with_musl \
  -ldflags "-s -w -buildid= -linkmode external -extldflags -static -X github.com/metacubex/mihomo/constant.Version=$VERSION" \
  -o "${1:-mihomo}" .
