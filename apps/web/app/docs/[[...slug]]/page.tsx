import { source, type DocPage } from "@/lib/source";
import {
  DocsPage,
  DocsBody,
  DocsTitle,
  DocsDescription,
} from "fumadocs-ui/page";
import { notFound } from "next/navigation";
import type { Metadata } from "next";
import { getMDXComponents } from "@/mdx-components";
import CopyMarkdown from "@/components/CopyMarkdown";

type PageProps = { params: Promise<{ slug?: string[] }> };

export default async function Page(props: PageProps) {
  const { slug } = await props.params;
  const page = source.getPage(slug);
  if (!page) notFound();

  // See lib/source.ts: loader() widens page data to the base type; the runtime value is the full
  // compiled doc, so re-attach its real shape (body/toc/full).
  const data = page.data as DocPage;
  const MDX = data.body;
  return (
    <DocsPage toc={data.toc} full={data.full}>
      {/* Copy-as-Markdown toolbar, floated to the top-right of the header (beside the title); the button
          fetches the page's own `.md` (app/api/md via rewrite). Placed before the title so it floats
          up beside it without affecting the title→description spacing. */}
      <CopyMarkdown markdownUrl={`${page.url}.md`} />
      <DocsTitle>{data.title}</DocsTitle>
      <DocsDescription>{data.description}</DocsDescription>
      <DocsBody>
        <MDX components={getMDXComponents()} />
      </DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: PageProps): Promise<Metadata> {
  const { slug } = await props.params;
  const page = source.getPage(slug);
  if (!page) notFound();
  return {
    title: page.data.title,
    description: page.data.description,
  };
}
