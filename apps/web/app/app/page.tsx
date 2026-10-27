import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowUpRight, Folder, Radio, ShieldCheck, Smartphone } from "lucide-react";
import AppWorkspacePreview from "@/components/landing/AppWorkspacePreview";
import ModeExplorer from "@/components/landing/ModeExplorer";
import PersonalSection from "@/components/landing/PersonalSection";
import ContinuitySection from "@/components/landing/ContinuitySection";
import FinalInvite from "@/components/landing/FinalInvite";
import Questions from "@/components/landing/Questions";
import base from "@/components/landing/landing.module.css";
import product from "@/components/landing/product.module.css";
import styles from "@/components/landing/app-showcase.module.css";

export const metadata: Metadata = {
  title: "Mindwire App — Your work, with you everywhere",
  description: "A workspace for your agents, files, and ideas. Work from Mac, iPhone, or iPad, follow progress, and review the next step in General or Developer mode.",
  alternates: { canonical: "/app/" },
};

export default function AppPage() {
  return <div className={base.landing}>
    <section className={styles.hero} aria-labelledby="app-title">
      <div className={styles.intro}>
        <span className={styles.label}><Smartphone size={15} />The Mindwire App · Mac, iPhone & iPad</span>
        <h1 id="app-title">Your work.<br /><em>With you, everywhere.</em></h1>
        <p>A home for your agents, files, and ideas. Start something at your desk, follow its progress, and pick up the next step from your phone.</p>
        <div className={styles.actions}><Link href="/download" className={base.primaryButton}>Get the app <ArrowUpRight size={17} /></Link><Link href="#modes" className={base.secondaryButton}>Find your way to work <ArrowDown size={16} /></Link></div>
      </div>
      <AppWorkspacePreview />
    </section>
    <section className={styles.benefits} aria-label="What the Mindwire app brings together">
      <div><Radio size={21} /><h2>Know where the work stands.</h2><p>Follow agent progress and tool activity in the same place as the conversation.</p></div>
      <div><Folder size={21} /><h2>Keep the context close.</h2><p>Your projects, source files, and finished results stay part of the workspace.</p></div>
      <div><ShieldCheck size={21} /><h2>Stay in the decisions.</h2><p>Review the work and respond to supported questions and approvals across devices.</p></div>
    </section>
    <ModeExplorer />
    <PersonalSection />
    <ContinuitySection />
    <section className={`${product.section} ${product.faqSection}`} aria-labelledby="app-questions">
      <div className={`${product.sectionCopy} ${styles.faqHeading}`}><span className={product.eyebrow}>Make yourself at home</span><h2 id="app-questions">A few things<br /><em>before you start.</em></h2><p>Choose a computer, connect a supported agent, and start with something you need done.</p></div>
      <Questions items={[
        { question: "Which mode should I use?", answer: "General mode keeps conversations, files, and finished results up front. Developer mode exposes projects, code changes, Git, and the terminal. Switch views as the work changes." },
        { question: "Does the work run on my phone?", answer: "The work runs on a connected computer: your Mac, a server, or an optional Cloud Computer. Your phone lets you follow progress and interact with the workspace. The host needs to stay running while tasks execute." },
        { question: "Can I use my existing agent account?", answer: "Connect an account or API key supported by your selected agent. Available authentication methods, provider terms, limits, and usage charges still apply." },
        { question: "Where can I get the app?", answer: <>See <Link href="/download">downloads</Link> for current Mac and iOS availability, then follow the <Link href="/get-started">app setup guide</Link> to connect your computer and agent.</> },
      ]} />
    </section>
    <FinalInvite audience="app" />
  </div>;
}
