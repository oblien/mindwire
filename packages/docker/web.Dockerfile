# syntax=docker/dockerfile:1
# Build context: repository root.
FROM golang:1.26.9-bookworm AS builder
ARG NEXT_PUBLIC_CONSOLE_URL=https://console.mindwire.sh
ARG NEXT_PUBLIC_IOS_APP_URL=
ENV NEXT_PUBLIC_CONSOLE_URL=${NEXT_PUBLIC_CONSOLE_URL}
ENV NEXT_PUBLIC_IOS_APP_URL=${NEXT_PUBLIC_IOS_APP_URL}
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates unzip \
 && rm -rf /var/lib/apt/lists/* \
 && curl -fsSL https://bun.sh/install | bash
ENV PATH="/root/.bun/bin:${PATH}"
WORKDIR /app

COPY package.json bun.lock ./
COPY packages/sdk/package.json packages/sdk/
COPY apps/web/package.json apps/web/
COPY apps/console/package.json apps/console/
RUN bun install --frozen-lockfile

COPY . .
RUN bun --filter='mindwire' run build \
 && bun --filter='@mindwire/web' run build

FROM node:22-slim AS runner
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
