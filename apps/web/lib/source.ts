import { docs } from "@/.source/server";
import { loader } from "fumadocs-core/source";

// The content-source adapter: turns the generated MDX collection into a tree + page lookups, rooted at
// /docs. `.source/server` is the RSC entry generated from source.config.ts by the `fumadocs-mdx` CLI
// (postinstall / the `fumadocs-mdx` step before `next dev`/`next build`).
export const source = loader({
  baseUrl: "/docs",
  source: docs.toFumadocsSource(),
});

// fumadocs-core's `loader` can't recover the compiled-MDX page shape from a StaticSource at the type
// level: VirtualFile<Config> references Config only through indexed access (Config['pageData']), which
// TypeScript cannot invert, so getPage().data widens to the base PageData. The runtime value is always
// the full compiled doc — this alias re-attaches its real type (body/toc/full + frontmatter) so the
// page renderer stays type-safe. It resolves to the generated collection's element (DocCollectionEntry).
export type DocPage = (typeof docs.docs)[number];
