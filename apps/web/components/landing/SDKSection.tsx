import Link from "next/link";
import { ArrowUpRight, Check } from "lucide-react";
import SDKExample from "./SDKExample";
import base from "./landing.module.css";
import styles from "./product.module.css";

export default function SDKSection() {
  return <section className={styles.sdkSection} aria-labelledby="sdk-section-title">
    <div className={styles.sdkSectionInner}>
      <div className={styles.sectionCopy}>
        <span className={styles.eyebrow}>Build with the same runtime</span>
        <h2 id="sdk-section-title">Make this capability<br /><em>part of your product.</em></h2>
        <p>Put Codex, Claude Code and OpenCode to work for your users. Mindwire SDK brings their task execution, streaming, and supported interactions into your app through one consistent API.</p>
        <ul className={styles.checkList}><li><Check size={16} />Stream progress, tool calls and results</li><li><Check size={16} />Build your interface around real agent work</li><li><Check size={16} />Run locally, on your server or in the cloud</li></ul>
        <Link href="/sdk" className={base.primaryButton}>Build with Mindwire <ArrowUpRight size={17} /></Link>
        <span className={styles.smallNote}>TypeScript · Go · REST + SSE</span>
      </div>
      <SDKExample compact />
    </div>
  </section>;
}
