# syntax=docker/dockerfile:1
# Build context: repository root.
FROM golang:1.26-bookworm AS builder
WORKDIR /src/daemon
COPY daemon/go.mod daemon/go.sum ./
RUN go mod download
COPY daemon/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mindwired ./cmd/daemon

FROM node:22-slim
COPY --from=builder /out/mindwired /usr/local/bin/mindwired
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /home/node/.npm-global \
 && chown -R node:node /home/node
ENV HOME=/home/node \
    NPM_CONFIG_PREFIX=/home/node/.npm-global \
    PATH=/home/node/.npm-global/bin:${PATH}
EXPOSE 8790
USER node
ENTRYPOINT ["/usr/local/bin/mindwired"]
