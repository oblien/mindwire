# syntax=docker/dockerfile:1
# Build context: repository root.
FROM node:25-bookworm AS builder
COPY --from=golang:1.26.9-bookworm /usr/local/go /usr/local/go
COPY --from=oven/bun:1.4.3 /usr/local/bin/bun /usr/local/bin/bun
# Bun installs dependencies; Node runs the SDK/Next tools, and Go generates SDK docs.
# Without Node, Bun silently substitutes its own runtime for node-based scripts.
ENV PATH=/usr/local/go/bin:${PATH}
RUN node --version && bun --version && go version
ARG NEXT_PUBLIC_CONSOLE_URL=https://console.mindwire.sh
ARG NEXT_PUBLIC_IOS_APP_URL=
ENV NEXT_PUBLIC_CONSOLE_URL=${NEXT_PUBLIC_CONSOLE_URL}
ENV NEXT_PUBLIC_IOS_APP_URL=${NEXT_PUBLIC_IOS_APP_URL}
WORKDIR /app

COPY package.json bun.lock bunfig.toml ./
COPY packages/sdk/package.json packages/sdk/
COPY apps/web/package.json apps/web/
COPY apps/console/package.json apps/console/
RUN bun install --frozen-lockfile

COPY . .
RUN bun --filter='mindwire' run build \
 && bun --filter='@mindwire/web' run build

FROM node:25-bookworm-slim AS runner
WORKDIR /app
ENV NODE_ENV=production \
    PORT=4327 \
    HOSTNAME=0.0.0.0
# The Node user owns Next's cache and prerendered pages so pricing can revalidate.
COPY --from=builder --chown=node:node /app/apps/web/.next/standalone ./
COPY --from=builder --chown=node:node /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=builder --chown=node:node /app/apps/web/public ./apps/web/public
USER node
EXPOSE 4327
CMD ["node", "apps/web/server.js"]
