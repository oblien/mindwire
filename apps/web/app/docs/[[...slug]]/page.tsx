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
import Link from "next/link";
import { ChevronRight } from "lucide-react";

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
    <DocsPage toc={data.toc} full={data.full} breadcrumb={{ enabled: false }}>
      <div className="docs-page-tools">
        <nav aria-label="Breadcrumb">
          <Link href="/docs">Docs</Link>
          <ChevronRight size={12} aria-hidden="true" />
          <span>
            {(
              {
                guides: "Guides",
                concepts: "Concepts",
                reference: "Reference",
              } as Record<string, string>
            )[slug?.[0] ?? ""] ?? "Overview"}
          </span>
        </nav>
        <CopyMarkdown markdownUrl={`${page.url}.md`} />
      </div>
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
  const title = `${page.data.title} — MindWire Docs`;
  const image = {
    url: `/api/og/docs/${slug?.length ? slug.map(encodeURIComponent).join("/") + "/" : ""}?v=paper-1`,
    width: 1200,
    height: 630,
    alt: title,
  };
  return {
    title,
    description: page.data.description,
    alternates: { canonical: page.url },
    openGraph: {
      title,
      description: page.data.description,
      siteName: "MindWire",
      type: "article",
      url: page.url,
      images: [image],
    },
    twitter: {
      card: "summary_large_image",
      title,
      description: page.data.description,
      images: [image],
    },
  };
}
