import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowRight, ArrowUpRight } from "lucide-react";
import StudyNav from "@/components/design-lab/StudyNav";
import EditorialDetails from "@/components/design-lab/EditorialDetails";
import CopyInstall from "@/components/design-lab/CopyInstall";
import styles from "@/components/design-lab/studies.module.css";

export const metadata: Metadata = { title: "02 / Editorial — Mindwire design study" };
export default function EditorialStudy() {
  return <div className={styles.editorial}>
    <StudyNav variant="editorial"/>
    <section className={styles.editorialHero} aria-labelledby="editorial-title">
      <div className={styles.editorialMasthead}><span>INDEPENDENT TOOLS FOR AMBITIOUS PRODUCTS.</span><span>EST. NOW → BUILT FOR NEXT</span></div>
      <h1 id="editorial-title">Intelligence.<br/>With your<br/><span>signature.</span><span className={styles.editorialAsterisk} aria-hidden="true">✳</span></h1>
      <div className={styles.editorialHeroBottom}><a href="#concept" className={styles.editorialExplore}>A DIFFERENT KIND<br/>OF AGENT LAYER <ArrowDown size={24}/></a><div><p>The world’s best agents.<br/>Your unmistakable product.</p><Link href="/docs/guides/quickstart">Make it yours <ArrowUpRight size={20}/></Link></div><span className={styles.editorialIssue}>M—W<br/><small>OPEN SOURCE<br/>BY DESIGN</small></span></div>
    </section>
    <section id="concept" className={styles.editorialManifesto}><span className={styles.eyebrow}>01 — THE POINT OF VIEW</span><div><h2>Great intelligence<br/>deserves a point<br/>of view. <span>Yours.</span></h2><div className={styles.editorialManifestoBottom}><span aria-hidden="true">↗</span><p>AI shouldn’t make every product feel the same. Mindwire gives you the agent layer, so you can focus on the things that make your product worth using.</p></div></div></section>
    <section id="inside" className={styles.editorialInside}><div className={styles.editorialSectionHead}><span className={styles.eyebrow}>02 — THE RAW MATERIALS</span><p>Everything you’ve built.<br/>Now with more behind it.</p></div><EditorialDetails/></section>
    <section className={styles.editorialStatement}><span>THEIR INTELLIGENCE.</span><span className={styles.editorialOutline}>YOUR EXPRESSION.</span><div><span>NO TWO PRODUCTS SHOULD FEEL THE SAME.</span><ArrowUpRight strokeWidth={1} size={64}/></div></section>
    <section className={styles.editorialBuild}><div><span className={styles.eyebrow}>03 — FROM THE FIRST LINE</span><h2>Less infrastructure.<br/>More authorship.</h2></div><div><p>An open-source SDK and runtime. The agents you choose. The tools you already have. A starting point for something distinctly yours.</p><CopyInstall/><Link href="/docs">Read the documentation <ArrowRight size={18}/></Link></div></section>
    <footer className={styles.editorialFooter}><div><span>THE NEXT CHAPTER IS YOURS.</span><Link href="/docs/guides/quickstart">Start writing it <ArrowUpRight size={25}/></Link></div><Link href="/design-lab" className={styles.editorialFooterWord}>mindwire<span>®</span></Link><div><span>AN OPEN INVITATION TO BUILD.</span><Link href="/docs">DOCUMENTATION ↗</Link><span>MADE TO BE MADE YOURS.</span></div></footer>
  </div>;
}
