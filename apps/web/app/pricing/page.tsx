import type { Metadata } from "next";
import { Check } from "lucide-react";
import Corners from "@/components/Corners";

export const metadata: Metadata = {
  title: "Pricing — MindWire",
  description: "Open source and self-hostable for free. Managed cloud when you want zero ops.",
};

const tiers = [
  {
    name: "Open Source",
    price: "$0",
    unit: "self-hosted",
    tagline: "Run MindWire on your own infra, forever.",
    features: [
      "Claude Code & Codex (more agents coming)",
      "Unlimited turns & sessions",
      "TypeScript SDK + REST/SSE",
      "Webhook, file & exec notifications",
      "Apache-2.0 — no limits",
      "Community support",
    ],
    cta: { label: "Start building", href: "/docs", primary: true },
  },
  {
    name: "Cloud",
    price: "Soon",
    unit: "managed",
    tagline: "Hosted MindWire — zero ops, usage-based.",
    features: [
      "Managed daemons & scaling",
      "Usage-based billing",
      "Dashboards & run logs",
      "Managed agent auth",
      "Priority support",
    ],
    cta: { label: "Join the waitlist", href: "mailto:hello@oblien.com?subject=MindWire%20Cloud%20waitlist" },
    badge: "Coming soon",
  },
  {
    name: "Enterprise",
    price: "Custom",
    unit: "",
    tagline: "For teams running agents at scale.",
    features: [
      "SSO / SAML",
      "SLA & dedicated support",
      "Private deployment",
      "Security review",
      "Volume pricing",
    ],
    cta: { label: "Talk to us", href: "mailto:sales@oblien.com?subject=MindWire%20Enterprise" },
  },
];

const heading = "text-neutral-950 dark:text-white font-semibold tracking-tight";

export default function Pricing() {
  return (
    <>
      <section className="mx-auto max-w-6xl px-4 pb-10 pt-16 text-center sm:px-6 sm:pt-24">
        <span className="eyebrow">Pricing</span>
        <h1 className={`mt-5 text-4xl sm:text-5xl ${heading}`}>Start free. Self-host forever.</h1>
        <p className="mx-auto mt-5 max-w-xl text-lg text-neutral-600 dark:text-neutral-400">
          MindWire is open source and self-hostable at no cost. Add managed cloud when you want zero
          ops.
        </p>
      </section>

      <section className="mx-auto max-w-6xl px-4 pb-24 sm:px-6">
        <div className="relative">
          <Corners />
          <div className="bento md:grid-cols-3">
            {tiers.map((t) => (
              <div key={t.name} className="cell flex flex-col p-8">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-xs uppercase tracking-[0.16em] text-neutral-500">
                    {t.name}
                  </span>
                  {t.badge && <span className="chip">{t.badge}</span>}
                </div>
                <div className="mt-6 flex items-baseline gap-2">
                  <span className={`text-4xl ${heading}`}>{t.price}</span>
                  {t.unit && <span className="text-sm text-neutral-500">{t.unit}</span>}
                </div>
                <p className="mt-3 text-sm text-neutral-600 dark:text-neutral-400">{t.tagline}</p>
                <ul className="mt-7 space-y-2.5 text-sm">
                  {t.features.map((f) => (
                    <li key={f} className="flex gap-2.5">
                      <Check size={16} strokeWidth={2} className="mt-0.5 shrink-0 text-neutral-400 dark:text-neutral-500" />
                      <span className="text-neutral-700 dark:text-neutral-300">{f}</span>
                    </li>
                  ))}
                </ul>
                <div className="mt-auto pt-8">
                  <a
                    href={t.cta.href}
                    className={`btn w-full ${t.cta.primary ? "btn-primary" : "btn-ghost"}`}
                  >
                    {t.cta.label}
                  </a>
                </div>
              </div>
            ))}
          </div>
        </div>
        <p className="mx-auto mt-6 max-w-xl text-center text-xs text-neutral-400 dark:text-neutral-600">
          Self-hosting the open-source daemon is free — you only pay your own model/API costs for the
          agents you run.
        </p>
      </section>
    </>
  );
}
