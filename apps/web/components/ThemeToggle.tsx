"use client";

import { useTheme } from "next-themes";

/**
 * Icons and accessible names follow the pre-paint theme class. Rendering both
 * avoids a hydration placeholder or a flash of the wrong icon on saved visits.
 */
export default function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  const toggle = () => {
    const dark = resolvedTheme
      ? resolvedTheme === "dark"
      : document.documentElement.classList.contains("dark");
    setTheme(dark ? "light" : "dark");
  };

  const icon = "h-[18px] w-[18px]";

  return (
    <button
      onClick={toggle}
      type="button"
      className="grid h-10 w-10 shrink-0 place-items-center rounded-full text-[var(--mw-muted)] hover:bg-[var(--mw-subtle)] hover:text-[var(--mw-ink)]"
      data-theme-toggle
    >
      <span className="sr-only hidden dark:block">Switch to light mode</span>
      <span className="sr-only dark:hidden">Switch to dark mode</span>
      <svg
        className={`${icon} hidden dark:block`}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
      <svg
        className={`${icon} dark:hidden`}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        aria-hidden="true"
      >
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
    </button>
  );
}
