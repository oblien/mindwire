# syntax=docker/dockerfile:1
#
# MindWire console (@mindwire/preview) — the control-plane image.
#
# This is a Vite SPA (dist/) plus a Hono/Node server (dist-server/) that drives the mindwire SDK on behalf
# of each signed-in user. It runs as ONE Node process (`node dist-server/index.js`) and needs:
#   - Node >= 22            — Better Auth persists to the `node:sqlite` builtin (kept verbatim by tsup;
#                             see apps/preview/tsup.config.ts). The app's own `start` script runs flagless,
#                             so we match it exactly.
#   - a persistent volume   — for AUTH_DB_PATH (the user/session SQLite), so accounts survive restarts.
#   - the tsup-externalized runtime deps resolvable in node_modules: `mindwire` (the workspace SDK, linked)
#     plus `oblien`/`dockerode`/`ssh2`. We keep bun's install layout intact so those resolve unchanged.
#
# Build context is the repo ROOT (`docker build -f apps/preview/Dockerfile .`) — the console is a bun
# workspace member and needs the root manifests + the SDK it links to.

# ---- builder: install the workspace and build the SDK + console with bun --------------------------------
# Pinned to the repo's packageManager (bun@1.3.1). Debian-based (glibc), same libc as the runner below,
# so any prebuilt node_modules we carry across stages stay ABI-compatible.
FROM oven/bun:1.3.1 AS builder
WORKDIR /app

# Manifests first, so `bun install` is cached until a package.json or the lockfile actually changes.
# All workspace members must be present for a frozen install to resolve the lockfile.
COPY package.json bun.lock ./
COPY packages/sdk/package.json packages/sdk/
COPY apps/preview/package.json apps/preview/
COPY apps/web/package.json apps/web/
# --ignore-scripts: this slim image ships no C/C++ toolchain, so ssh2's OPTIONAL native dep `cpu-features`
# can't compile (`node-gyp` → "Unable to detect compiler type") and would fail the whole install. It's
# optional — ssh2 degrades gracefully without it — and nothing we build or run here relies on a lifecycle
# script: the build tools (esbuild/rollup/lightningcss/@tailwindcss/oxide) and the runtime all use prebuilt
# per-platform binaries that bun links directly, not postinstall compiles.
RUN bun install --frozen-lockfile --ignore-scripts

# Now the sources (node_modules/dist are excluded by .dockerignore, so the installed deps survive).
COPY . .

# Build the SDK first — the console typechecks and bundles against mindwire's built dist/index.d.ts — then
# the console (vite build → dist/, tsup → dist-server/). No Go toolchain needed: this is the SDK's `build`
# (tsup), not `build:daemon`.
RUN bun --filter='mindwire' run build \
 && bun --filter='@mindwire/preview' run build

# ---- runner: a slim Node 22 image carrying only what the console needs at runtime ----------------------
FROM node:22-slim AS runner
WORKDIR /app
ENV NODE_ENV=production \
    PORT=8787 \
    AUTH_DB_PATH=/data/auth.db

# Carry the resolved workspace so the externalized deps resolve via bun's on-disk layout:
#   - root node_modules      — hoisted transitive deps shared across the workspace
#   - packages/sdk           — the SDK the console links to (the `mindwire` symlink resolves here, and it
#                              carries its built dist/)
#   - apps/preview/node_modules — the console's direct deps (mindwire/oblien/dockerode/ssh2 live here)
# We deliberately skip the apps/web tree — the docs site is not part of this image.
COPY --from=builder /app/package.json ./package.json
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/packages/sdk ./packages/sdk
COPY --from=builder /app/apps/preview/package.json ./apps/preview/package.json
COPY --from=builder /app/apps/preview/node_modules ./apps/preview/node_modules
COPY --from=builder /app/apps/preview/dist ./apps/preview/dist
COPY --from=builder /app/apps/preview/dist-server ./apps/preview/dist-server

# Persist the auth SQLite outside the layer, and run unprivileged (the base image ships a `node` user).
RUN mkdir -p /data && chown -R node:node /data
USER node
VOLUME ["/data"]
EXPOSE 8787

# `/api/ping` is one of the few routes waved through the auth gate (see apps/preview/server/guard.ts),
# so it is a valid unauthenticated liveness probe.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:'+(process.env.PORT||8787)+'/api/ping').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

# Matches the app's own `start` script exactly (flagless node — node:sqlite is available on Node 22).
CMD ["node", "apps/preview/dist-server/index.js"]
