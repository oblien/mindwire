import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Changelog — MindWire",
  description: "What's new in MindWire.",
};

const releases = [
  {
    version: "v0.1.0",
    date: "2026",
    tag: "Initial",
    changes: [
      "mindwire — the TypeScript SDK: streaming Run handles (async-iterable events, cancel, wait), step-flow auth, catalog, config, chats, and notifications. Zero runtime deps; ESM + CJS + types.",
      "Go daemon core — a unified event protocol, a per-agent adapter registry, the capabilities matrix (native vs. emulated), dynamic auth/settings, and the HTTP + SSE API.",
      "Claude Code adapter — the first drop-in agent, driven via the CLI.",
      "Provider-agnostic webhook notifications — POST turn events to any URL.",
      "Docs + landing site.",
    ],
  },
];

const heading = "text-neutral-950 dark:text-white font-semibold tracking-tight";

export default function Changelog() {
  return (
    <section className="mx-auto max-w-3xl px-4 pb-24 pt-16 sm:px-6 sm:pt-24">
      <span className="eyebrow">Changelog</span>
      <h1 className={`mt-5 text-4xl sm:text-5xl ${heading}`}>Changelog</h1>
      <p className="mt-5 text-lg text-neutral-600 dark:text-neutral-400">What's new in MindWire.</p>

      <div className="mt-14 space-y-14">
        {releases.map((r) => (
          <div key={r.version} className="grid gap-4 border-t border-neutral-200 pt-8 dark:border-neutral-800 sm:grid-cols-[150px_1fr]">
            <div>
              <div className="font-mono text-sm font-medium text-neutral-950 dark:text-white">{r.version}</div>
              <div className="font-mono text-xs text-neutral-400 dark:text-neutral-600">{r.date}</div>
            </div>
            <div>
              <span className="chip mb-4 inline-flex">{r.tag}</span>
              <ul className="space-y-3 text-sm leading-relaxed text-neutral-700 dark:text-neutral-300">
                {r.changes.map((c, i) => (
                  <li key={i} className="flex gap-3">
                    <span className="mt-0.5 shrink-0 font-mono text-neutral-400 dark:text-neutral-600">+</span>
                    <span>{c}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
