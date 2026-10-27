import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";

// The root header owns search. Fumadocs supplies the desktop sidebar and its mobile browse menu.
export const baseOptions: BaseLayoutProps = {
  nav: { title: "Browse documentation", url: "/docs" },
  searchToggle: { enabled: false },
  themeSwitch: { enabled: false },
};
