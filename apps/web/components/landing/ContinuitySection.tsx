"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowUpRight, Cloud, Laptop } from "lucide-react";
import ContinuityScene from "./ContinuityScene";
import { navigateTabs } from "./tabs";
import base from "./landing.module.css";
import styles from "./product.module.css";

export default function ContinuitySection() {
  const [cloud, setCloud] = useState(false);
  return <section className={`${styles.section} ${styles.continuity}`} id="anywhere" aria-labelledby="anywhere-title">
    <ContinuityScene cloud={cloud} />
    <div className={styles.sectionCopy}>
      <span className={styles.eyebrow}>One workspace across your devices</span>
      <h2 id="anywhere-title">Start at your desk.<br /><em>Continue on your phone.</em></h2>
      <p>Start at your desk. Pick it up on your phone. The agent works on a real computer, with the files and tools they need to get things done.</p>
      <div className={styles.segmented} role="tablist" aria-label="Where the work runs" onKeyDown={navigateTabs}>
        <button type="button" role="tab" id="host-local" aria-selected={!cloud} aria-controls="host-details" tabIndex={!cloud ? 0 : -1} onClick={() => setCloud(false)}><Laptop size={16} />Your computer</button>
        <button type="button" role="tab" id="host-cloud" aria-selected={cloud} aria-controls="host-details" tabIndex={cloud ? 0 : -1} onClick={() => setCloud(true)}><Cloud size={16} />Mindwire Cloud</button>
      </div>
      <div className={styles.hostDetails} role="tabpanel" id="host-details" aria-labelledby={cloud ? "host-cloud" : "host-local"}>
        <h3>{cloud ? "Close the laptop. Keep the momentum." : "Start right where you are."}</h3>
        <p>{cloud ? "Use a monthly Cloud Computer as an online workspace. Keep projects and scheduled work running while your own computer takes a break." : "Use your Mac, Windows or Linux computer with no Mindwire runtime fee. Keep it awake and connected while tasks are running."}</p>
        <Link href={cloud ? "/cloud#cloud-plans" : "/get-started"} className={base.secondaryButton}>{cloud ? "Find your Cloud plan" : "Connect your computer"}<ArrowUpRight size={16} /></Link>
      </div>
    </div>
  </section>;
}
