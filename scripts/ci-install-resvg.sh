#!/bin/sh
# Installs the pinned resvg binary the fidelity harness renders with.
# Single source of truth for both CIs: version and checksum live here only.
set -eu

RESVG_VERSION="0.47.0"
RESVG_SHA256="5c84dcbcd032fe7e8d96e616fd6807a2f9df6561d2e6582b37e91e63c6cb4fe7"

url="https://github.com/linebender/resvg/releases/download/v${RESVG_VERSION}/resvg-linux-x86_64.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsSL --retry 3 --retry-all-errors -o "$tmp/resvg.tar.gz" "$url"
echo "${RESVG_SHA256}  $tmp/resvg.tar.gz" | sha256sum -c -
tar -xzf "$tmp/resvg.tar.gz" -C "$tmp"
install -m755 "$tmp/resvg" "${1:-/usr/local/bin}/resvg"
resvg --version
