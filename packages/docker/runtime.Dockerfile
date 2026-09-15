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
 && npm install -g @anthropic-ai/claude-code @openai/codex @xai-official/grok opencode-ai \
 && mkdir -p /home/node/.npm-global /home/node/.mindwire /usr/share/mindwire \
 && chown -R node:node /home/node

# npm resolves OpenCode's native platform package without the shell installer's
# unauthenticated GitHub latest-release lookup. Check every CLI on each build arch;
# command substitutions inside printf previously hid failed version commands.
RUN node <<'JS'
const { execFileSync } = require('node:child_process');
const { writeFileSync } = require('node:fs');
const commands = {
  claudeCode: ['claude', '--version'],
  codex: ['codex', '--version'],
  grok: ['grok', 'version'],
  opencode: ['opencode', '--version'],
};
const versions = {};
for (const [name, [command, ...args]] of Object.entries(commands)) {
  const version = execFileSync(command, args, { encoding: 'utf8', timeout: 30000 }).trim().split(/\r?\n/)[0];
  if (!version) throw new Error(`${name} returned an empty version`);
  versions[name] = version;
  console.log(`${name}: ${version}`);
}
writeFileSync('/usr/share/mindwire/agents.json', JSON.stringify(versions) + '\n');
JS
ENV HOME=/home/node \
    NPM_CONFIG_PREFIX=/home/node/.npm-global \
    PATH=/home/node/.npm-global/bin:${PATH} \
    ADDR=:8790 \
    STATE_PATH=/home/node/.mindwire/agent-state.json
WORKDIR /home/node
EXPOSE 8790
USER node
ENTRYPOINT ["/usr/local/bin/mindwired"]
