import Link from "next/link";
import { ArrowRight, ArrowUpRight, BookOpen, Braces, Layers3, Palette, Target, Wrench } from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import OblienLogo from "@/components/OblienLogo";
import styles from "./agent-foundation.module.css";

export default function AgentFoundation() {
  return (
    <section className={styles.foundation} id="how-it-works" aria-labelledby="foundation-title">
      <div className={styles.heading}>
        <h2 id="foundation-title">The agent layer is already built.</h2>
        <p>Make it part of your product.</p>
      </div>
      <ol className={styles.flow}>
        <li className={styles.agents}>
          <h3>Start with a leading agent</h3>
          <div className={styles.providers}>
            <div><span><AgentLogo agent="codex" size={28} /></span><strong>Codex</strong></div>
            <div><span><AgentLogo agent="claude" size={29} /></span><strong>Claude Code</strong></div>
            <div><span><AgentLogo agent="opencode" size={27} /></span><strong>OpenCode</strong></div>
          </div>
          <p>Planning, coding, tool use, and execution. Built into the agent you choose.</p>
        </li>
        <li className={styles.mindwire}>
          <ArrowRight className={styles.connector} size={26} aria-hidden />
          <div className={styles.wordmark}><OblienLogo size={25} /><strong>Mindwire</strong></div>
          <h3>White-label it.<br />Make it work your way.</h3>
          <ul className={styles.customization}>
            <li><Palette size={17} aria-hidden /><span>Your brand & interface</span></li>
            <li><Wrench size={17} aria-hidden /><span>Your product’s tools</span></li>
            <li><BookOpen size={17} aria-hidden /><span>Your knowledge</span></li>
            <li><Target size={17} aria-hidden /><span>Your mindset & goals</span></li>
          </ul>
          <Link href="/sdk">One SDK and runtime <ArrowUpRight size={15} aria-hidden /></Link>
        </li>
        <li className={styles.product}>
          <ArrowRight className={styles.connector} size={26} aria-hidden />
          <span className={styles.productLabel}>What your users get</span>
          <h3>Your product.<br /><em>Now AI-powered.</em></h3>
          <div className={styles.outcomes}>
            <Link href="#in-action"><Braces size={18} aria-hidden /><span>Build working apps</span><ArrowUpRight size={14} aria-hidden /></Link>
            <Link href="#in-action"><Palette size={18} aria-hidden /><span>Create finished work</span><ArrowUpRight size={14} aria-hidden /></Link>
            <Link href="#automation"><Layers3 size={18} aria-hidden /><span>Complete workflows</span><ArrowUpRight size={14} aria-hidden /></Link>
          </div>
          <p>All inside the experience you design.</p>
        </li>
      </ol>
    </section>
  );
}
