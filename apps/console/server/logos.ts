// Provider brand marks, proxied for the console. Two upstreams, in order of preference:
//
//   1. lobehub's icon set (jsDelivr) — a COLORED brand mark, where one exists for the provider. These
//      carry explicit fills (`fill="gold"`, gradients, …), so they render in the real brand colours.
//   2. models.dev's logo set — a monochrome `currentColor` mark. Every catalog provider has one; it
//      inherits the ink palette (and the light/dark toggle) when inlined. This is the right look for the
//      brands that ARE monochrome (OpenAI, xAI, Groq, Vercel) and a neutral stand-in for the long tail.
//
// The browser inlines the returned markup so a colored SVG paints its own fills while a `currentColor`
// one still inherits ink — one inline path handles both. A total miss resolves to `null` → monogram tile.
//
// SECURITY: `id` is constrained to the catalog id charset (no traversal / SSRF to another host), and the
// lobehub name only ever comes from the curated `LOBE_COLOR` map below — never from request input. Because
// the markup is inlined client-side, it is stripped of scripts, handlers, <foreignObject>, and
// `javascript:` refs before being returned.

const MDEV_BASE = "https://models.dev/logos";
// Pinned so the marks are deterministic across deploys (jsDelivr rejects floating tags on its metadata API
// anyway); bump deliberately when refreshing the icon set.
const LOBE_BASE = "https://cdn.jsdelivr.net/npm/@lobehub/icons-static-svg@1.94.0/icons";
const ID_RE = /^[a-z0-9][a-z0-9._-]*$/i;

// models.dev provider id → lobehub icon base name that has a `-color` variant. Region/plan variants of a
// brand (…-cn, …-coding-plan, …-token-plan) fold onto the same mark. Providers absent here have no known
// colored mark and fall back to the models.dev monochrome logo. Derived by intersecting the live
// models.dev provider list with lobehub's colored-icon set.
const LOBE_COLOR: Record<string, string> = {
  aihubmix: "aihubmix",
  alibaba: "alibaba",
  "alibaba-cn": "alibaba",
  "alibaba-coding-plan": "alibaba",
  "alibaba-coding-plan-cn": "alibaba",
  "alibaba-token-plan": "alibaba",
  "alibaba-token-plan-cn": "alibaba",
  "amazon-bedrock": "bedrock",
  anthropic: "claude",
  arcee: "arcee",
  azure: "azure",
  cerebras: "cerebras",
  claudinio: "claude",
  cohere: "cohere",
  crusoe: "crusoe",
  deepinfra: "deepinfra",
  deepseek: "deepseek",
  "fireworks-ai": "fireworks",
  "github-copilot": "copilot",
  google: "google",
  "google-vertex": "vertexai",
  "google-vertex-anthropic": "claude",
  huggingface: "huggingface",
  "kimi-for-coding": "kimi",
  llama: "meta",
  longcat: "longcat",
  meta: "meta",
  minimax: "minimax",
  "minimax-cn": "minimax",
  "minimax-cn-coding-plan": "minimax",
  "minimax-coding-plan": "minimax",
  mistral: "mistral",
  modelscope: "modelscope",
  moonshotai: "kimi",
  "moonshotai-cn": "kimi",
  morph: "morph",
  nova: "nova",
  "novita-ai": "novita",
  nvidia: "nvidia",
  openrouter: "openrouter",
  perplexity: "perplexity",
  "perplexity-agent": "perplexity",
  poe: "poe",
  poolside: "poolside",
  "qiniu-ai": "qiniu",
  siliconflow: "siliconcloud",
  "siliconflow-cn": "siliconcloud",
  stepfun: "stepfun",
  "stepfun-ai": "stepfun",
  "stepfun-ai-step-plan": "stepfun",
  "stepfun-step-plan": "stepfun",
  submodel: "submodel",
  "tencent-coding-plan": "tencent",
  "tencent-token-plan": "tencent",
  "tencent-tokenhub": "tencent",
  togetherai: "together",
  upstage: "upstage",
  venice: "venice",
  zai: "zhipu",
  "zai-coding-plan": "zhipu",
  zhipuai: "zhipu",
  "zhipuai-coding-plan": "zhipu",
};

/** id → sanitized SVG markup, or `null` when no upstream has a mark for it (both outcomes cached). */
const cache = new Map<string, string | null>();
/** In-flight fetches, so a burst of requests for the same id collapses to one upstream round-trip. */
const inflight = new Map<string, Promise<string | null>>();

/** Strip anything script-like so the markup is safe to inline in the browser. Colours are preserved. */
function sanitize(svg: string): string {
  return svg
    .replace(/<\?xml[\s\S]*?\?>/gi, "")
    .replace(/<!DOCTYPE[\s\S]*?>/gi, "")
    .replace(/<script[\s\S]*?<\/script>/gi, "")
    .replace(/<foreignObject[\s\S]*?<\/foreignObject>/gi, "")
    .replace(/\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, "")
    .replace(/(href|xlink:href)\s*=\s*("javascript:[^"]*"|'javascript:[^']*')/gi, "")
    .trim();
}

async function fetchSvg(url: string): Promise<string | null> {
  try {
    const res = await fetch(url, { headers: { accept: "image/svg+xml,*/*" } });
    if (!res.ok) return null;
    const text = await res.text();
    if (!text.includes("<svg")) return null;
    return sanitize(text);
  } catch {
    return null;
  }
}

/** Colored lobehub mark if we have one for this id, else the models.dev monochrome mark, else null. */
async function resolveLogo(id: string): Promise<string | null> {
  const lobe = LOBE_COLOR[id];
  if (lobe) {
    const colored = await fetchSvg(`${LOBE_BASE}/${lobe}-color.svg`);
    if (colored) return colored;
  }
  return fetchSvg(`${MDEV_BASE}/${id}.svg`);
}

/** The provider's inline-ready SVG markup, or `null` if unavailable. Fetched once per id, then cached. */
export async function fetchProviderLogo(id: string): Promise<string | null> {
  if (!ID_RE.test(id)) return null;
  const key = id.toLowerCase();
  if (cache.has(key)) return cache.get(key) ?? null;
  let p = inflight.get(key);
  if (!p) {
    p = resolveLogo(key).then((svg) => {
      cache.set(key, svg);
      inflight.delete(key);
      return svg;
    });
    inflight.set(key, p);
  }
  return p;
}
