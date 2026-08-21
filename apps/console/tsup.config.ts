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
  // Keep native/runtime imports as authored; the Node server resolves external dependencies from its
  // workspace install in the production image.
  removeNodeProtocol: false,
});
