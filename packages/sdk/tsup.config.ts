import { defineConfig } from "tsup";
import { createRequire } from "node:module";

// Stamp the build with the package version so the SDK (and the sandbox adapters' version reconcile)
// can compare against a running daemon's /healthz without reading package.json at runtime.
const pkg = createRequire(import.meta.url)("./package.json") as { version: string };

export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm", "cjs"],
  dts: true,
  clean: true,
  sourcemap: true,
  treeshake: true,
  target: "es2022",
  define: {
    __MINDWIRE_VERSION__: JSON.stringify(pkg.version),
  },
  outExtension({ format }) {
    return { js: format === "cjs" ? ".cjs" : ".js" };
  },
});
