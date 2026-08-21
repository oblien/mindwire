import Link from "next/link";
import OblienLogo from "./OblienLogo";

type FooterLink = { href: string; label: string; soon?: boolean };

const cols: { title: string; links: FooterLink[] }[] = [
  {
    title: "Product",
    links: [
      { href: "/#how", label: "How it works" },
      { href: "/#console", label: "Console" },
      { href: "/sandboxing", label: "Sandboxing" },
      { href: "/pricing", label: "Pricing" },
      { href: "/changelog", label: "Changelog" },
      { href: "/docs/guides/quickstart", label: "Quickstart" },
    ],
  },
  {
    title: "Agents",
    links: [
      { href: "/docs", label: "Claude Code" },
      { href: "/docs", label: "Codex" },
      { href: "/docs", label: "Grok Build" },
      { href: "/docs", label: "GitHub Copilot", soon: true },
      { href: "/docs/reference/agents/opencode", label: "opencode" },
    ],
  },
  {
    title: "Project",
    links: [
      { href: "https://github.com/oblien/mindwire", label: "GitHub" },
      { href: "https://github.com/oblien/mindwire/blob/main/CONTRIBUTING.md", label: "Contributing" },
      { href: "/docs", label: "Documentation" },
    ],
  },
];

const linkCls =
  "text-sm text-neutral-500 transition-colors hover:text-neutral-950 dark:hover:text-white";

export default function Footer() {
  return (
    <footer className="mt-24 border-t border-neutral-200 dark:border-neutral-800">
      <div className="mx-auto grid max-w-6xl grid-cols-2 gap-x-8 gap-y-10 px-4 py-14 sm:px-6 md:grid-cols-[1.5fr_1fr_1fr_1fr]">
        <div className="col-span-2 md:col-span-1">
          <div className="flex items-center gap-2.5 text-[1.15rem] font-semibold tracking-tight">
            <OblienLogo size={28} /> MindWire
          </div>
          <p className="mt-4 max-w-[30ch] text-sm leading-relaxed text-neutral-500">
            Drive the best coding agents from your product. Open source, self-hostable,
            Apache&nbsp;2.0.
          </p>
          <a
            href="https://oblien.com"
            rel="noopener"
            className="mt-5 inline-flex items-center gap-1.5 text-sm text-neutral-500 transition-colors hover:text-neutral-950 dark:hover:text-white"
          >
            An <span className="font-medium text-neutral-800 dark:text-neutral-200">Oblien</span> product ↗
          </a>

          <div className="mt-6 flex items-center gap-1">
            <a href="https://x.com/oblienhq" rel="noopener" aria-label="X" className="grid h-8 w-8 place-items-center text-neutral-400 transition-colors hover:text-neutral-950 dark:text-neutral-500 dark:hover:text-white">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24h-6.66l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
              </svg>
            </a>
            <a href="https://www.linkedin.com/company/oblien" rel="noopener" aria-label="LinkedIn" className="grid h-8 w-8 place-items-center text-neutral-400 transition-colors hover:text-neutral-950 dark:text-neutral-500 dark:hover:text-white">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 01-2.063-2.065 2.064 2.064 0 112.063 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z" />
              </svg>
            </a>
            <a href="https://github.com/oblien/mindwire" rel="noopener" aria-label="GitHub" className="grid h-8 w-8 place-items-center text-neutral-400 transition-colors hover:text-neutral-950 dark:text-neutral-500 dark:hover:text-white">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.3.8-.6v-2c-3.2.7-3.9-1.5-3.9-1.5-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.7-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.7 18.3.5 12 .5z" />
              </svg>
            </a>
          </div>
        </div>

        {cols.map((c) => (
          <div key={c.title}>
            <h4 className="mb-4 font-mono text-[11px] uppercase tracking-[0.16em] text-neutral-400 dark:text-neutral-600">
              {c.title}
            </h4>
            <ul className="space-y-2.5">
              {c.links.map((l) => (
                <li key={l.label}>
                  {l.soon ? (
                    <span className="inline-flex items-center gap-1.5 text-sm text-neutral-400 dark:text-neutral-600">
                      {l.label}
                      <span className="font-mono text-[9px] uppercase tracking-wider">soon</span>
                    </span>
                  ) : (
                    <Link href={l.href} className={linkCls}>
                      {l.label}
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>

      <div className="border-t border-neutral-200 dark:border-neutral-800">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-6 text-xs text-neutral-400 dark:text-neutral-600 sm:px-6">
          <span>© 2026 Oblien LLC · Apache-2.0</span>
          <div className="flex items-center gap-5">
            <a href="https://oblien.com" rel="noopener" className="transition-colors hover:text-neutral-950 dark:hover:text-white">
              Oblien ↗
            </a>
            <a href="https://github.com/oblien/mindwire" rel="noopener" className="transition-colors hover:text-neutral-950 dark:hover:text-white">
              GitHub ↗
            </a>
          </div>
        </div>
      </div>
    </footer>
  );
}
