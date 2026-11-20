#!/bin/sh
set -eu
binary=${1:?}
version=${2:?}
reported=$("$binary" -v)
case "$reported" in
  "Mihomo Meta $version linux "*with_naive*) ;;
  *) echo "Unexpected binary: $reported" >&2; exit 1 ;;
esac
home=$(mktemp -d)
trap 'rm -rf "$home"' EXIT
cat > "$home/config.yaml" <<'YAML'
mixed-port: 19763
tproxy-port: 19762
tun: {enable: true, stack: system, auto-route: false}
proxies:
  - {name: naive, type: naive, server: example.invalid, port: 443, username: test, password: test}
  - {name: nw3, type: nowhere, server: example.invalid, port: 443, network: tcp, password: '0102030405060708090a0b0c0d0e0f10'}
  - {name: snell, type: snell, version: 6, server: example.invalid, port: 443, psk: example-test-psk, mode: default}
  - {name: reality, type: anytls, server: example.invalid, port: 443, password: example, client-fingerprint: chrome, sni: example.invalid, reality-opts: {public-key: 'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8', short-id: '0123456789abcdef'}}
rules: ["MATCH,DIRECT"]
YAML
"$binary" -t -d "$home" -f "$home/config.yaml"
