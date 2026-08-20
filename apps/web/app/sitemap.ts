import type { MetadataRoute } from "next";
import { source } from "@/lib/source";

const BASE = "https://mindwire.sh";

// trailingSlash: true — canonical URLs end with "/". Marketing routes are hand-listed; doc routes come
// from the content source so new pages appear automatically.
export default function sitemap(): MetadataRoute.Sitemap {
  const marketing: MetadataRoute.Sitemap = [
    { url: `${BASE}/`, changeFrequency: "monthly", priority: 1 },
    { url: `${BASE}/pricing/`, changeFrequency: "monthly", priority: 0.6 },
    { url: `${BASE}/sandboxing/`, changeFrequency: "monthly", priority: 0.6 },
    { url: `${BASE}/changelog/`, changeFrequency: "weekly", priority: 0.5 },
  ];

  const docs: MetadataRoute.Sitemap = source.getPages().map((page) => ({
    url: `${BASE}${page.url}/`,
    changeFrequency: "weekly",
    priority: 0.7,
  }));

  return [...marketing, ...docs];
}
