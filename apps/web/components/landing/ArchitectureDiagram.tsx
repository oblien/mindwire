import Link from "next/link";
import { ArrowUpRight, Check, FileText, Layers, Plug, Sparkles } from "lucide-react";
import { productAgents } from "@/lib/product";
import AgentLogo from "../marketing/AgentLogo";
import DiagramWires from "./DiagramWires";
import styles from "./atelier-home.module.css";

export default function ArchitectureDiagram() {
  return (
    <section id="how-it-works" className={styles.pipeline} aria-labelledby="architecture-title">
      <div className={styles.pipelineHeading}><div><span className={styles.eyebrow}>EVERYTHING, CONNECTED.</span><h2 id="architecture-title">Your world in.<br /><em>Possibility out.</em></h2></div><p>Mindwire brings the agent, your context, and your product together. Your users ask. Your product delivers.</p></div>
      <DiagramWires className={styles.pipelineBoard}>
        <div className={styles.pipelineSources} data-signal="sources">
          <div data-signal="files" className={`${styles.inputCard} ${styles.knowledgeInput}`}><span><FileText size={15} /> YOUR KNOWLEDGE</span><h3>Everything<br />you know.</h3><p>Files, memory & context</p><img src="/images/atelier/knowledge.webp" alt="" width={1254} height={1254} loading="lazy" /></div>
          <div data-signal="website" className={`${styles.inputCard} ${styles.productInput}`}><span><Layers size={15} /> YOUR PRODUCT</span><h3>Your look.<br />Your experience.</h3><div className={styles.miniProduct} aria-hidden="true"><span>your product <ArrowUpRight size={12} /></span><div><i /><i /><i /></div><small>What should we make?</small></div></div>
          <div data-signal="workspace" className={`${styles.inputCard} ${styles.toolsInput}`}><span><Plug size={15} /> YOUR TOOLS</span><h3>Power to<br />get things done.</h3><p>Actions, services & APIs</p><img src="/images/atelier/tools.webp" alt="" width={1254} height={1254} loading="lazy" /></div>
        </div>
        <div className={styles.pipelineCore}>
          <span className={styles.coreCaption}>THE CONNECTION<br />THAT CHANGES EVERYTHING.</span>
          <div data-signal="hub" className={styles.coreEngine}><div className={styles.coreRings} aria-hidden="true" /><img src="/images/atelier/core.webp" alt="" width={1254} height={1254} loading="lazy" /></div>
          <strong>mindwire</strong><span>One agent layer. All yours.</span>
        </div>
        <div className={styles.pipelineOutputs}>
          <div data-signal="agents" className={styles.agentOutput}><span className={styles.outputEyebrow}><Sparkles size={15} /> THE INTELLIGENCE</span><h3>The best brains.<br />On your side.</h3><div className={styles.agentGrid}>{productAgents.map(agent=><Link key={agent.id} href={agent.href}><span><AgentLogo agent={agent.id} size={27} surface="dark" /></span>{agent.id==='grok'?'Grok':agent.id==='claude'?'Claude':agent.name}</Link>)}</div></div>
          <Link href="/sdk" data-signal="api" className={styles.productOutput}><span className={styles.outputEyebrow}>THE POSSIBILITIES <ArrowUpRight size={18} /></span><h3>Extraordinary work.<br />Inside your product.</h3><div className={styles.resultSheet}><div><span className={styles.resultMark}>f.</span><span>forma<small>Your creative workspace</small></span></div><p>Your launch page is ready.</p><span className={styles.resultImage}><img src="/images/atelier/core.webp" alt="" width={1254} height={1254} loading="lazy" /><span>GOOD<br />THINGS<br /><em>AHEAD.</em></span></span><span className={styles.resultDone}><Check size={14} /> Built around your brand</span></div></Link>
        </div>
      </DiagramWires>
      <div className={styles.pipelineFoot}><span>Your interface. Your users. Your rules.</span><Link href="/docs/concepts/architecture">See what’s under the hood <ArrowUpRight size={17} /></Link></div>
    </section>
  );
}
