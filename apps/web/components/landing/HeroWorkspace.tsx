"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowUpRight, BookOpen, Check, CheckCheck, ChevronRight, Code2, FileText, Laptop, SlidersHorizontal } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import { ClientTracker, OrganizedFiles, ProjectReport } from "./HeroResults";
import BotAvatar from "./BotAvatar";
import { navigateTabs } from "./tabs";
import styles from "./landing.module.css";

const examples = [
  {
    id: "builder", name: "App builder", role: "From a file to a useful tool", short: "Build from a spreadsheet", character: "robot", agent: "codex", agentName: "Codex",
    prompt: "Turn this spreadsheet into a client tracker. Let me search clients and keep track of follow-ups.",
    reply: "Your tracker is ready. I brought in your clients and added search and follow-ups. Give it a try.",
    steps: ["Read clients.csv", "Built and checked the tracker", "Opened the working app"],
    context: ["The user’s uploaded clients.csv", "A workspace for the generated app", "Your product’s interface guidelines"],
    instructions: "Build a useful tool from the supplied files. Check the result and explain what changed. Your product presents the finished app.",
    file: "client-tracker", color: "blue",
  },
  {
    id: "files", name: "File workspace", role: "A little order, built into your product", short: "Organize a folder", character: "bean", agent: "claude", agentName: "Claude Code",
    prompt: "Sort this folder into projects, invoices, and meeting notes. Keep a backup of the originals.",
    reply: "Everything has a place now. I grouped seven files into three folders and kept the originals in a backup.",
    steps: ["Checked the files in your folder", "Created a backup of the originals", "Sorted files into three folders"],
    context: ["The folder selected by the user", "Your app’s access and backup rules", "The requested project structure"],
    instructions: "Organize the supplied files, preserve the originals, and explain what moved. Ask the user when a file’s purpose is unclear.",
    file: "organized-workspace", color: "peach",
  },
  {
    id: "research", name: "Research tools", role: "Turn project notes into next steps", short: "Prepare a report", character: "sprout", agent: "claude", agentName: "Claude Code",
    prompt: "Read my project notes and customer feedback. Make a launch brief with the next steps and link back to the sources.",
    reply: "Your brief is ready. I pulled out the launch priorities and next steps, with the source files attached to each section.",
    steps: ["Read the notes and customer feedback", "Identified the launch priorities", "Prepared a brief with sources"],
    context: ["Project notes and customer feedback", "The report format your app needs", "Source files for the user to review"],
    instructions: "Work from the supplied material. Explain the findings, cite the source files, and flag anything uncertain. Save the brief for your app to display.",
    file: "launch-brief.md", color: "mint",
  },
] as const;

export function ReadingCorner({ compact = false }: { compact?: boolean }) {
  const [shelf, setShelf] = useState<"reading" | "finished">("reading");
  return (
    <div className={`${styles.readingApp} ${compact ? styles.readingCompact : ""}`}>
      <div className={styles.readingTitle}><BookOpen size={20} /><span>Little reading corner</span></div>
      <p>A few pages. A little perspective.</p>
      <div className={styles.bookTabs} role="tablist" aria-label="Example reading shelves" onKeyDown={navigateTabs}>
        {(["reading", "finished"] as const).map(item => <button type="button" key={item} role="tab" aria-selected={shelf === item} tabIndex={shelf === item ? 0 : -1} aria-controls={compact ? "mode-reading-shelf" : "hero-reading-shelf"} id={`${compact ? "mode" : "hero"}-shelf-${item}`} onClick={() => setShelf(item)}>{item === "reading" ? "Reading now" : "Finished"}</button>)}
      </div>
      <div role="tabpanel" id={compact ? "mode-reading-shelf" : "hero-reading-shelf"} aria-labelledby={`${compact ? "mode" : "hero"}-shelf-${shelf}`} className={styles.bookShelf}>
        <div className={styles.bookCover} data-cover={shelf === "reading" ? "orange" : "green"}><small>{shelf === "reading" ? "A FIELD GUIDE" : "COLLECTED NOTES"}</small><strong>{shelf === "reading" ? <>The art<br />of noticing.</> : <>A quieter<br />kind of life.</>}</strong><i /><span>{shelf === "reading" ? "ESSAYS & OBSERVATIONS" : "ON THE EVERYDAY"}</span></div>
        <div className={styles.bookDetails}><span>{shelf === "reading" ? "ON YOUR NIGHTSTAND" : "BACK ON THE SHELF"}</span><h4>{shelf === "reading" ? "The art of noticing" : "A quieter kind of life"}</h4><p>{shelf === "reading" ? "Page 84 of 240" : "Finished on Sunday"}</p><div className={styles.bookProgress}><i style={{ width: shelf === "reading" ? "35%" : "100%" }} /></div><small>{shelf === "reading" ? "One chapter at a time." : "A little more perspective."}</small></div>
      </div>
      <div className={styles.readingGoal}><span>This week</span><strong>{shelf === "reading" ? "64" : "100"}<small> / 100 pages</small></strong><div className={styles.weekDots}>{["M", "T", "W", "T", "F", "S", "S"].map((day, index) => <span key={index} data-complete={index < (shelf === "reading" ? 4 : 7)}>{day}</span>)}</div></div>
    </div>
  );
}

export default function HeroWorkspace() {
  const [selected, setSelected] = useState(0);
  const [inspector, setInspector] = useState<"result" | "context">("result");
  const example = examples[selected];

  return (
    <div className={styles.workspaceScene} id="demo" role="region" aria-label="Try the product examples">
      <svg className={styles.workspaceWires} viewBox="0 0 1400 540" fill="none" aria-hidden="true"><path d="M0 70h68q22 0 22 22v130q0 22 22 22h65M0 460h55q35 0 35-35v-70q0-24 24-24h65M1400 80h-68q-22 0-22 22v70q0 22-22 22h-60M1400 470h-60q-30 0-30-30v-70q0-22-22-22h-50" /><circle cx="0" cy="70" r="4" /><circle cx="0" cy="460" r="4" /><circle cx="1400" cy="80" r="4" /><circle cx="1400" cy="470" r="4" /></svg>
      <div className={styles.workspace} data-accent={example.color}>
        <aside className={styles.botSidebar}>
          <div className={styles.spaceLabel}><span className={styles.spaceMonogram}>m.</span>Your product</div>
          <span className={styles.sidebarLabel}>A FEW POSSIBILITIES</span>
          <div role="tablist" aria-label="Explore product examples" aria-orientation="vertical" onKeyDown={navigateTabs} className={styles.botList}>
            {examples.map((item, index) => <button type="button" role="tab" key={item.id} id={`example-${item.id}`} aria-selected={selected === index} aria-controls="example-conversation" tabIndex={selected === index ? 0 : -1} onClick={() => { setSelected(index); setInspector("result"); }}><BotAvatar character={item.character} size={43} /><span><strong>{item.name}</strong><small>{item.short}</small></span><ChevronRight size={13} /></button>)}
          </div>
          <div className={styles.sidebarNote}><span>Your interface.<br />Your context. Your users.</span><div className={styles.botMiniatures}><BotAvatar size={26} /><BotAvatar character="robot" size={26} /><BotAvatar character="sprout" size={26} /></div></div>
          <div className={styles.computerLink}><Laptop size={18} /><span>Mindwire runtime<small>Local, on your server, or in Cloud</small></span></div>
        </aside>
        <div className={styles.conversation} id="example-conversation" role="tabpanel" aria-labelledby={`example-${example.id}`}>
          <div className={styles.conversationHeader}><span>{example.name}<small>{example.role}</small></span><span className={styles.exampleLabel}>Example</span></div>
          <div key={example.id} className={styles.chatBody}>
            <div className={styles.userBubble}>{example.prompt}</div>
            <div className={styles.botResponse}><BotAvatar character={example.character} size={34} /><span>Made with Mindwire<small>In your product</small></span></div>
            <p className={styles.reply}>{example.reply}</p>
            <div className={styles.completedSteps}>{example.steps.map(step => <div key={step}><Check size={14} /><span>{step}</span></div>)}</div>
            <button type="button" className={styles.resultLink} onClick={() => { setInspector("result"); document.getElementById("example-inspector-panel")?.scrollIntoView({ block: "nearest", behavior: "instant" }); document.getElementById("inspector-result")?.focus({ preventScroll: true }); }}><span><FileText size={18} />{example.file}</span><ArrowUpRight size={16} /></button>
          </div>
          <div className={styles.conversationBottom}><span><AgentLogo agent={example.agent} size={16} />{example.agentName} · Through Mindwire</span><button type="button" aria-label={`How ${example.name.toLowerCase()} works: instructions and context`} onClick={() => setInspector(inspector === "context" ? "result" : "context")} aria-pressed={inspector === "context"}><SlidersHorizontal size={17} /></button></div>
        </div>
        <div className={styles.inspector}>
          <div className={styles.inspectorTabs} role="tablist" aria-label="Product example details" onKeyDown={navigateTabs}>
            {(["result", "context"] as const).map(item => <button type="button" key={item} role="tab" id={`inspector-${item}`} aria-selected={inspector === item} aria-controls="example-inspector-panel" tabIndex={inspector === item ? 0 : -1} onClick={() => setInspector(item)}>{item === "result" ? "The result" : "How it works"}</button>)}
          </div>
          <div id="example-inspector-panel" role="tabpanel" aria-labelledby={`inspector-${inspector}`} className={styles.inspectorBody} key={`${example.id}-${inspector}`}>
            {inspector === "result" ? selected === 0 ? <ClientTracker /> : selected === 1 ? <OrganizedFiles /> : <ProjectReport /> : <div className={styles.botProfile}>
              <div className={styles.profileIdentity}><BotAvatar character={example.character} size={62} /><div><h3>{example.name}</h3><span>Powered by the Mindwire runtime</span></div></div>
              <h4><SlidersHorizontal size={14} />Your instructions</h4><p>{example.instructions}</p>
              <h4><CheckCheck size={15} />Context from your product</h4><ul>{example.context.map(item => <li key={item}>{item}</li>)}</ul>
              <Link href="/sdk" className={styles.knowledgeLink}><Code2 size={16} /><span>Build it with the Mindwire SDK</span><ArrowUpRight size={14} /></Link>
            </div>}
          </div>
        </div>
      </div>
    </div>
  );
}
