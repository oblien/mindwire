// Generates the Agents overview page from the daemon's catalog.json into content/docs/reference/agents/
// index.mdx. catalog.json carries id/name/tagline + version; live capabilities/settings/auth are runtime
// (GET /agent), so we link there rather than invent a static matrix. The hand-written per-agent pages
// (claude.mdx, codex.mdx) and meta.json in the same folder are NOT touched — only index.mdx is written.
// Generates from the daemon source when Go is available; falls back to a locally built dist channel for
// docs-only environments. The catalog is a build artifact, never a committed source file.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const daemonDir = path.resolve("../../daemon");
const builtCatalog = path.join(daemonDir, "dist/latest/catalog.json");
const OUT = path.resolve("content/docs/reference/agents");
let raw;

try {
  raw = execFileSync("go", ["run", "./cmd/daemon", "--print-catalog"], { cwd: daemonDir, encoding: "utf8" });
  console.log("gen-catalog-docs: generated catalog from daemon source");
} catch {
  if (!fs.existsSync(builtCatalog)) {
    console.log("gen-catalog-docs: Go and a built daemon catalog are unavailable — skipping");
    process.exit(0);
  }
  raw = fs.readFileSync(builtCatalog, "utf8");
  console.log("gen-catalog-docs: using local daemon dist catalog");
}

const catalog = JSON.parse(raw);
const agents = catalog.agents ?? [];
const esc = (s) => (s ?? "").replace(/\|/g, "\\|");

fs.mkdirSync(OUT, { recursive: true });

const rows = agents
  .map((a) => `| \`${a.id}\` | ${esc(a.name)} | ${esc(a.tagline)} |`)
  .join("\n");

const md = `---
title: Agents
description: Coding agents this daemon build can drive.
---

Agents this daemon build supports. Select one per request with \`?agent=<id>\` (SDKs: \`new Mindwire({ agent })\` in TypeScript, \`mindwire.New(mindwire.Options{Agent: "<id>"})\` in Go). Each agent's **live** capabilities, settings schema, and auth methods are served at runtime by [\`GET /agent\`](/docs/reference/rest/agent/get-agent/).

## Supported agents

| ID | Name | Description |
|---|---|---|
${rows || "| _(none)_ | | |"}

Daemon definitions version: \`${catalog.version ?? "?"}\`.

## Adding an agent

Each agent is a drop-in adapter under \`daemon/internal/agent/<id>/\`. See [Architecture](/docs/concepts/architecture/) and [Daemon internals](/docs/concepts/internals/) for the adapter contract.
`;

fs.writeFileSync(path.join(OUT, "index.mdx"), md);

console.log(`gen-catalog-docs: wrote Agents overview (${agents.length} agent(s)) to content/docs/reference/agents/index.mdx`);
