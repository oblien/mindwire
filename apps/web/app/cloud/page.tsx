import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight, Cloud, Folder, Laptop, Server } from "lucide-react";
import Questions from "@/components/landing/Questions";
import { cloudDashboardUrl } from "@/lib/product";
import base from "@/components/landing/landing.module.css";
import product from "@/components/landing/product.module.css";
import styles from "@/components/landing/platform.module.css";

export const metadata: Metadata = {
  title: "Mindwire Cloud — A computer for your agent work",
  description: "Optional monthly Cloud Computers for the Mindwire runtime. Keep files and projects on an online host, with your agent account and model usage separate.",
  alternates: { canonical: "/cloud/" },
};

export default function CloudPage() {
  return <div className={base.landing}>
    <header className={product.pageHero}><span className={product.eyebrow}>Mindwire Cloud · Optional hosting</span><h1>A computer for<br /><em>the work ahead.</em></h1><p>Give your agent tasks an online workspace with the files and tools they need. Use a monthly Cloud Computer when you want work to run beyond your own laptop.</p><div className={styles.actions} style={{ justifyContent: "center" }}><Link href="/pricing#cloud-plans" className={base.primaryButton}>See Cloud plans <ArrowUpRight size={17} /></Link><a href={cloudDashboardUrl} className={base.secondaryButton}>Open Console <ArrowUpRight size={17} /></a></div></header>
    <section className={styles.cloudSteps} aria-labelledby="cloud-capabilities"><h2 id="cloud-capabilities">A host for the same runtime.</h2><div className={styles.productGrid}>
      <article><span className={styles.productIcon}><Server size={25} /></span><h3>Run your workspace.</h3><p>Connect a supported agent account and work with its tools on your Cloud Computer. Agent usage remains separate from hosting.</p></article>
      <article><span className={styles.productIcon} data-tone="mint"><Folder size={25} /></span><h3>Keep files together.</h3><p>Keep projects and output on the computer that runs the task. Manage your computer and subscription through Console.</p></article>
      <article><span className={styles.productIcon} data-tone="peach"><Laptop size={25} /></span><h3>Use your preferred client.</h3><p>Connect through the Mindwire app or supported SDK destinations. Your client can disconnect while work continues on the running host.</p></article>
    </div></section>
    <section className={styles.hosts} aria-labelledby="cloud-choice"><div><Cloud size={29} /><h2 id="cloud-choice">Cloud is one way to run Mindwire.</h2><p>The SDK and runtime also work locally, on a server you manage, and in supported Docker environments. Choose the host that fits your application.</p></div><div className={styles.hostLinks}><Link href="/docs/guides/destinations">Compare runtime destinations <ArrowUpRight size={16} /></Link><Link href="/pricing">Understand hosting and AI costs <ArrowUpRight size={16} /></Link></div></section>
    <section className={`${product.section} ${product.faqSection}`} aria-labelledby="cloud-questions"><div className={product.sectionCopy}><span className={product.eyebrow}>Before you choose</span><h2 id="cloud-questions">Hosting, with<br /><em>clear boundaries.</em></h2></div><Questions items={[
      { question: "Is this a model subscription?", answer: "No. A Cloud plan provides a computer. Use an account or API key supported by your chosen agent; provider limits, terms, and usage charges apply separately." },
      { question: "What am I paying for?", answer: <>Each computer has its own monthly plan for the listed CPU, memory, and included storage. Check <Link href="/pricing">pricing</Link> and checkout for availability and any separate transfer, storage, or backup terms.</> },
      { question: "Can I use my own infrastructure instead?", answer: "Yes. Mindwire’s open-source SDK and runtime can run on hardware and infrastructure you manage. A Cloud subscription is optional." },
      { question: "Does my phone need to stay open?", answer: "No. Execution happens on the computer. Tasks and configured schedules can continue while the client is closed, as long as the host is running and the work is not waiting for input." },
    ]} /></section>
  </div>;
}
