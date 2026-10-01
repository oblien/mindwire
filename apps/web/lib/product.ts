export const githubUrl = "https://github.com/oblien/mindwire";
export const cloudDashboardUrl = "https://oblien.com/dashboard";
export const cloudPricingUrl = "https://oblien.com/pricing";
export const cloudRatesUrl = "https://oblien.com/dashboard/credits/pricing";

// Add the public iOS URL at deployment time. Until then, download buttons lead
// to the iPhone setup section, which clearly marks the download as coming soon.
function iosDownload() {
  const value = process.env.NEXT_PUBLIC_IOS_APP_URL?.trim();
  if (!value) return null;
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" || url.username || url.password) return null;
    if (url.hostname === "apps.apple.com") {
      return { href: url.href, label: "Download on the App Store" };
    }
    if (url.hostname === "testflight.apple.com") {
      return { href: url.href, label: "Join on TestFlight" };
    }
  } catch {
    // An invalid deployment value must not turn a download CTA into an unsafe URL.
  }
  return null;
}

export const iosApp = iosDownload();
export const downloadUrl = iosApp?.href ?? "/get-started#iphone";

export const productAgents = [
  { id: "codex", name: "Codex", href: "/docs/reference/agents/codex" },
  { id: "claude", name: "Claude Code", href: "/docs/reference/agents/claude" },
  { id: "grok", name: "Grok Build", href: "/docs/reference/agents/grok" },
  { id: "opencode", name: "opencode", href: "/docs/reference/agents/opencode" },
] as const;

export type ProductAgent = (typeof productAgents)[number]["id"];
