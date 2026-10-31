"use client";

import { useState } from "react";
import { ArrowUpRight, Check, FileText, Layers, Sparkles } from "lucide-react";
import styles from "./studies.module.css";

const examples = [
  { name: "Understand", prompt: "What matters in these launch notes?", source: "launch-notes.md", title: "The important things. All here.", text: "Your team is shipping a new workspace on Friday. Three decisions need your attention.", items: ["Confirm the launch timeline", "Review the onboarding flow", "Share the release with your team"], color: "purple" },
  { name: "Create", prompt: "Turn our ideas into a first draft.", source: "brand-guidelines.pdf", title: "A starting point that sounds like you.", text: "A launch announcement, shaped by your brand and grounded in your product notes.", items: ["Written in your brand voice", "Built from your own knowledge", "Ready for your team to edit"], color: "orange" },
  { name: "Take action", prompt: "Get the workspace ready for launch.", source: "workspace / launch", title: "From a conversation to a done list.", text: "Connected tools turn the plan into work. Your product stays in control of every step.", items: ["Created the launch checklist", "Organized the shared files", "Prepared the team update"], color: "blue" },
];

export default function FluidPlayground() {
  const [active, setActive] = useState(0);
  const example = examples[active];
  return <div className={styles.fluidPlayground}>
    <div className={styles.fluidChoices} aria-label="Choose an example">
      {examples.map((item, index) => <button key={item.name} aria-pressed={active === index} onClick={() => setActive(index)}><span>0{index + 1}</span>{item.name}<ArrowUpRight size={19}/></button>)}
    </div>
    <div className={styles.fluidDemo} data-tone={example.color}>
      <div className={styles.demoOrbit} aria-hidden="true"><i/><i/><i/></div>
      <div className={styles.sourceChip}><FileText size={17}/>{example.source}</div>
      <div className={styles.promptBubble}><span className={styles.promptAvatar}>Y</span>{example.prompt}</div>
      <div className={styles.resultWindow} aria-live="polite">
        <div className={styles.resultChrome}><span><Layers size={15}/> YOUR WORKSPACE</span><span>EXAMPLE</span></div>
        <div className={styles.resultBody}><Sparkles size={23}/><h3>{example.title}</h3><p>{example.text}</p><ul>{example.items.map(item => <li key={item}><Check size={14}/>{item}</li>)}</ul></div>
        <div className={styles.resultFooter}>Powered by Mindwire <span>Made yours ↗</span></div>
      </div>
      <span className={styles.demoCaption}>YOUR CONTEXT → THEIR INTELLIGENCE → SOMETHING NEW</span>
    </div>
  </div>;
}
