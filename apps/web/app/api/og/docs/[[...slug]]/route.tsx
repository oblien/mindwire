import { notFound } from "next/navigation";
import { source } from "@/lib/source";
import { renderSocialImage } from "@/lib/social-image";

export const revalidate = 86400;

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ slug?: string[] }> },
) {
  const { slug } = await params;
  const page = source.getPage(slug);
  if (!page) notFound();
  const section = (
    {
      guides: "Guides",
      concepts: "Concepts",
      reference: "Reference",
    } as Record<string, string>
  )[slug?.[0] ?? ""];
  return renderSocialImage({
    title: page.data.title,
    description:
      page.data.description ??
      "Build your product with the MindWire SDK and runtime.",
    label: section ? `Documentation / ${section}` : "Documentation",
  });
}
