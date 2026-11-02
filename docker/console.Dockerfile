# syntax=docker/dockerfile:1
# Source deployment for Mindwire's hosted account and Cloud management service.
# This image is built locally by Compose, never published to GHCR.
FROM node:24-bookworm AS builder
COPY --from=oven/bun:1.4.3 /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /app

# Manifests first, so `bun install` is cached until a package.json or the lockfile actually changes.
# All workspace members must be present for a frozen install to resolve the lockfile.
COPY package.json bun.lock bunfig.toml ./
COPY patches/ patches/
COPY packages/sdk/package.json packages/sdk/
COPY apps/console/package.json apps/console/
COPY apps/web/package.json apps/web/
# Use a self-contained, hoisted install for the image. Copying the isolated
# workspace layout can leave dangling links into node_modules/.bun in the runner.
# Native dependencies use the same Node version and glibc as the runner.
# better-sqlite3 13 ships prebuilt binaries and sets gypfile=false. Skip Bun's
# implicit node-gyp lifecycle; prepare esbuild explicitly with Node instead.
RUN bun install --frozen-lockfile --linker hoisted --ignore-scripts \
 && npm rebuild esbuild

# Now the sources (node_modules/dist are excluded by .dockerignore, so the installed deps survive).
COPY . .

RUN bun --filter='@mindwire/console' run build

# ---- runner: a slim Node 24 image carrying only what the console needs at runtime ----------------------
FROM node:24-bookworm-slim AS runner
WORKDIR /app
ENV NODE_ENV=production \
    PORT=8787 \
    AUTH_DB_PATH=/data/auth.db

# Copy the hoisted dependency tree, assigning the runtime user ownership even when a
# native package or its cache directory was installed with private permissions.
COPY --from=builder /app/package.json ./package.json
COPY --from=builder --chown=node:node /app/node_modules ./node_modules
COPY --from=builder /app/apps/console/package.json ./apps/console/package.json
COPY --from=builder --chown=node:node /app/apps/console/node_modules ./apps/console/node_modules
COPY --from=builder /app/apps/console/dist ./apps/console/dist
COPY --from=builder /app/apps/console/dist-server ./apps/console/dist-server
COPY --from=builder /app/apps/console/drizzle ./apps/console/drizzle

# Run unprivileged. The hosted deployment supplies DATABASE_URL for PostgreSQL.
RUN mkdir -p /data && chown -R node:node /data
USER node
# Verify the copied dependency graph as the runtime user before publishing it.
# The server imports this driver even when PostgreSQL is configured.
RUN node --input-type=module -e 'import Database from "better-sqlite3"; const db = new Database(":memory:"); db.close()'
VOLUME ["/data"]
EXPOSE 8787

# `/api/ping` is one of the few routes waved through the auth gate (see apps/console/server/guard.ts),
# so it is a valid unauthenticated liveness probe.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:'+(process.env.PORT||8787)+'/api/ping').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

# Matches the app's own `start` script exactly.
CMD ["node", "apps/console/dist-server/index.js"]
