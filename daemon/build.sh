#!/usr/bin/env bash
# Cross-compile mindwired as static (CGO-off) binaries, one per sandbox
# platform, plus the static catalog.json — into a single rolling "latest" channel
# laid out exactly like the release host, so publishing is a straight copy of dist/latest/.
#
#   ./build.sh                 # linux amd64 + arm64 (the cloud sandbox targets)
#   PLATFORMS="linux/amd64" ./build.sh   # subset
#
# Output mirrors what the app downloads (<base>/latest/...):
#   dist/latest/mindwired-<os>-<arch>
#   dist/latest/catalog.json   (embeds agent.Version — the app's update trigger)
# Rebuilding IS the release: it overwrites the channel in place. Bump agent.Version when a change
# should force connected clients to re-pull — the app reads catalog.json's version at runtime and
# updates each sandbox to it automatically (no app release needed).
set -euo pipefail
cd "$(dirname "$0")"

VERSION=$(grep -oE 'Version = "[^"]+"' internal/agent/agent.go | head -1 | grep -oE '"[^"]+"' | tr -d '"')
PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64}"
PKG="./cmd/daemon"
OUT="dist/latest"

mkdir -p "$OUT"
echo "building mindwired v${VERSION} → ${OUT} (latest channel)"

for plat in $PLATFORMS; do
  os="${plat%/*}"; arch="${plat#*/}"
  out="${OUT}/mindwired-${os}-${arch}"
  echo "  → ${out}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w" -o "$out" "$PKG"
done

# Catalog is platform-independent JSON — emit it with the host toolchain.
echo "  → ${OUT}/catalog.json"
go run "$PKG" --print-catalog > "${OUT}/catalog.json"

# Publish a tracked copy at the repo root too: the docs site's agents reference reads this
# (committed) file, so the web build needs no Go toolchain.
cp "${OUT}/catalog.json" catalog.json

echo "done. ${OUT}/:"
ls -lh "$OUT"
