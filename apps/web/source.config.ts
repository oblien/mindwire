import { defineDocs, defineConfig } from "fumadocs-mdx/config";

// The single docs collection: every .md/.mdx under content/docs (hand-written + the four generated
// reference trees). Frontmatter uses the built-in page schema (title/description/icon/full).
export const docs = defineDocs({
  dir: "content/docs",
});

export default defineConfig();
