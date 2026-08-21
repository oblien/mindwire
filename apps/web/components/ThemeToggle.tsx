"use client";

import { useTheme } from "next-themes";

/**
 * The `.dark` class on <html> is the single source of truth, now owned by next-themes (via the
 * RootProvider in the root layout). Which icon shows is still decided purely by CSS off that class
 * (dark:hidden / hidden dark:block), so there is no state to hydrate and no icon flicker on load —
 * this button only flips the theme.
 */
export default function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  const toggle = () => setTheme(resolvedTheme === "dark" ? "light" : "dark");

  const icon = "h-[15px] w-[15px]";

  return (
    <button
      onClick={toggle}
      aria-label="Toggle theme"
      className="grid h-9 w-9 place-items-center text-neutral-500 transition-colors hover:text-neutral-950 dark:hover:text-white"
    >
      <svg className={`${icon} dark:hidden`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
      <svg className={`${icon} hidden dark:block`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
    </button>
  );
}
