import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowRight, ArrowUpRight } from "lucide-react";
import BotAvatar from "@/components/landing/BotAvatar";
import SecretaryPreview from "@/components/landing/SecretaryPreview";
import UseCaseExplorer from "@/components/landing/UseCaseExplorer";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/use-cases.module.css";

export const metadata: Metadata = {
  title: "Mindwire App — Tasks and examples",
  description: "Explore tasks you can try in the Mindwire app, from preparing reports to building tools, with starter prompts and context to provide.",
  alternates: { canonical: "/use-cases/" },
};

export default function UseCasesPage() {
  return <div className={base.landing}>
    <section className={styles.hero} aria-labelledby="use-cases-title">
      <div className={styles.heroCopy}><span className={styles.heroLabel}><BotAvatar size={32} />Mindwire App · Examples</span><h1 id="use-cases-title">A little help.<br /><em>In a lot of ways.</em></h1><p>Prepare a meeting brief, organize your notes, or build a useful tool. Explore tasks to try in the Mindwire app with the files and context you provide.</p><div className={styles.heroActions}><Link href="#use-case-library" className={base.primaryButton}>Find your first idea<ArrowDown size={16} /></Link><Link href="/use-cases/personal-secretary" className={styles.textLink}>Meet your secretary<ArrowRight size={15} /></Link></div><span className={styles.heroNote}>Your everyday. Your work. Your next good idea.</span></div>
      <div className={styles.featuredScene}>
        <svg className={styles.sceneWires} viewBox="0 0 560 590" fill="none" aria-hidden="true"><path d="M0 130h28q20 0 20 20v65q0 20 20 20h25M560 380h-29q-20 0-20 20v49q0 20-20 20h-26M125 590v-35q0-20 20-20h38" /><g><circle cx="93" cy="235" r="3" /><circle cx="465" cy="469" r="3" /><circle cx="183" cy="535" r="3" /></g></svg>
        <SecretaryPreview framed />
        <p className={styles.previewCaption}>Try the brief · An example built from your notes</p>
      </div>
    </section>
    <UseCaseExplorer />
    <section className={styles.makeItYours} aria-labelledby="make-it-yours-title">
      <div className={styles.makeItYoursIntro}><BotAvatar character="sprout" size={62} /><h2 id="make-it-yours-title">The best use case?<br /><em>Probably yours.</em></h2><p>These are starting points. Useful results start with the right files, tools, and a clear request.</p><Link href="/get-started" className={base.primaryButton}>Set up the app<ArrowUpRight size={16} /></Link></div>
      <ol className={styles.startSteps}><li><span>01</span><div><h3>Give it a job.</h3><p>Choose a purpose and describe what a good result looks like. A short, specific request goes a long way.</p></div></li><li><span>02</span><div><h3>Bring your context.</h3><p>Share the files, notes and preferences that matter. Add the tools you want it to use in its workspace.</p></div></li><li><span>03</span><div><h3>Keep shaping it.</h3><p>Review what it makes. Ask for a change, try another approach, or turn a useful result into a tool you can return to.</p></div></li></ol>
    </section>
  </div>;
}
