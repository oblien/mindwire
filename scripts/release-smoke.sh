#!/usr/bin/env bash
#
# Release smoke gate for the published `mindwire` SDK.
#
# npm distributes one package: `mindwire`. Daemon binaries ship separately as GitHub Release assets and
# Docker images, so this verifies the exact consumer path: pack the SDK, install it in an empty project,
# and load its public ESM API without reaching into this monorepo's node_modules.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

pass() { printf '  \033[32m✓\033[0m %s\n' "$1"; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; exit 1; }

if [ ! -d packages/sdk/dist ]; then
  fail "no packages/sdk/dist — build the SDK before running this smoke test"
fi
command -v node >/dev/null 2>&1 || fail "node is not on PATH"
command -v npm >/dev/null 2>&1 || fail "npm is not on PATH"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# Keep npm fully isolated from a developer or runner's shared cache/credentials.
export npm_config_cache="$WORK/npm-cache"

# `--json` gives the filename without being confused by the package's prepack build output. The workflow
# builds first, so `--ignore-scripts` keeps this smoke focused on the already-built publish artifact.
TGZ="$(cd packages/sdk && npm pack --ignore-scripts --json | node -e 'let data=""; process.stdin.on("data", c => data += c); process.stdin.on("end", () => console.log(JSON.parse(data)[0].filename));')"
cp "packages/sdk/$TGZ" "$WORK/"
pass "packed $TGZ"

(
  cd "$WORK"
  npm init -y >/dev/null
  npm install --no-audit --no-fund "./$TGZ" >/dev/null
  node --input-type=module <<'EOF'
import * as mindwire from "mindwire";
if (typeof mindwire.Mindwire !== "function") {
  throw new Error("mindwire package did not expose Mindwire");
}
EOF
) && pass "installed and loaded the SDK in an empty project" || fail "packaged SDK smoke failed"

printf '\033[32m✔ SDK release smoke passed.\033[0m\n'
