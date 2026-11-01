"use client";

import Link from "next/link";
import { RefreshCw } from "lucide-react";
import BotAvatar from "@/components/landing/BotAvatar";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/resource-pages.module.css";

export default function ErrorPage({ retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return <section className={styles.hero} style={{ minHeight: "65vh" }}>
    <div className="mb-7 flex justify-center"><BotAvatar character="robot" size={82} /></div>
    <h1 style={{ fontSize: "clamp(36px,5vw,60px)" }}>Let’s give that another try.</h1>
    <p>This page couldn’t load. Try again, or head back home.</p>
    <div className={styles.actions}><button type="button" onClick={retry} className={base.primaryButton}>Try again<RefreshCw size={17} /></button><Link href="/" className={base.secondaryButton}>Back to MindWire</Link></div>
  </section>;
}
