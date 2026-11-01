import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight } from "lucide-react";
import BotAvatar from "@/components/landing/BotAvatar";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/resource-pages.module.css";

export const metadata: Metadata = { title: "Page not found — MindWire" };

export default function NotFound() {
  return <section className={styles.hero} style={{ minHeight: "65vh" }}>
    <div className="mb-7 flex justify-center"><BotAvatar character="sprout" size={82} /></div>
    <p className={styles.kicker}>404 · A small detour</p>
    <h1 style={{ fontSize: "clamp(36px,5vw,60px)" }}>This page wandered off.</h1>
    <p>The link may have changed, or the page is no longer here. Let’s find you a better starting point.</p>
    <div className={styles.actions}><Link href="/" className={base.primaryButton}>Back to MindWire<ArrowRight size={17} /></Link><Link href="/docs" className={base.secondaryButton}>Browse the docs</Link></div>
  </section>;
}
