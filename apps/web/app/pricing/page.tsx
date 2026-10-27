import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight, CalendarDays, Check, Cloud, Code2, Info } from "lucide-react";
import Questions from "@/components/landing/Questions";
import FinalInvite from "@/components/landing/FinalInvite";
import { getComputerPricing, usd } from "@/lib/computer-pricing";
import { cloudDashboardUrl } from "@/lib/product";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Pricing — Mindwire",
  description: "Use the open-source SDK and runtime on your own infrastructure, or choose an optional monthly Cloud Computer. Agent usage is billed separately.",
  alternates: { canonical: "/pricing/" },
};
export const revalidate = 60;

const descriptions: Record<string, string> = {
  essential: "For smaller projects and everyday agent tasks.",
  pro: "More capacity for active projects and regular builds.",
  max: "Extra headroom for larger builds and demanding work.",
};

export default async function Pricing() {
  const pricing = await getComputerPricing();
  return <div className={base.landing}>
    <header className={styles.pageHero}>
      <span className={styles.eyebrow}>SDK, runtime, and optional hosting</span>
      <h1>Build on your terms.<br /><em>Choose where it runs.</em></h1>
      <p>Use the SDK and runtime on infrastructure you manage. Add a monthly Cloud Computer when you need an online host. Your agent account stays separate.</p>
    </header>
    <div className={styles.pageWrap}>
      <section className={styles.freePlan} aria-labelledby="own-computer-title">
        <Code2 size={44} strokeWidth={1.4} />
        <div><h2 id="own-computer-title">Open-source SDK & runtime</h2><p>Build with TypeScript, Go, or REST. Run on your own computer or server, using supported agent accounts.</p></div>
        <div className={styles.freePrice}><strong>$0</strong><span>Mindwire runtime fee</span></div>
        <Link href="/docs/guides/quickstart" className={base.primaryButton}>Start building <ArrowUpRight size={16} /></Link>
      </section>
      <section aria-labelledby="cloud-plans-title" id="cloud-plans">
        <div className={styles.cloudHeading}><div><h2 id="cloud-plans-title">A home that stays online.</h2><p>Your runtime, files, and projects on a monthly Cloud Computer.</p></div><span className={styles.billingInterval}><CalendarDays size={15} />Monthly · USD</span></div>
        <div className={styles.planGrid}>
          {pricing.plans.map((plan, i) => <article className={styles.planCard} data-tone={plan.id === "pro" ? "blue" : i === 2 ? "peach" : "mint"} key={plan.id}>
            <div className={styles.planName}><h3>{plan.name}</h3><Cloud size={34} strokeWidth={1.4} /></div>
            <p className={styles.planDescription}>{descriptions[plan.id] ?? "An online workspace for your runtime and projects."}</p>
            <div className={styles.planPrice}><strong>{usd(plan.monthlyCents)}</strong><span>/ month</span></div>
            <p className={styles.planPriceNote}>Per computer. AI usage billed separately.</p>
            <div className={styles.planSpecs}><div><strong>{plan.vcpus}</strong><small>Shared vCPU</small></div><div><strong>{plan.memoryMb / 1024} GB</strong><small>Memory</small></div><div><strong>{plan.diskGb} GB</strong><small>Storage</small></div></div>
            <ul className={styles.planBenefits}><li><Check size={15} />Persistent files and projects</li><li><Check size={15} />Access from your Mac or phone</li><li><Check size={15} />Bring your supported agent accounts</li></ul>
            <a className={plan.id === "pro" ? base.primaryButton : base.secondaryButton} href={`${cloudDashboardUrl}?plan=${encodeURIComponent(plan.id)}`}>{pricing.available ? `Choose ${plan.name}` : "View in Console"}<ArrowUpRight size={16} /></a>
          </article>)}
        </div>
        {!pricing.available && <div className={styles.availabilityNote}><Info size={16} /><span>{pricing.source === "catalog" ? "These are our published plans. Live checkout availability couldn’t be confirmed." : "Cloud checkout is not available right now."} <a href={cloudDashboardUrl}>Check availability in your account.</a></span></div>}
        <p className={styles.pricingNote}>CPU, memory and included storage are covered for the paid month. Model usage, transfer, retained storage and backups follow separate terms shown in checkout. Existing subscriptions keep their saved terms.</p>
      </section>
      <section className={styles.includedSection} aria-labelledby="costs-title">
        <h2 id="costs-title">A clear place for every cost.</h2>
        <div className={styles.includedGrid}>
          <div><span>01 · Mindwire</span><h3>The SDK and runtime.</h3><p>The open-source runtime and SDK have no Mindwire usage fee. Bring your own computer or server and use the same tools.</p></div>
          <div><span>02 · Your computer</span><h3>Your execution environment.</h3><p>Use hardware you own, or pay a monthly subscription for a Cloud Computer. Your infrastructure stays separate from your AI account.</p></div>
          <div><span>03 · Your agent</span><h3>Your agent provider.</h3><p>Connect a supported provider subscription or API key. The provider’s limits, availability and usage charges still apply.</p></div>
        </div>
      </section>
    </div>
    <section className={`${styles.section} ${styles.faqSection}`} aria-labelledby="pricing-questions">
      <div className={styles.sectionCopy}><span className={styles.eyebrow}>Good to know</span><h2 id="pricing-questions">Less guesswork.<br /><em>More getting on.</em></h2><p>Runtime, hosting, and agent usage are separate. Choose the pieces your application needs.</p></div>
      <Questions items={[
        { question: "Do I need Cloud to use Mindwire?", answer: "No. Run Mindwire on your own Mac, Windows or Linux computer, or connect a server. Cloud is an optional home for work that needs to stay online when your personal computer is off." },
        { question: "Does a plan include Codex or Claude usage?", answer: "No. Connect a supported account or API key for your agent. Your provider’s subscription, rate limits and usage charges remain separate from the computer subscription." },
        { question: "Is this one subscription for all my computers?", answer: "Each Cloud Computer has its own monthly subscription. You can keep using your personal computers alongside it without a Mindwire runtime fee." },
        { question: "Can I stop the renewal?", answer: "Yes. Manage the subscription in your Mindwire account or the app. Canceling renewal keeps the paid period intact. It does not delete the computer or its files; retained storage follows the terms of your purchase." },
        { question: "Can I bring my existing cloud account?", answer: "Use the connection flow in your Mindwire account to link a supported existing account. Linking keeps existing computers, files and paid terms; it does not replace an existing subscription." },
      ]} />
    </section>
    <FinalInvite />
  </div>;
}
