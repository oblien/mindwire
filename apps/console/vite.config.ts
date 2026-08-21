import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// The SPA is served same-origin with the Hono API in production (Hono static-serves `dist/`). In dev,
// Vite owns the browser at :5174 and proxies the two server surfaces — the JSON API (`/api`) and the
// SSE turn stream (`/events`) — to the Hono process at :8787. The browser therefore only ever talks to
// one origin, and never imports the SDK directly: all `mindwire` usage lives behind Hono in `server/`.
const SERVER_ORIGIN = process.env.SERVER_ORIGIN || "http://127.0.0.1:8787";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@shared": fileURLToPath(new URL("./shared", import.meta.url)),
    },
  },
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      "/api": { target: SERVER_ORIGIN, changeOrigin: true },
      // SSE — disable buffering by keeping it a plain HTTP proxy (no ws upgrade needed).
      "/events": { target: SERVER_ORIGIN, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
