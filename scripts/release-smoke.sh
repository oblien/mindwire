#!/usr/bin/env bash
#
# Release smoke gate for the `mindwire` SDK + its bundled daemon.
#
# The unit/e2e suite exercises the SDK against sources. It does NOT prove the PUBLISHED shape installs and
# boots: that `mindwire` + the host `mindwire-daemon-<os>-<arch>` package resolve as a real `npm install`,
# and that `startEmbedded()` actually spawns the daemon binary and serves. This gate packs both, installs
# the tarballs into a throwaway project with no ambient node_modules, and boots the daemon through the
# SDK's public API — hitting /healthz and /catalog. Any failure exits non-zero so the release stops
# BEFORE the immutable npm publish.
#
# Prerequisites (built by the release workflow's prior steps): packages/sdk/dist and the per-platform
# packages/sdk/npm/mindwire-daemon-* dirs. Locally, build them first — or run with SMOKE_BUILD=1 to have
# this script build them (needs the Go toolchain + bun).
#
# Usage:  bash scripts/release-smoke.sh
#   SMOKE_BUILD=1   (re)build the SDK dist + daemon packages before packing (needs Go + bun)
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
FAILED=0

pass()  { printf '  \033[32m✓\033[0m %s\n' "$1"; }
fail()  { printf '  \033[31m✗\033[0m %s\n' "$1"; FAILED=1; }
group() { printf '\n\033[1m%s\033[0m\n' "$1"; }

# ── host → daemon package ─────────────────────────────────────────────────
# The daemon ships one native binary per platform. Boot the package that matches THIS host, so the smoke
# runs on the linux-x64 CI runner and on a maintainer's macOS/arm64 box alike.
os="$(uname -s)"; arch="$(uname -m)"
case "$os" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo "release-smoke: unsupported OS '$os' (linux/darwin only)"; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64)  ARCH=x64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) echo "release-smoke: unsupported arch '$arch' (x86_64/arm64 only)"; exit 1 ;;
esac
DAEMON_DIR="packages/sdk/npm/mindwire-daemon-${OS}-${ARCH}"

# ── optional build ────────────────────────────────────────────────────────
if [ "${SMOKE_BUILD:-}" = "1" ]; then
  group "Building the daemon packages + SDK (SMOKE_BUILD=1)…"
  MINDWIRE_RELEASE=1 bun --filter='mindwire' run build:daemon >/dev/null 2>&1 \
    || { echo "build:daemon failed"; exit 1; }
  bun --filter='mindwire' run build >/dev/null 2>&1 \
    || { echo "SDK build failed"; exit 1; }
fi

# ── preconditions ──────────────────────────────────────────────────────────
group "Preconditions"
if [ -d packages/sdk/dist ]; then
  pass "SDK dist present"
else
  fail "no packages/sdk/dist — build the SDK first (bun --filter=mindwire run build), or set SMOKE_BUILD=1"
fi
if [ -d "$DAEMON_DIR" ]; then
  pass "host daemon package present ($DAEMON_DIR)"
else
  fail "no $DAEMON_DIR — build the daemon packages first (MINDWIRE_RELEASE=1 bun --filter=mindwire run build:daemon), or set SMOKE_BUILD=1"
fi
for tool in node npm; do
  command -v "$tool" >/dev/null 2>&1 && pass "$tool on PATH" || fail "$tool not on PATH"
done
# Nothing below can work without the artifacts + tools — bail with the verdict rather than erroring out.
if [ "$FAILED" -ne 0 ]; then
  echo
  printf '\033[31m✗ release smoke FAILED — preconditions unmet, do not publish.\033[0m\n'
  exit 1
fi

# ── pack ────────────────────────────────────────────────────────────────────
group "Packaging the SDK + host daemon package"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
SDK_TGZ="$(cd packages/sdk && npm pack --silent | tail -n1)" \
  && pass "packed mindwire → $SDK_TGZ" || fail "npm pack (SDK) failed"
DAEMON_TGZ="$(cd "$DAEMON_DIR" && npm pack --silent | tail -n1)" \
  && pass "packed $(basename "$DAEMON_DIR") → $DAEMON_TGZ" || fail "npm pack (daemon) failed"
if [ "$FAILED" -ne 0 ]; then
  echo; printf '\033[31m✗ release smoke FAILED — could not pack the tarballs.\033[0m\n'; exit 1
fi
cp "packages/sdk/$SDK_TGZ" "$WORK/"
cp "$DAEMON_DIR/$DAEMON_TGZ" "$WORK/"

# ── install into a throwaway project ─────────────────────────────────────────
group "Installing the tarballs into a throwaway project"
(
  cd "$WORK"
  npm init -y >/dev/null 2>&1
  # Install the daemon package first so the SDK's optionalDependency resolves to the local tarball.
  npm install --no-audit --no-fund "./$DAEMON_TGZ" "./$SDK_TGZ" >/dev/null 2>&1
) && pass "npm install resolved both tarballs" || fail "npm install failed"
if [ "$FAILED" -ne 0 ]; then
  echo; printf '\033[31m✗ release smoke FAILED — the packaged shape did not install.\033[0m\n'; exit 1
fi

# ── boot through the SDK ─────────────────────────────────────────────────────
group "Booting the daemon through the SDK (startEmbedded → /healthz + /catalog)"
cat > "$WORK/smoke.mjs" <<'EOF'
import { startEmbedded } from "mindwire";
const d = await startEmbedded();
try {
  const res = await fetch(`${d.baseUrl}/healthz`);
  if (!res.ok) throw new Error(`/healthz ${res.status}`);
  const body = await res.json();
  if (!body.ok) throw new Error(`unhealthy: ${JSON.stringify(body)}`);
  const cat = await (await fetch(`${d.baseUrl}/catalog`)).json();
  const ids = (cat.agents ?? []).map((a) => a.id);
  if (!ids.includes("claude-code")) throw new Error(`catalog missing claude-code: ${JSON.stringify(ids)}`);
  console.log(`version ${body.version} — agents ${ids.join(",")}`);
} finally {
  d.stop();
}
EOF
if out="$(cd "$WORK" && node smoke.mjs 2>&1)"; then
  pass "daemon booted, /healthz.ok, /catalog has claude-code — $out"
else
  fail "startEmbedded smoke failed:"
  printf '%s\n' "$out" | sed 's/^/      /'
fi

# ── verdict ──────────────────────────────────────────────────────────────────
echo
if [ "$FAILED" -eq 0 ]; then
  printf '\033[32m✔ release smoke passed — the published SDK + host daemon package install and boot.\033[0m\n'
else
  printf '\033[31m✗ release smoke FAILED — do not publish.\033[0m\n'
  exit 1
fi
