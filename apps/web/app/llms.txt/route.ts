import { source } from "@/lib/source";

// The llms.txt index: one line per doc page (title, URL, description) so an LLM can discover the whole
// docs tree and fetch /llms-full.txt for the full corpus. Replaces the old build-docs.mjs public/llms.txt.
// Rendered dynamically from the Fumadocs source at request time — no build step, no committed artifact.
export const dynamic = "force-dynamic";

export function GET() {
  const pages = source.getPages();
  const body = [
    "# MindWire",
    "",
    "> The runtime for coding agents: one protocol, one event stream and one auth flow over Claude Code, Codex, opencode and more — driven from TypeScript, Go, or plain HTTP + SSE.",
    "",
    "## Docs",
    "",
    ...pages
      .sort((a, b) => a.url.localeCompare(b.url))
      .map((p) => {
        const desc = p.data.description ? `: ${p.data.description}` : "";
        return `- [${p.data.title}](https://mindwire.sh${p.url})${desc}`;
      }),
    "",
  ].join("\n");

  return new Response(body, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
