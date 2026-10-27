import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight, Check, Code2, GitPullRequest, Radio, SlidersHorizontal } from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import SDKExample from "@/components/landing/SDKExample";
import InstallBlock from "@/components/landing/InstallBlock";
import Questions from "@/components/landing/Questions";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Mindwire SDK — Add AI without rebuilding the agent layer",
  description: "White-label and customize leading agent harnesses with one open-source SDK and runtime. Your tools, knowledge, goals, and interface. TypeScript, Go, and REST.",
  alternates: { canonical: "/sdk/" },
};

export default function SDK() {
  return <div className={base.landing}>
    <section className={styles.sdkHero} aria-labelledby="sdk-title">
      <div className={styles.sdkHeroCopy}><span className={styles.eyebrow}>Mindwire SDK</span><h1 id="sdk-title">The agent layer.<br /><em>Ready for your product.</em></h1><p>White-label and customize leading agents through one SDK. Add your tools, knowledge, instructions, and goals. Build advanced AI features on top of the planning and execution already in the agent.</p><InstallBlock sdk /><Link href="/docs/guides/quickstart" className={base.primaryButton}>Build your first integration <ArrowUpRight size={17} /></Link><div className={styles.sdkAgentLine}><span><AgentLogo agent="codex" size={17} />Codex</span><span><AgentLogo agent="claude" size={17} />Claude Code</span><span><AgentLogo agent="opencode" size={16} />OpenCode</span></div><span className={styles.smallNote}>Examples run on your server with a configured agent account.</span></div>
      <SDKExample />
    </section>
    <section className={styles.sdkFeatureBand} aria-labelledby="sdk-features-title"><h2 id="sdk-features-title">The agent does the work.<br />You own the experience.</h2><p>A shared protocol for agent conversations, tools and results. Keep your product focused on what your users want to make.</p><div className={styles.sdkFeatures}>
      <div><Radio size={25} strokeWidth={1.5} /><h3>Make progress visible.</h3><p>Stream text, reasoning, tool calls and results. Reattach to a run’s event stream when a client reconnects.</p><Link href="/docs/guides/streaming" className={styles.textLink}>Explore streaming <ArrowUpRight size={14} /></Link></div>
      <div><GitPullRequest size={25} strokeWidth={1.5} /><h3>Go beyond the chat box.</h3><p>Render file changes, commands and tool activity with normalized events, while keeping the original agent details available.</p><Link href="/docs/guides/tool-events" className={styles.textLink}>Understand tool events <ArrowUpRight size={14} /></Link></div>
      <div><SlidersHorizontal size={25} strokeWidth={1.5} /><h3>Keep people in the loop.</h3><p>Present supported questions and approvals in your own interface. Continue, follow up or cancel work through the same runtime.</p><Link href="/docs/guides/interaction" className={styles.textLink}>Handle interactions <ArrowUpRight size={14} /></Link></div>
    </div></section>
    <section className={`${styles.section} ${styles.architecture}`} aria-labelledby="runtime-title">
      <div className={styles.architectureScene}>
        <div className={styles.architectureApp}><Code2 size={27} /><div>Your product<small>Your interface. Your users. Your ideas.</small></div></div><div className={styles.architectureConnector} />
        <div className={styles.architectureCore}><strong>Mindwire runtime</strong><p>TypeScript · Go · REST + SSE</p><div className={styles.architectureCapabilities}><span>Conversations</span><span>Tool events</span><span>Approvals</span><span>Optional profiles</span></div></div>
        <div className={styles.architectureConnector} /><div className={styles.architectureEngines}><span><AgentLogo agent="codex" size={20} />Codex</span><span><AgentLogo agent="claude" size={20} />Claude Code</span><span><AgentLogo agent="opencode" size={19} />OpenCode</span></div><div className={styles.architectureHosts}><span>Local</span><span>SSH server</span><span>Docker</span><span>Cloud</span></div>
      </div>
      <div className={styles.sectionCopy}><span className={styles.eyebrow}>One integration. Plenty of possibilities.</span><h2 id="runtime-title">Your app up front.<br /><em>Real agents behind it.</em></h2><p>Turn your editor into an app builder, your workspace into a creative studio, or your product’s tools into an automated workflow. Use one integration while each agent keeps its native capabilities.</p><ul className={styles.checkList}><li><Check size={16} />Optional profiles with instructions, memory and knowledge</li><li><Check size={16} />A local runtime or a remote workspace</li><li><Check size={16} />Your infrastructure and provider accounts</li></ul><Link href="/docs/guides/destinations" className={styles.textLink}>Choose where it runs <ArrowUpRight size={16} /></Link></div>
    </section>
    <section className={`${styles.section} ${styles.faqSection}`} aria-labelledby="sdk-questions" style={{ paddingTop: 0 }}>
      <div className={styles.sectionCopy}><span className={styles.eyebrow}>A few implementation details</span><h2 id="sdk-questions">Built to fit.<br /><em>Yours to shape.</em></h2><p>The same runtime powers the Mindwire app and your integration.</p></div>
      <Questions items={[
        { question: "What do I install?", answer: <>Install <code>mindwire</code> for TypeScript. On Node or Bun, the default target starts a local daemon for you. Go embeds the runtime directly. For plain HTTP, run the daemon and use REST plus its SSE stream. <Link href="/docs/guides/quickstart">See the quickstart.</Link></> },
        { question: "Can I use this from a browser?", answer: "Yes, through a configured remote runtime. Keep server credentials on your backend and expose only the operations your users are authorized to access. Browsers do not start a local daemon." },
        { question: "Do users bring their own AI accounts?", answer: <>Use an account or API key supported by the selected agent. Authentication methods and capabilities vary by provider, and their usage terms still apply. Mindwire does not bundle model usage. <Link href="/docs/guides/authentication">Read the authentication guide.</Link></> },
        { question: "Are all agents’ capabilities identical?", answer: "No. Mindwire normalizes the shared protocol while keeping native agent details available. Check the runtime catalog for supported capabilities and design your UI around what the selected agent actually exposes." },
        { question: "How do I isolate work for different users?", answer: <>Give separate users or trust boundaries separate environments. A project folder alone is not a sandbox. Use supported Docker or remote targets and make explicit choices about credentials and access. <Link href="/sandboxing">Explore sandboxing.</Link></> },
      ]} />
    </section>
    <div className={styles.pageWrap}><section className={styles.sdkClosing}><div><h2>What could your product do next?</h2><p>Start with one task your users already need to complete.</p></div><Link href="/docs/guides/quickstart" className={base.primaryButton}>Open the quickstart <ArrowUpRight size={17} /></Link></section></div>
  </div>;
}
