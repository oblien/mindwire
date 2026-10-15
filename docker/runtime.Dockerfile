# syntax=docker/dockerfile:1
# Build context: repository root. An agent workspace with the runtime and pinned harnesses.
FROM golang:1.27.2-bookworm AS builder
WORKDIR /src/packages/daemon
COPY packages/daemon/go.mod packages/daemon/go.sum ./
RUN go mod download
COPY packages/daemon/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mindwired ./cmd/daemon \
 && /out/mindwired --print-toolchains > /out/toolchains.json

FROM node:24-bookworm-slim
COPY --from=builder /out/mindwired /usr/local/bin/mindwired
COPY --from=builder /out/toolchains.json /usr/share/mindwire/toolchains.json
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl git ca-certificates bubblewrap \
 && bwrap --version \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /home/node/.npm-global /home/node/.mindwire /usr/share/mindwire \
 && chown -R node:node /home/node

# npm resolves OpenCode's native platform package without the shell installer's
# unauthenticated GitHub latest-release lookup. Check every CLI on each build arch;
# command substitutions inside printf previously hid failed version commands.
RUN node <<'JS'
const { execFileSync } = require('node:child_process');
const { readFileSync, writeFileSync } = require('node:fs');
const plan = JSON.parse(readFileSync('/usr/share/mindwire/toolchains.json', 'utf8'));
const versions = {};
for (const item of plan) {
  execFileSync('npm', ['install', '-g', '--no-audit', '--no-fund', '--registry', 'https://registry.npmjs.org', `${item.package}@${item.version}`], { stdio: 'inherit' });
  const version = execFileSync(item.binary, item.versionArgs, { encoding: 'utf8', timeout: 30000, env: { ...process.env, ...item.environment } }).trim().split(/\r?\n/)[0];
  const actual = version.match(/\b\d+\.\d+\.\d+(?:-[\w.-]+)?\b/)?.[0];
  if (actual !== item.version) throw new Error(`${item.agent} installed ${actual}, expected ${item.version}`);
  versions[item.agent] = version;
  console.log(`${item.agent}: ${version}`);
}
writeFileSync('/usr/share/mindwire/agents.json', JSON.stringify(versions) + '\n');
JS
ENV HOME=/home/node \
    NPM_CONFIG_PREFIX=/home/node/.npm-global \
    PATH=/home/node/.npm-global/bin:${PATH} \
    ADDR=:8790 \
    MINDWIRE_ISOLATION=container \
    DISABLE_AUTOUPDATER=1 \
    OPENCODE_DISABLE_AUTOUPDATE=true \
    STATE_PATH=/home/node/.mindwire/agent-state.json
WORKDIR /home/node
EXPOSE 8790
USER node
HEALTHCHECK --interval=30s --timeout=6s --start-period=20s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:8790/healthz',{headers:{Authorization:'Bearer '+process.env.DAEMON_TOKEN},signal:AbortSignal.timeout(5000)}).then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
ENTRYPOINT ["/usr/local/bin/mindwired"]
