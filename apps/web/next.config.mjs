import { createMDX } from "fumadocs-mdx/next";
import path from "node:path";
import { fileURLToPath } from "node:url";

const withMDX = createMDX();
const repoRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), "../..");

/** @type {import('next').NextConfig} */
const nextConfig = {
	output: "standalone",
	outputFileTracingRoot: repoRoot,
  // trailingSlash is kept: the marketing pages' internal links rely on it, and Fumadocs supports it.
  // `output: "export"` is intentionally gone — Fumadocs docs (Orama search, llms.txt, dynamic OG, the
  // REST "try it" proxy) need a running server, so the site deploys as a Node/edge app now.
  trailingSlash: true,
  // Pretty per-page markdown: `/docs/<slug>.md` → the app/api/md/[[...slug]] handler. `.md` has a file
  // extension so trailingSlash leaves it alone; `/docs.md` (the index) needs its own entry since the
  // catch-all rewrite requires the `/docs/` prefix. Same suffix-rewrite shape Fumadocs documents for .mdx.
  async rewrites() {
    return [
      { source: "/docs.md", destination: "/api/md" },
      { source: "/docs/:path*.md", destination: "/api/md/:path*" },
    ];
  },
};

export default withMDX(nextConfig);
