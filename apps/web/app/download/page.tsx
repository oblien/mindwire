import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight, Clock3, Laptop, Smartphone } from "lucide-react";
import { MacArt, PhoneArt } from "@/components/landing/DeviceArt";
import InstallBlock from "@/components/landing/InstallBlock";
import { githubUrl, iosApp, macApp } from "@/lib/product";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Download for Mac & iPhone — Mindwire",
  description: "Use the Mindwire runtime at your desk and on the go. Find Mindwire app availability for Mac, iPhone and iPad, or start today with the command-line runtime.",
  alternates: { canonical: "/download/" },
};

export default function Download() {
  return <div className={base.landing}>
    <header className={styles.pageHero}><span className={styles.eyebrow}>Mindwire App · Mac, iPhone & iPad</span><h1>At your desk.<br /><em>Out in the world.</em></h1><p>Use the Mindwire app to start tasks, work with files, and handle approvals. Choose General or Developer mode for the work in front of you.</p></header>
    <div className={styles.pageWrap}>
      <div className={styles.downloadGrid}>
        <section className={styles.downloadCard} id="mac" aria-labelledby="mac-title">
          <div className={styles.downloadCardHeader}><div><h2 id="mac-title">Mindwire for Mac</h2><p>A workspace for tasks, files, code, and results. Use the same runtime available through the SDK.</p></div><Laptop size={25} /></div>
          <div className={styles.downloadVisual}><MacArt /></div>
          <div className={styles.downloadFoot}>{macApp ? <a href={macApp.href} className={base.primaryButton}>{macApp.label}<ArrowUpRight size={16} /></a> : <span><Clock3 size={14} />Mac app coming soon</span>}<Link href="#cli" className={styles.textLink}>Use the CLI today <ArrowUpRight size={14} /></Link></div>
          <p className={styles.downloadCaption}>App preview · macOS 26 or later</p>
        </section>
        <section className={styles.downloadCard} id="iphone" aria-labelledby="iphone-title">
          <div className={styles.downloadCardHeader}><div><h2 id="iphone-title">Mindwire for iOS</h2><p>Send an idea. Check the progress. Pick up where your computer left off.</p></div><Smartphone size={25} /></div>
          <div className={styles.downloadVisual}><PhoneArt /></div>
          <div className={styles.downloadFoot}>{iosApp ? <a href={iosApp.href} className={base.primaryButton}>{iosApp.label}<ArrowUpRight size={16} /></a> : <span><Clock3 size={14} />iOS app coming soon</span>}<Link href="/get-started#iphone" className={styles.textLink}>Connection guide <ArrowUpRight size={14} /></Link></div>
          <p className={styles.downloadCaption}>App preview · iPhone & iPad · iOS 26 or later</p>
        </section>
      </div>
      <section className={styles.cliSection} id="cli" aria-labelledby="cli-title">
        <div><div className={styles.platformList}><span>macOS</span><span>Windows</span><span>Linux</span></div><h2 id="cli-title">A terminal is a good place to start.</h2><p>The Mindwire runtime is available today. Install it, connect your computer, and use it with the app or build with the SDK.</p><a href={`${githubUrl}/releases/latest`} className={styles.textLink}>Binaries & release notes <ArrowUpRight size={15} /></a></div>
        <div><InstallBlock /><span className={styles.smallNote}>Requires Node.js 18+. Pairing instructions appear in your terminal.</span></div>
      </section>
      <section className={styles.downloadGuide} aria-labelledby="download-guide-title"><h2 id="download-guide-title">Set up the app in three steps.</h2><ol className={styles.steps}>
        <li><span>01</span><h3>Choose a computer.</h3><p>Use your own computer, connect a server, or choose a Mindwire Cloud Computer.</p></li>
        <li><span>02</span><h3>Bring your agent.</h3><p>Connect a supported Codex, Claude Code or OpenCode account through its own sign-in flow.</p></li>
        <li><span>03</span><h3>Start a task.</h3><p>Choose General or Developer mode, add the context you need, and describe the result you want.</p></li>
      </ol><Link href="/get-started" className={styles.textLink}>Follow the setup guide <ArrowUpRight size={16} /></Link></section>
      <div className={styles.sdkClosing}><div><h2>Prefer to leave your laptop closed?</h2><p>Run your workspace on an optional Mindwire Cloud Computer.</p></div><Link href="/pricing" className={base.primaryButton}>Explore Cloud plans <ArrowUpRight size={16} /></Link></div>
    </div>
  </div>;
}
