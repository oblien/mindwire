import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";

// Shared layout options for Fumadocs. The site's own <Nav> (in the root layout) is the global header,
// so Fumadocs' built-in navbar is disabled — the DocsLayout contributes only the sidebar + TOC.
// The sidebar's own search trigger and theme switch are disabled too: search lives in the Nav (a
// ⌘K trigger wired to Fumadocs' search context) and the Nav already owns the theme toggle. Removing
// both collapses the sidebar's footer row, so the sidebar is just clean, full-height navigation.
export const baseOptions: BaseLayoutProps = {
  nav: { enabled: false },
  searchToggle: { enabled: false },
  themeSwitch: { enabled: false },
};
