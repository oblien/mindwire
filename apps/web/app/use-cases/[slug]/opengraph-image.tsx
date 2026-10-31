import { notFound } from "next/navigation";
import { getUseCase } from "@/lib/use-cases";
import { renderSocialImage, socialImageSize } from "@/lib/social-image";

export const alt =
  "Mindwire product examples — your product, powered by agents";
export const size = socialImageSize;
export const contentType = "image/png";

export default async function Image({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const item = getUseCase((await params).slug);
  if (!item) notFound();
  return renderSocialImage({
    title: item.title,
    description: item.summary,
    label: `Product examples / ${item.platform}`,
  });
}
