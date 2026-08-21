// A model-provider mark for the Models catalog and the Providers surface — the same "real brand, no
// mask" rule as AgentIcon/ChannelIcon, scaled to the whole models.dev catalog (~190 providers).
//
// The mark is the provider's REAL SVG, resolved by our cached server proxy (`/api/catalog/logo/:id`) and
// inlined here. The proxy prefers a COLORED brand mark (lobehub's icon set) and falls back to models.dev's
// monochrome `currentColor` mark otherwise. Inlining the markup — rather than dropping it into an `<img>`
// — is what makes both work from one code path: a colored SVG paints its own fills, while a monochrome one
// inherits the ink palette from this span's text colour and flips with the light/dark toggle. Fetches are
// cached module-wide (and deduped in flight), so a given id hits the network at most once per session. A
// provider no upstream has a mark for (or a transient failure) falls back to a MONOGRAM tile: a sharp,
// bordered ink square with the provider's initials, so it reads as an intentional chip, not a broken image.
import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";
import { api } from "@/lib/api";

// id → inline SVG markup, or `null` once we know models.dev has no mark for it. `undefined` (absent key)
// means "not resolved yet". Module-scoped so every ProviderLogo across the app shares one cache.
const logoCache = new Map<string, string | null>();
const logoInflight = new Map<string, Promise<string | null>>();

/** Resolve a provider's mark once, sharing the fetch across every caller and caching both outcomes. */
function loadLogo(id: string): Promise<string | null> {
  if (logoCache.has(id)) return Promise.resolve(logoCache.get(id) ?? null);
  let p = logoInflight.get(id);
  if (!p) {
    p = api
      .catalogLogo(id)
      .catch(() => null)
      .then((svg) => {
        logoCache.set(id, svg);
        logoInflight.delete(id);
        return svg;
      });
    logoInflight.set(id, p);
  }
  return p;
}

// Two-letter monogram for the long tail: the first alphanumerics of the leading id segment (before the
// first separator), so `fireworks-ai` → "FI", `moonshotai` → "MO", `zhipuai` → "ZH".
function monogram(provider: string): string {
  const head = provider.split(/[-_.\s]/)[0] ?? provider;
  return (head.replace(/[^a-z0-9]/gi, "").slice(0, 2) || provider.slice(0, 2)).toUpperCase();
}

/** Human provider names for the well-known ids; the long tail falls back to a title-cased id. */
const NAMES: Record<string, string> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  google: "Google",
  "google-vertex": "Google Vertex",
  "google-vertex-anthropic": "Anthropic (Vertex)",
  xai: "xAI",
  mistral: "Mistral",
  meta: "Meta",
  llama: "Llama",
  deepseek: "DeepSeek",
  groq: "Groq",
  perplexity: "Perplexity",
  openrouter: "OpenRouter",
  "github-copilot": "GitHub Copilot",
  opencode: "opencode",
  ollama: "Ollama",
  huggingface: "Hugging Face",
  alibaba: "Alibaba (Qwen)",
  moonshotai: "Moonshot AI",
  zhipuai: "Zhipu AI",
  zai: "Z.ai",
  cohere: "Cohere",
  azure: "Azure",
  "amazon-bedrock": "Amazon Bedrock",
  cerebras: "Cerebras",
  "fireworks-ai": "Fireworks",
  togetherai: "Together",
  vercel: "Vercel",
  v0: "v0",
  custom: "Custom",
  other: "Other",
};

/** Pretty provider name: a curated label, else the id title-cased (`nano-gpt` → `Nano Gpt`). */
export function providerLabel(provider: string): string {
  if (NAMES[provider]) return NAMES[provider];
  return provider
    .split(/[-_.\s]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

/**
 * A provider's brand mark, sized by `className` (set width/height + a text colour, e.g. `size-5 text-ink`).
 * The inlined models.dev SVG is monochrome `currentColor`, so it inherits that ink colour and adapts to the
 * light/dark toggle. Until the fetch resolves (and permanently, for providers models.dev has no mark for),
 * a bordered initials monogram stands in.
 */
export function ProviderLogo({ provider, className }: { provider: string; className?: string }) {
  const [svg, setSvg] = useState<string | null>(() => logoCache.get(provider) ?? null);

  useEffect(() => {
    let live = true;
    // Seed synchronously from cache when we already know the answer, so a re-mount doesn't flash a monogram.
    if (logoCache.has(provider)) {
      setSvg(logoCache.get(provider) ?? null);
      return;
    }
    setSvg(null);
    loadLogo(provider).then((s) => {
      if (live) setSvg(s);
    });
    return () => {
      live = false;
    };
  }, [provider]);

  if (svg) {
    return (
      <span
        className={cn("inline-flex items-center justify-center [&>svg]:size-full", className)}
        dangerouslySetInnerHTML={{ __html: svg }}
        aria-hidden
      />
    );
  }

  return (
    <span
      className={cn(
        "inline-flex items-center justify-center border border-ink/20 bg-ink/[0.05] font-mono text-[10px] font-semibold uppercase leading-none text-ink/70",
        className,
      )}
      aria-hidden
    >
      {monogram(provider)}
    </span>
  );
}
