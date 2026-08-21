import { source, type DocPage } from "@/lib/source";
import { getLLMText } from "@/lib/get-llm-text";

// The full corpus in one plain-text file: every doc page's raw markdown, frontmatter stripped, with a
// title + canonical URL header. LLMs fetch this once instead of crawling every page. Each section is
// rendered by the shared getLLMText() (same output as the per-page `.md` route).
// Rendered dynamically from the Fumadocs source at request time — no build step, no committed artifact.
export const dynamic = "force-dynamic";

export async function GET() {
  const pages = source.getPages().sort((a, b) => a.url.localeCompare(b.url));

  const sections = await Promise.all(
    pages.map((page) => getLLMText({ url: page.url, data: page.data as DocPage })),
  );

  return new Response(sections.join("\n\n---\n\n") + "\n", {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
