import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowRight, ArrowUpRight } from "lucide-react";
import StudyNav from "@/components/design-lab/StudyNav";
import TerminalDemo from "@/components/design-lab/TerminalDemo";
import SignalScope from "@/components/design-lab/SignalScope";
import CopyInstall from "@/components/design-lab/CopyInstall";
import styles from "@/components/design-lab/studies.module.css";

export const metadata: Metadata = { title: "03 / Electronic — Mindwire design study" };
const channels = [
  { number: "01", name: "CONTEXT", description: "Give the agent a world to work in.", inputs: "FILES / KNOWLEDGE / INSTRUCTIONS", output: "UNDERSTANDING" },
  { number: "02", name: "CAPABILITY", description: "Connect the things your product can do.", inputs: "TOOLS / ACTIONS / WORKFLOWS", output: "EXECUTION" },
  { number: "03", name: "EXPRESSION", description: "Make the experience unmistakably yours.", inputs: "INTERFACE / BRAND / PERSONALITY", output: "YOUR PRODUCT" },
];
export default function ElectronicStudy() {
  return <div className={styles.electronic}>
    <StudyNav variant="electronic"/>
    <section className={styles.electronicHero} aria-labelledby="electronic-title"><div className={styles.electronicHeroCopy}><span className={styles.electronicEyebrow}><i/> AN OPEN CIRCUIT FOR YOUR IDEAS</span><h1 id="electronic-title">Your product.<br/>On a new<br/><span>frequency.</span></h1><p>Wire leading agents into your product.<br/>Your context in. New capabilities out.<br/>The experience stays entirely yours.</p><Link href="/docs/guides/quickstart" className={styles.electronicButton}>MAKE THE CONNECTION <ArrowUpRight size={18}/></Link></div><SignalScope/><div className={styles.electronicHeroRail}><span>OPEN SOURCE SDK + RUNTIME</span><span>BUILT FOR YOUR SIGNAL</span><a href="#concept">TRACE THE CIRCUIT <ArrowDown size={14}/></a></div></section>
    <section id="concept" className={styles.electronicChannels}><div className={styles.electronicSectionHead}><span>01 / THE CONNECTION</span><h2>Everything in your world.<br/><span>Working on the same wavelength.</span></h2></div><div className={styles.channelHeader}><span>CHANNEL</span><span>INPUT</span><span>OUTPUT</span></div>{channels.map(channel => <div className={styles.channelRow} key={channel.number}><div><span>{channel.number}</span><h3>{channel.name}</h3><p>{channel.description}</p></div><span>{channel.inputs}</span><span><i/> {channel.output}<ArrowUpRight size={17}/></span></div>)}<div className={styles.channelFoot}><span>YOU DEFINE THE SYSTEM.</span><span>MINDWIRE CONNECTS THE PARTS. ↗</span></div></section>
    <section id="inside" className={styles.electronicRuntime}><div><span className={styles.electronicEyebrow}>02 / WATCH THE SIGNAL</span><h2>From intent<br/>to <span>output_</span></h2><p>A conversation starts the process. Context gives it direction. Connected tools let the agent do the work.</p><div className={styles.electronicLegend}><span><i/> CONTEXT LOADED</span><span><i/> AGENT CONNECTED</span><span><i/> YOUR UI IN CONTROL</span></div></div><TerminalDemo/></section>
    <section className={styles.electronicBuild}><div><span className={styles.electronicEyebrow}>03 / THE STARTING SIGNAL</span><h2>One connection.<br/>Your entire world.</h2><p>Bring the runtime to your product.<br/>Take the experience wherever you want.</p><CopyInstall/><Link href="/docs">OPEN DOCUMENTATION <ArrowRight size={16}/></Link></div><div className={styles.circuit} role="img" aria-label="Your context, tools, and interface connect through Mindwire to your choice of agent"><div className={styles.circuitSources}><span>CONTEXT</span><span>TOOLS</span><span>YOUR UI</span></div><svg viewBox="0 0 120 200" preserveAspectRatio="none" aria-hidden="true"><path d="M0 24H38V100H120M0 100H120M0 176H38V100"/><circle cx="38" cy="100" r="4"/></svg><div className={styles.circuitCore}><span>mw_</span><small>AGENT RUNTIME</small></div><svg viewBox="0 0 80 200" aria-hidden="true"><path d="M0 100H80"/></svg><div className={styles.circuitOutput}><span>YOUR</span><span>CHOICE</span><small>OF AGENT</small></div><div className={styles.circuitCaption}>SYSTEM MAP / YOUR PRODUCT REMAINS THE INTERFACE</div></div></section>
    <section className={styles.electronicClosing}><span className={styles.electronicEyebrow}>CONNECTION READY.</span><h2>MAKE<br/>CONTACT<span>↗</span></h2><Link href="/docs/guides/quickstart" className={styles.electronicButton}>INITIALIZE YOUR FIRST AGENT <ArrowUpRight size={19}/></Link><div className={styles.closingBars} aria-hidden="true">{Array.from({length:56},(_,i)=><i key={i} style={{height:`${18+Math.sin(i*.13)**2*100}px`}}/>)}</div></section>
    <footer className={styles.electronicFooter}><Link href="/design-lab" className={styles.wordmark}>mindwire_</Link><span>KEEP YOUR SIGNAL. EXTEND YOUR REACH.</span><Link href="/docs">DOCS ↗</Link></footer>
  </div>;
}
