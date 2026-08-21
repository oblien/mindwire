"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Copy, ExternalLink, FileText } from "lucide-react";

// Copy-as-Markdown toolbar for docs pages (Fumadocs doesn't export its own page-actions, so this is a
// custom monochrome/square build). "Copy Markdown" fetches the page's `.md` and writes it to the
// clipboard; the caret opens a small menu to view the raw markdown or hand it to ChatGPT / Claude.
// `markdownUrl` is the same-origin `.md` path (e.g. /docs/guides/quickstart.md).
const SITE = "https://mindwire.sh";

export default function CopyMarkdown({ markdownUrl }: { markdownUrl: string }) {
  const [copied, setCopied] = useState(false);
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);

  // Close the menu on outside click / Escape.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const copy = async () => {
    try {
      const res = await fetch(markdownUrl);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      await navigator.clipboard.writeText(await res.text());
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // clipboard or fetch unavailable (insecure context, offline) — fail silently.
    }
  };

  // Assistants fetch the doc over the network, so these use the canonical production URL, not localhost.
  const prompt = `Read ${SITE}${markdownUrl} so I can ask you questions about it.`;
  const menu = [
    { label: "View as Markdown", href: markdownUrl, external: false, Icon: FileText },
    {
      label: "Open in ChatGPT",
      href: `https://chatgpt.com/?hints=search&q=${encodeURIComponent(prompt)}`,
      external: true,
      Icon: ExternalLink,
    },
    {
      label: "Open in Claude",
      href: `https://claude.ai/new?q=${encodeURIComponent(prompt)}`,
      external: true,
      Icon: ExternalLink,
    },
  ];

  const chrome =
    "border border-neutral-200 bg-neutral-50 text-neutral-700 transition-colors hover:border-neutral-300 hover:text-neutral-950 dark:border-white/10 dark:bg-white/[0.03] dark:text-neutral-300 dark:hover:border-white/20 dark:hover:text-white";

  return (
    <div ref={wrapRef} className="not-prose relative float-right ml-4 mb-3 flex items-center gap-1.5">
      <button type="button" onClick={copy} className={`inline-flex items-center gap-2 px-3 py-1.5 text-sm ${chrome}`}>
        {copied ? <Check size={15} aria-hidden /> : <Copy size={15} aria-hidden />}
        {copied ? "Copied" : "Copy Markdown"}
      </button>

      <button
        type="button"
        aria-label="Open document in…"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={`inline-flex items-center px-2 py-1.5 ${chrome}`}
      >
        <ChevronDown size={15} aria-hidden className={`transition-transform ${open ? "rotate-180" : ""}`} />
      </button>

      {open && (
        <div className="absolute right-0 top-full z-20 mt-1 min-w-52 border border-neutral-200 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-[#08080b]">
          {menu.map((m) => (
            <a
              key={m.label}
              href={m.href}
              onClick={() => setOpen(false)}
              {...(m.external ? { target: "_blank", rel: "noopener noreferrer" } : {})}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-neutral-600 transition-colors hover:bg-neutral-100 hover:text-neutral-950 dark:text-neutral-300 dark:hover:bg-white/[0.06] dark:hover:text-white"
            >
              <m.Icon size={15} aria-hidden className="text-neutral-400" />
              {m.label}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
