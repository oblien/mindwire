import { source, type DocPage } from "@/lib/source";
import { getLLMText } from "@/lib/get-llm-text";
import { notFound } from "next/navigation";

// Per-page raw markdown for the "Copy Markdown" button (and for LLMs). A rewrite in next.config maps the
// pretty URL `/docs/<slug>.md` onto this handler, so every doc is fetchable as clean markdown. One entry
// per doc page is prerendered at build time (force-static + generateStaticParams); nothing else resolves.
export const dynamic = "force-static";
export const dynamicParams = false;

export function generateStaticParams() {
  return source.generateParams();
}

export async function GET(
  _req: Request,
  { params }: { params: Promise<{ slug?: string[] }> },
) {
  const { slug } = await params;
  const page = source.getPage(slug);
  if (!page) notFound();

  const text = await getLLMText({ url: page.url, data: page.data as DocPage });
  return new Response(text + "\n", {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}
