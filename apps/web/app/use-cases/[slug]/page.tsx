import type { Metadata } from "next";
import { notFound, permanentRedirect } from "next/navigation";
import { getUseCase, legacyUseCases, useCases } from "@/lib/use-cases";
import BusinessCaseStudy from "@/components/examples/BusinessCaseStudy";

type Props = { params: Promise<{ slug: string }> };

function resolveCase(slug: string) {
  if (Object.hasOwn(legacyUseCases, slug)) {
    const next = legacyUseCases[slug];
    permanentRedirect(next ? `/use-cases/${next}/` : "/use-cases/");
  }
  const item = getUseCase(slug);
  if (!item) notFound();
  return item;
}
export function generateStaticParams() {
  return [
    ...useCases.map(({ slug }) => ({ slug })),
    ...Object.keys(legacyUseCases).map((slug) => ({ slug })),
  ];
}
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const item = resolveCase((await params).slug);
  return {
    title: `${item.title} — Mindwire examples`,
    description: item.summary,
    alternates: { canonical: `/use-cases/${item.slug}/` },
  };
}
export default async function UseCasePage({ params }: Props) {
  return <BusinessCaseStudy item={resolveCase((await params).slug)} />;
}
