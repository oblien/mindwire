# syntax=docker/dockerfile:1
# Build context: repository root. This is the production-ready runtime image, not merely the daemon
# binary: it carries mindwired plus the supported coding-agent CLIs so first use never installs them.
FROM golang:1.26-bookworm AS builder
WORKDIR /src/daemon
COPY daemon/go.mod ./
RUN go mod download
COPY daemon/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mindwired ./cmd/daemon

FROM node:22-slim
COPY --from=builder /out/mindwired /usr/local/bin/mindwired
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl git ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code @openai/codex \
 && curl -fsSL https://opencode.ai/install | bash \
 && install -m 0755 /root/.opencode/bin/opencode /usr/local/bin/opencode \
 && mkdir -p /home/node/.npm-global /home/node/.mindwire /usr/share/mindwire \
 && printf '{"claudeCode":"%s","codex":"%s","opencode":"%s"}\n' "$(claude --version | head -n1 | sed 's/"/\\\\"/g')" "$(codex --version | head -n1 | sed 's/"/\\\\"/g')" "$(opencode --version | head -n1 | sed 's/"/\\\\"/g')" > /usr/share/mindwire/agents.json \
 && chown -R node:node /home/node
ENV HOME=/home/node \
    NPM_CONFIG_PREFIX=/home/node/.npm-global \
    PATH=/home/node/.npm-global/bin:${PATH} \
    ADDR=:8790 \
    STATE_PATH=/home/node/.mindwire/agent-state.json
WORKDIR /home/node
EXPOSE 8790
USER node
ENTRYPOINT ["/usr/local/bin/mindwired"]
