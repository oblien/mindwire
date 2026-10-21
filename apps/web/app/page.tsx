import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "lucide-react";
import ServiceLogo from "@/components/marketing/ServiceLogo";
import HeroWorkspace from "@/components/landing/HeroWorkspace";
import ModeExplorer from "@/components/landing/ModeExplorer";
import PersonalSection from "@/components/landing/PersonalSection";
import BotAvatar from "@/components/landing/BotAvatar";
import ContinuitySection from "@/components/landing/ContinuitySection";
import SDKSection from "@/components/landing/SDKSection";
import Questions from "@/components/landing/Questions";
import FinalInvite from "@/components/landing/FinalInvite";
import styles from "@/components/landing/landing.module.css";
import product from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Mindwire — Your AI subscription. Put to work.",
  description:
    "Turn your ChatGPT or Claude subscription into personal bots that build apps, organize files, and carry out tasks on a computer.",
};

export default function Home() {
  return (
    <div className={styles.landing}>
      <section className={styles.hero} aria-labelledby="hero-title">
        <div className={styles.heroCopy}>
          <div className={`${styles.heroCompanion} ${styles.companionLeft}`} aria-hidden="true">
            <svg className={styles.companionTrail} viewBox="0 0 180 210" fill="none"><path d="M20 14c100-10 136 22 107 60S18 92 39 142s70 33 109 50" /><circle cx="20" cy="14" r="3" /><circle cx="148" cy="192" r="3" /></svg>
            <BotAvatar size={92} />
            <span>Your files. Your tools.</span>
          </div>
          <div className={`${styles.heroCompanion} ${styles.companionRight}`} aria-hidden="true">
            <BotAvatar character="robot" size={94} />
            <BotAvatar character="sprout" size={57} />
            <span>Your ideas, made real.</span>
            <svg className={styles.companionTrail} viewBox="0 0 180 210" fill="none"><path d="M20 14c90 0 127 25 127 78s-87 27-87 73 58 19 93 30" /><circle cx="20" cy="14" r="3" /><circle cx="153" cy="195" r="3" /></svg>
          </div>
          <h1 id="hero-title">Your AI subscription.<br /><span>Put to work.</span></h1>
          <p>Turn your ChatGPT or Claude subscription into personal bots that
            build apps, organize files, and carry out tasks on a computer.</p>
          <div className={styles.heroActions}>
            <Link href="/get-started" className={styles.primaryButton}>Get started <ArrowUpRight size={19} /></Link>
            <Link href="#demo" className={styles.secondaryButton}>See it work <ArrowDown size={17} /></Link>
          </div>
          <div className={styles.agentLine}>
            <span>Bring the subscription you already have.</span>
            <div>
              <Link href="/get-started#account"><ServiceLogo service="chatgpt" size={19} />ChatGPT</Link>
              <Link href="/get-started#account"><ServiceLogo service="claude" size={21} />Claude</Link>
            </div>
          </div>
        </div>
        <HeroWorkspace />
        <div className={styles.heroCaption}>
          <span>Give it a task. Open what it made.</span>
          <span>Interactive examples · No account needed</span>
        </div>
      </section>
      <PersonalSection />
      <ModeExplorer />
      <ContinuitySection />
      <SDKSection />
      <section className={`${product.section} ${product.faqSection}`} aria-labelledby="home-questions">
        <div className={product.sectionCopy}><span className={product.eyebrow}>A few good questions</span><h2 id="home-questions">Before you<br /><em>say hello.</em></h2><p>Getting a little team of your own should feel simple.</p><Link href="/get-started" className={product.textLink}>Walk me through it <ArrowUpRight size={17} /></Link></div>
        <Questions items={[
          { question: "Is this another AI subscription?", answer: <>Mindwire connects to supported agent accounts you already have. Your agent provider’s subscription or API usage stays separate. Use your own computer with no Mindwire runtime fee, or choose an optional <Link href="/pricing">monthly Cloud Computer</Link>.</> },
          { question: "What makes a bot mine?", answer: "Give it a name, a purpose and instructions. Add memory, reference material and reusable skills so it can work with your context. Your saved bots stay with your workspace." },
          { question: "Do I need to know how to code?", answer: "No. General mode focuses on conversations, files and the things your bots make for you. Developer mode brings projects, code changes, Git and terminals forward. Switch modes without starting over." },
          { question: "Where does the work actually happen?", answer: "On the computer you connect: your own machine, a server or a Mindwire Cloud Computer. Your agent uses that computer’s files and tools. Local computers need to stay awake and online during work." },
          { question: "Can I use Mindwire inside my own app?", answer: <>Yes. <Link href="/sdk">Mindwire SDK</Link> gives your product the same runtime: start tasks, stream progress and results, handle supported approvals and keep conversations going across agents.</> },
        ]} />
      </section>
      <FinalInvite />
    </div>
  );
}
