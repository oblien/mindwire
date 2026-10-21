import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import BotAvatar from "@/components/landing/BotAvatar";
import InstallBlock from "@/components/landing/InstallBlock";
import { cloudDashboardUrl, macApp } from "@/lib/product";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Get started — Mindwire",
  description: "Connect a computer and your ChatGPT or Claude account, then give your personal bot its first task.",
  alternates: { canonical: "/get-started/" },
};

export default function GetStarted() {
  return <div className={base.landing}>
    <header className={styles.pageHero}><h1>Connect your computer.<br /><em>Put your AI to work.</em></h1><p>Your bot uses the computer’s files and tools. Your ChatGPT or Claude account powers it.</p></header>
    <div className={styles.pageWrap}>
      <div className={styles.setupLayout}>
        <aside className={styles.setupIntro}><BotAvatar size={82} /><h2>Start with<br />your computer.</h2><p>Keep it awake while your bot works. Or choose a Cloud computer that runs independently of yours.</p><Link href="/pricing" className={base.secondaryButton}>Explore Mindwire Cloud <ArrowUpRight size={16} /></Link></aside>
        <ol className={styles.setupSteps}>
          <li id="computer"><h3>Open Mindwire on your computer.</h3>{macApp ? <p><a href={macApp.href} className={styles.textLink}>Download Mindwire for Mac.</a> Open the app and choose <strong>Connect your phone</strong>.</p> : <p>Have the Mac app? Open it and choose <strong>Connect your phone</strong>. To install through Terminal on Mac or Linux, or PowerShell on Windows, use the commands below.</p>}<p>Install Node.js 18 or later first, then run:</p><InstallBlock pairing /><Link href="/docs/guides/personal-computers" className={styles.textLink}>Full installation guide <ArrowUpRight size={14} /></Link></li>
          <li id="iphone"><h3>Connect this phone.</h3><p>In the Mindwire mobile app, choose <strong>My computer</strong>, then <strong>Scan QR code</strong>. Scan the code on your computer and approve the connection there.</p><Link href="/download" className={styles.textLink}>Mac & iOS app availability <ArrowUpRight size={14} /></Link></li>
          <li id="account"><h3 id="agent">Connect ChatGPT or Claude.</h3><p>Choose your account in Mindwire and follow the sign-in instructions. If you’re already signed in on the computer, Mindwire checks that connection first. Your subscription’s access and usage limits apply.</p><Link href="/docs/guides/authentication" className={styles.textLink}>Account connection help <ArrowUpRight size={14} /></Link></li>
          <li><h3>Give your bot a task.</h3><p>Ask it to build a tool from your spreadsheet, organize a folder, or prepare a report. Add your files and describe the result you want.</p><Link href="/use-cases" className={styles.textLink}>Explore things you can make <ArrowUpRight size={14} /></Link></li>
        </ol>
      </div>
      <section className={styles.downloadGuide} id="server" aria-labelledby="other-homes"><h2 id="other-homes">Already have somewhere to work?</h2><div className={styles.includedGrid}>
        <div><h3>Your server</h3><p>Add an SSH connection in the app and choose a project. Use the server directly or a supported Docker workspace.</p><Link href="/docs/guides/destinations" className={styles.textLink}>Workspace destinations <ArrowUpRight size={14} /></Link></div>
        <div><h3>Mindwire Cloud</h3><p>Sign in to your Mindwire account, choose a monthly computer, then connect your agent and start a project.</p><a href={cloudDashboardUrl} className={styles.textLink}>Open your account <ArrowUpRight size={14} /></a></div>
        <div><h3>Your own product</h3><p>Use the SDK to start agent tasks and stream progress inside an experience you build.</p><Link href="/sdk" className={styles.textLink}>Explore the SDK <ArrowUpRight size={14} /></Link></div>
      </div></section>
    </div>
  </div>;
}
