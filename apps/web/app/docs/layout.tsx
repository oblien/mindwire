import { DocsLayout } from "fumadocs-ui/layouts/docs";
import type { ReactNode } from "react";
import { source } from "@/lib/source";
import { baseOptions } from "@/app/layout.config";

// The docs shell: Fumadocs' sidebar + TOC around the page. Rendered inside the root layout, so the
// shared Nav stays (the Footer is hidden on /docs); Fumadocs' own navbar is disabled via baseOptions.
// defaultOpenLevel: 1 expands the three top-level sections (Guides, Concepts, Reference) by default so
// more of the tree is visible at a glance, while the deep reference sub-trees (sdk-ts/rest/…, depth 2)
// stay collapsed rather than dumping ~100 pages into the sidebar.
export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsLayout tree={source.pageTree} {...baseOptions} sidebar={{ defaultOpenLevel: 1 }}>
      {children}
    </DocsLayout>
  );
}
