import { defineConfig } from "tsup";

// Bundle the Hono server to a single ESM file for deployment. Everything in `dependencies` stays
// external (resolved from node_modules at runtime) — critically the SDK's Node-only optional peers
// (`oblien`/`dockerode`/`ssh2`), which must never be inlined here. `node server` then serves the
// built SPA (`dist/`) plus the JSON API and SSE stream from one process.
export default defineConfig({
  entry: ["server/index.ts"],
  outDir: "dist-server",
  format: ["esm"],
  platform: "node",
  target: "node22",
  clean: true,
  sourcemap: true,
  // Never bundle the SDK's optional native peers; keep the SDK itself external too.
  external: ["oblien", "dockerode", "ssh2", "mindwire"],
  // tsup defaults this to `true`, which rewrites `node:sqlite` → a bogus bare `sqlite` import that fails
  // at runtime. The auth store depends on the real Node 22 builtin, so keep the `node:` prefix verbatim.
  removeNodeProtocol: false,
});
