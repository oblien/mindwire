import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowUpRight, Cloud, Code2, GitBranch, Laptop, Radio, ShieldCheck, Smartphone } from "lucide-react";
import AgentFoundation from "@/components/landing/AgentFoundation";
import AgentEngine from "@/components/landing/AgentEngine";
import ProductStudio from "@/components/landing/ProductStudio";
import AutomationFlow from "@/components/landing/AutomationFlow";
import SDKExample from "@/components/landing/SDKExample";
import CopyButton from "@/components/landing/CopyButton";
import Questions from "@/components/landing/Questions";
import { ToolsIllustration, AgentsIllustration } from "@/components/landing/ProductIllustrations";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product-home.module.css";

export const metadata: Metadata = {
  title: "Mindwire — Your product. Powered by agents.",
  description: "Turn your product into an AI-powered product without rebuilding the agent layer. White-label and customize Codex, Claude Code, and OpenCode with Mindwire’s open-source SDK and runtime.",
  alternates: { canonical: "/" },
};

export default function Home() {
  return <div className={`${base.landing} ${styles.page}`}>
    <section className={styles.hero} aria-labelledby="hero-title">
      <div className={styles.heroIntro}>
        <ToolsIllustration />
        <AgentsIllustration />
        <h1 id="hero-title">Your product.<br /><em>Powered by agents.</em></h1>
        <p><strong>Make your product AI-powered. Skip rebuilding the agent layer.</strong> Use Mindwire to white-label and customize Codex, Claude Code, and OpenCode with your tools, knowledge, and brand.</p>
        <div className={styles.heroActions}><Link href="/docs/guides/quickstart" className={base.primaryButton}>Build with Mindwire <ArrowUpRight size={17} /></Link><Link href="#how-it-works" className={base.secondaryButton}>See how it fits <ArrowDown size={16} /></Link></div>
        <div className={styles.heroNote}><GitBranch size={13} />Open-source SDK & runtime<span>·</span>TypeScript, Go, and REST</div>
      </div>
      <AgentFoundation />
    </section>

    <section className={styles.section} aria-labelledby="creation-title">
      <div className={styles.creationIntro}>
        <h2 id="creation-title">This is what your<br /><em>product could do.</em></h2>
        <p>A user brings a brief. Your product builds the website, creates the campaign, and prepares the launch. The agent handles the work. Your users get the results, right inside your app.</p>
      </div>
      <AgentEngine />
    </section>

    <ProductStudio />

    <AutomationFlow />

    <section className={`${styles.section} ${styles.developer}`} aria-labelledby="integration-title">
      <div className={styles.developerCopy}>
        <h2 id="integration-title">Integrate the agent.<br /><em>Skip the reinvention.</em></h2>
        <p>Keep the planning, tool use, and execution that leading agents already provide. Mindwire handles the connection, sessions, and events. You build the features your users came for.</p>
        <ul className={styles.developerPoints}><li><Radio size={15} />Progress, tool activity, and results in one stream</li><li><ShieldCheck size={15} />Supported approvals in your own experience</li><li><Laptop size={15} />Run locally, on your server, or in the cloud</li></ul>
        <div className={styles.install}><code>npm install mindwire</code><CopyButton value="npm install mindwire" label="Copy SDK install command" /></div>
        <Link href="/docs/guides/quickstart" className={styles.inlineLink}>Build your first integration <ArrowUpRight size={16} /></Link>
      </div>
      <SDKExample compact />
    </section>

    <div className={styles.section}>
      <section className={styles.destinations} aria-label="Build, run, and use Mindwire">
        <div className={styles.destination}><Code2 size={23} /><div><h3>Build it into your product.</h3><p>The SDK and runtime work with your interface and infrastructure.</p><Link href="/sdk" className={styles.inlineLink}>Explore the SDK <ArrowUpRight size={14} /></Link></div></div>
        <div className={styles.destination}><Cloud size={23} /><div><h3>Give the work a home.</h3><p>Use your own host or an optional monthly Cloud Computer.</p><Link href="/cloud" className={styles.inlineLink}>Explore Cloud <ArrowUpRight size={14} /></Link></div></div>
        <div className={styles.destination}><Smartphone size={23} /><div><h3>See it in everyday use.</h3><p>The Mindwire app puts the same runtime in your hands.</p><Link href="/app" className={styles.inlineLink}>Meet the app <ArrowUpRight size={14} /></Link></div></div>
      </section>
    </div>

    <section className={`${styles.section} ${styles.faq}`} aria-labelledby="home-questions">
      <div className={styles.sectionIntro}><h2 id="home-questions">A head start.<br /><em>On your terms.</em></h2><p>Use the agent’s capabilities. Keep control of your product.</p></div>
      <Questions items={[
        { question: "What does Mindwire save me from building?", answer: "You reuse the planning, coding, tool use, and execution already built into native agent harnesses such as Codex, Claude Code, and OpenCode. Mindwire connects them to your application through one SDK and runtime, handling sessions, authentication, and event translation. You supply your product’s interface, tools, and context." },
        { question: "What does white-label mean here?", answer: "You build the interface and decide how agent capabilities appear in your product. Use your own brand, tools, knowledge, instructions, and goals. Your users stay in your app; the Mindwire app is not required. The native agent still runs underneath, with its own capabilities and provider terms." },
        { question: "Can I bring custom tools, knowledge, and goals?", answer: <>Yes. Connect <Link href="/docs/guides/mcp">supported MCP tools</Link>, supply files and knowledge, and shape the work with instructions, skills, and task goals. Supported memory and configuration features vary by agent.</> },
        { question: "What can start an automation?", answer: "Start a task on demand, configure a supported schedule, or have your backend start a turn in response to an application event. The runtime host must remain running; a task can wait when it needs input or approval." },
        { question: "Do I need Cloud or a new AI subscription?", answer: <>Cloud is optional. Run on infrastructure you manage and use an account or API key supported by your chosen agent. Provider terms, capabilities, limits, and usage charges apply separately. <Link href="/pricing">See how costs work.</Link></> },
      ]} />
    </section>

    <section className={styles.closing} aria-labelledby="closing-title"><h2 id="closing-title">Your next AI feature.<br /><em>Already has a head start.</em></h2><p>The agent layer is ready. Bring your tools, knowledge, and brand.<br />Make it part of your product with Mindwire.</p><div className={styles.heroActions}><Link href="/docs/guides/quickstart" className={base.primaryButton}>Start building <ArrowUpRight size={17} /></Link><a href="https://github.com/oblien/mindwire" className={base.secondaryButton}>Explore the source <GitBranch size={16} /></a></div><span>Open source. Your infrastructure. Your product.</span></section>
  </div>;
}
