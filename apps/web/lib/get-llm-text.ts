import type { DocPage } from "@/lib/source";

// Render one doc page as clean LLM-facing markdown: its raw source with the frontmatter block dropped,
// prefixed by a title + canonical-URL header (the description, when present, sits under the title).
// Shared by /llms-full.txt (the whole corpus) and the per-page `.md` route (the Copy Markdown button).
// `getText("raw")` reads the original source file; safe to call at build time (force-static routes).
export async function getLLMText(page: { url: string; data: DocPage }): Promise<string> {
  const { data } = page;
  const raw = await data.getText("raw");
  const body = raw.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n+/, "").trim();
  const header = [
    `# ${data.title}`,
    `URL: https://mindwire.sh${page.url}`,
    data.description ? `\n${data.description}` : "",
  ]
    .filter(Boolean)
    .join("\n");
  return `${header}\n\n${body}`;
}
