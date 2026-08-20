"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useSearchContext } from "fumadocs-ui/contexts/search";
import { Search } from "lucide-react";
import OblienLogo from "./OblienLogo";
import ThemeToggle from "./ThemeToggle";

const links = [
  { href: "/docs", label: "Docs" },
  { href: "/sandboxing", label: "Sandboxing" },
  { href: "/pricing", label: "Pricing" },
  { href: "/changelog", label: "Changelog" },
  // Link to the MindWire integration skill users add to their agents.
  { href: "https://github.com/oblien/mindwire", label: "Agent Skill" },
];

export default function Nav() {
  const pathname = usePathname();
  const onDocs = pathname?.startsWith("/docs") ?? false;
  // RootProvider provides the search context site-wide, so the hook is always safe to call here; the
  // ⌘K trigger is only rendered on /docs (where the Fumadocs sidebar search is disabled).
  const { setOpenSearch } = useSearchContext();

  const link =
    "hidden px-3 py-2 text-sm text-neutral-500 transition-colors hover:text-neutral-950 dark:hover:text-white md:inline-flex";
  const iconBtn =
    "grid h-9 w-9 place-items-center text-neutral-500 transition-colors hover:text-neutral-950 dark:hover:text-white";

  // On /docs the header spans the Fumadocs layout width (--fd-layout-width = 97rem) with left padding
  // matching the sidebar's, so the logo sits above the sidebar and the actions above the TOC — and it
  // gets a clean solid backdrop + hairline divider (the docs grid is structured; the landing page's
  // frameless progressive-blur "twave" would fog the sidebar). Marketing pages keep the max-w-6xl +
  // twave treatment untouched.
  const header = onDocs
    ? "sticky top-0 z-50 border-b border-neutral-200 bg-white/70 backdrop-blur-md dark:border-white/10 dark:bg-[#08080b]/70"
    : "sticky top-0 z-50";
  const inner = onDocs
    ? "relative z-10 mx-auto flex h-16 max-w-[97rem] items-center px-4 xl:px-6"
    : "relative z-10 mx-auto flex h-16 max-w-6xl items-center px-4 sm:px-6";

  return (
    <header className={header}>
      {/* progressive-blur backdrop — content dissolves under the nav, no border (marketing only) */}
      {!onDocs && (
        <div aria-hidden className="twave pointer-events-none absolute inset-x-0 top-0 z-0 h-32">
          <div />
          <div />
          <div />
          <div />
          <div />
          <div />
        </div>
      )}

      <div className={inner}>
        <Link href="/" className="flex items-center gap-2 text-[1.05rem] font-semibold tracking-tight">
          <OblienLogo size={28} /> MindWire
        </Link>

        <nav className="ml-auto flex items-center gap-0.5">
          {/* Docs search trigger — opens Fumadocs' ⌘K dialog (sidebar search is disabled on /docs). */}
          {onDocs && (
            <button
              type="button"
              onClick={() => setOpenSearch(true)}
              aria-label="Search docs"
              className="mr-3 hidden w-72 items-center justify-between border border-neutral-200 bg-neutral-50 py-2 pl-3 pr-2 text-sm text-neutral-500 transition-colors hover:border-neutral-300 hover:text-neutral-950 md:flex dark:border-white/10 dark:bg-white/[0.03] dark:text-neutral-400 dark:hover:border-white/20 dark:hover:text-white"
            >
              <span className="flex items-center gap-2">
                <Search size={16} aria-hidden />
                Search
              </span>
              <kbd className="border border-neutral-200 bg-white px-1.5 py-0.5 font-mono text-[11px] leading-none text-neutral-400 dark:border-white/10 dark:bg-white/[0.04] dark:text-neutral-500">
                ⌘K
              </kbd>
            </button>
          )}
          {links.map((l) => (
            <Link key={l.href} href={l.href} className={link}>
              {l.label}
            </Link>
          ))}
          <a href="https://github.com/oblien/mindwire" rel="noopener" aria-label="GitHub" className={iconBtn}>
            <svg width="17" height="17" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.3.8-.6v-2c-3.2.7-3.9-1.5-3.9-1.5-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.7-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.7 18.3.5 12 .5z" />
            </svg>
          </a>
          <ThemeToggle />
          <Link href="/docs" className="btn btn-primary ml-2 px-4 py-2 text-[13px]">
            Start building
          </Link>
        </nav>
      </div>
    </header>
  );
}
