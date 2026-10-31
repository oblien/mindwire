import Link from "next/link";
import { ArrowDown, ArrowUpRight, Braces, Check, Command, GitFork, Layers, MoveUpRight } from "lucide-react";
import { productAgents, githubUrl } from "@/lib/product";
import AgentLogo from "../marketing/AgentLogo";
import FrameworkNav from "./FrameworkNav";
import ArchitectureDiagram from "./ArchitectureDiagram";
import AtelierMotion from "./AtelierMotion";
import AtelierPlayground from "./AtelierPlayground";
import RuntimeConnect from "./RuntimeConnect";
import styles from "./atelier-home.module.css";

function PrimaryLink({ children = "Start building", href = "/docs/guides/quickstart" }: { children?: React.ReactNode; href?: string }) {
  return <Link href={href} className={styles.primaryLink}>{children}<span><ArrowUpRight size={19} /></span></Link>;
}

function Hero() {
  return (
    <section className={styles.heroSection} aria-labelledby="hero-title">
      <AtelierMotion className={styles.hero}>
        <div className={styles.heroCopy}>
          <span className={styles.eyebrow}><span className={styles.smallMark}>m</span> INTELLIGENCE, BUILT INTO YOUR PRODUCT.</span>
          <h1 id="hero-title">Your product.<br />Powered by<br /><em>agents.</em></h1>
          <p>The best agents already know how to think, create, and get things done. Make them part of what you’re building.</p>
          <div className={styles.heroActions}><PrimaryLink /><a href="#how-it-works" className={styles.textLink}>See how it works <ArrowDown size={17} /></a></div>
        </div>
        <div className={styles.heroArtwork} aria-hidden="true">
          <svg className={styles.heroOrbit} viewBox="0 0 700 700" fill="none">
            <ellipse cx="350" cy="350" rx="303" ry="240" transform="rotate(-38 350 350)" />
            <ellipse cx="350" cy="350" rx="332" ry="264" transform="rotate(-38 350 350)" />
            <path d="M-40 598C112 605 71 430 218 475S473 692 707 495" className={styles.orbitAccent} />
          </svg>
          <div className={styles.coreShadow} />
          <img className={styles.heroCore} src="/images/atelier/core.webp" alt="" width={1254} height={1254} fetchPriority="high" />
          <div className={styles.heroTag}><span><Command size={16} /> YOUR PRODUCT</span><strong>Meet its new<br />superpower.</strong><span className={styles.tagArrow}><MoveUpRight size={29} /></span></div>
          <span className={styles.artLabel}>A LITTLE MINDWIRE.<br />A LOT MORE POSSIBLE.</span>
        </div>
      </AtelierMotion>
      <div className={styles.heroFoot}>
        <span>Extraordinary agents.<br /><strong>One place to make them yours.</strong></span>
        <div className={styles.providerList}>{productAgents.map(agent => <Link key={agent.id} href={agent.href}><AgentLogo agent={agent.id} size={24} /><span>{agent.name}</span></Link>)}</div>
        <a href={githubUrl} className={styles.openSource}><GitFork size={16} /> Open source <ArrowUpRight size={14} /></a>
      </div>
    </section>
  );
}

function Story() {
  return (
    <section className={styles.story} aria-labelledby="idea-title">
      <div className={styles.storyWords}><span className={styles.eyebrow}>THE IDEA IS SIMPLE</span><h2 id="idea-title">Great agents.<br />Entirely <em>yours.</em></h2></div>
      <div className={styles.storyCopy}><p>You bring the thing only you can build. We bring the agent layer that makes it intelligent.</p><p>Your interface. Your tools. Your knowledge. The world’s best agent harnesses, working under your name.</p><a href="#features">Make it your own <ArrowUpRight size={19} /></a></div>
      <svg className={styles.storyLines} viewBox="0 0 500 300" fill="none" aria-hidden="true">{Array.from({length:12},(_,i)=><path key={i} d={`M${i*17-100} 350C${i*17} 20 ${i*17+170} 460 ${i*17+340} -80`} />)}</svg>
    </section>
  );
}

function Capabilities() {
  return (
    <section className={styles.capabilities} aria-labelledby="capabilities-title">
      <div className={styles.sectionHeading}><span className={styles.eyebrow}>ALREADY CAPABLE. ALREADY YOURS.</span><h2 id="capabilities-title">Give your users<br /><em>more than answers.</em></h2><p>An agent that does the work, right inside the product they already love.</p></div>
      <div className={styles.capabilityGrid}>
        <article className={`${styles.capability} ${styles.createCapability}`}>
          <div className={styles.capabilityVisual} aria-hidden="true"><div className={styles.posterBack} /><div className={styles.poster}><span>THE NEXT<br />GOOD THING.</span><img src="/images/atelier/core.webp" alt="" width={1254} height={1254} loading="lazy" /><small>MADE WITH YOUR IDEAS ↗</small></div><div className={styles.createdTag}><Check size={14} /> Ready to make it yours</div></div>
          <span className={styles.capabilityLabel}><Layers size={16} /> CREATE</span><h3>From a thought<br />to a thing.</h3><p>Pages, tools, documents, and designs. Give an idea somewhere to go.</p><Link href="/use-cases">Explore what you can build <ArrowUpRight size={17} /></Link>
        </article>
        <article className={`${styles.capability} ${styles.contextCapability}`}>
          <div className={styles.capabilityVisual} aria-hidden="true"><img className={styles.knowledgeObject} src="/images/atelier/knowledge.webp" alt="" width={1254} height={1254} loading="lazy" /><span className={styles.fileSlip}>brand-guide.md <ArrowUpRight size={13} /></span></div>
          <span className={styles.capabilityLabel}><Command size={16} /> UNDERSTAND</span><h3>It starts with<br />your world.</h3><p>Your files, your context, your expertise. The details that make the difference.</p><Link href="/docs/guides/prompts-and-memory">Bring your knowledge <ArrowUpRight size={17} /></Link>
        </article>
        <article className={`${styles.capability} ${styles.actCapability}`}>
          <div className={styles.capabilityVisual} aria-hidden="true"><img className={styles.toolsObject} src="/images/atelier/tools.webp" alt="" width={1254} height={1254} loading="lazy" /><span className={styles.actionSlip}><Check size={14} /> Changes saved</span></div>
          <span className={styles.capabilityLabel}><Braces size={16} /> TAKE ACTION</span><h3>Less back-and-forth.<br />More done.</h3><p>Connect your tools so the agent can move work forward, with your rules in place.</p><Link href="/docs/guides/mcp">Connect your tools <ArrowUpRight size={17} /></Link>
        </article>
      </div>
    </section>
  );
}

function Connect() {
  return (
    <section className={styles.connect} id="connect" aria-labelledby="connect-title">
      <div className={styles.connectCopy}><span className={styles.eyebrow}>A HEAD START, IN ONE INSTALL.</span><h2 id="connect-title">You build<br />the <em>next part.</em></h2><p>Start with agents that already work. Use the SDK to bring them into your product, then run it wherever it makes sense for you.</p><div className={styles.connectLinks}><Link href="/sdk">Explore the SDK <ArrowUpRight size={18} /></Link><Link href="/cloud">Run it in Cloud <ArrowUpRight size={18} /></Link></div><img src="/images/atelier/tools.webp" alt="" width={1254} height={1254} loading="lazy" /></div>
      <div className={styles.buildPanel}>
        <div className={styles.codeHeader}><span><Braces size={17} /> A little code. A lot of capability.</span><span>mindwire.ts</span></div>
        <div className={styles.codeBody} aria-label="Mindwire SDK usage example"><span className={styles.codeComment}>// Your product. Their intelligence.</span><pre><code><span>const</span>{" run = "}<span>await</span>{" mw.turn({\n"}<b>{"  chatId"}</b>{": conversation.id,\n"}<b>{"  message"}</b>{": user.message,\n});\n\n"}<span>for await</span>{" ("}<span>const</span>{" event "}<span>of</span>{" run) {\n  sendToYourProduct(event);\n}"}</code></pre></div>
        <RuntimeConnect className={styles.runtimePanel} />
      </div>
    </section>
  );
}

function Closing() {
  return (
    <section className={styles.closing} aria-labelledby="closing-title">
      <img className={styles.closingObject} src="/images/atelier/core.webp" alt="" width={1254} height={1254} loading="lazy" />
      <div className={styles.closingHalo} aria-hidden="true" />
      <div className={styles.closingCopy}><span className={styles.eyebrow}>THE NEXT GREAT PRODUCT IS YOURS.</span><h2 id="closing-title">Give it a<br /><em>mind of its own.</em></h2><p>All the intelligence. All your imagination.</p><div className={styles.heroActions}><PrimaryLink /><a href={githubUrl} className={styles.textLink}>Explore the source <ArrowUpRight size={18} /></a></div></div>
    </section>
  );
}

function Footer() {
  return (
    <footer className={styles.footer}>
      <div className={styles.footerTop}><Link href="/" className={styles.footerBrand}>mindwire<span>Intelligence, made yours.</span></Link><nav aria-label="Footer navigation"><div><span>BUILD</span><Link href="/sdk">SDK & runtime</Link><Link href="/cloud">Cloud</Link><Link href="/docs">Documentation</Link></div><div><span>EXPLORE</span><Link href="/app">Mindwire App</Link><Link href="/use-cases">Examples</Link><Link href="/get-started">Get started</Link></div><div><span>CONNECT</span><a href={githubUrl}>GitHub <ArrowUpRight size={12} /></a><a href={`${githubUrl}/issues`}>Support <ArrowUpRight size={12} /></a><Link href="/changelog">Changelog</Link></div></nav></div>
      <div className={styles.footerWordmark} aria-hidden="true">mindwire<span>↗</span></div>
      <div className={styles.footerBottom}><span>Open source. Open possibilities.</span><a href="https://oblien.com">Made by Oblien <ArrowUpRight size={13} /></a><a href="#top">Back to top <ArrowUpRight size={13} /></a></div>
    </footer>
  );
}

export default function FrameworkHome() {
  return (
    <div className={styles.page} id="top">
      <FrameworkNav className={styles.navigation} announcement={false} />
      <Hero />
      <Story />
      <Capabilities />
      <ArchitectureDiagram />
      <AtelierPlayground />
      <Connect />
      <Closing />
      <Footer />
    </div>
  );
}
