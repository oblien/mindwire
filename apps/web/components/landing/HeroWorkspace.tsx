"use client";

import { useState } from "react";
import { ArrowUpRight, BookOpen, Check, CheckCheck, ChevronRight, FileText, FolderOpen, Laptop, Paperclip, SlidersHorizontal } from "lucide-react";
import ServiceLogo from "../marketing/ServiceLogo";
import { ClientTracker, OrganizedFiles, ProjectReport } from "./HeroResults";
import BotAvatar from "./BotAvatar";
import { navigateTabs } from "./tabs";
import styles from "./landing.module.css";

const bots = [
  {
    id: "kit", name: "Kit", role: "Your app builder", short: "Build an app", character: "robot", service: "chatgpt", serviceName: "ChatGPT",
    prompt: "Turn this spreadsheet into a client tracker. Let me search clients and keep track of follow-ups.",
    reply: "Your tracker is ready. I brought in your clients and added search and follow-ups. Give it a try.",
    steps: ["Read clients.csv", "Built and checked the tracker", "Opened the working app"],
    memory: ["You run a small design studio", "You prefer simple, clear interfaces", "Your client list lives in clients.csv"],
    instructions: "Build useful tools from my ideas and files. Check the result, explain what changed, and let me try it.",
    file: "client-tracker", color: "blue",
  },
  {
    id: "cleo", name: "Cleo", role: "Your personal assistant", short: "Organize files", character: "bean", service: "claude", serviceName: "Claude",
    prompt: "Sort this folder into projects, invoices, and meeting notes. Keep a backup of the originals.",
    reply: "Everything has a place now. I grouped seven files into three folders and kept the originals in a backup.",
    steps: ["Checked the files in your folder", "Created a backup of the originals", "Sorted files into three folders"],
    memory: ["Keep a backup before moving files", "Group documents by project", "Use short, recognizable folder names"],
    instructions: "Keep my work organized. Ask when a file’s purpose is unclear, preserve the originals, and explain what moved.",
    file: "organized-workspace", color: "peach",
  },
  {
    id: "sage", name: "Sage", role: "Your research partner", short: "Prepare a report", character: "sprout", service: "claude", serviceName: "Claude",
    prompt: "Read my project notes and customer feedback. Make a launch brief with the next steps and link back to the sources.",
    reply: "Your brief is ready. I pulled out the launch priorities and next steps, with the source files attached to each section.",
    steps: ["Read the notes and customer feedback", "Identified the launch priorities", "Prepared a brief with sources"],
    memory: ["You want a short executive summary", "Always include the source files", "Highlight clear next steps"],
    instructions: "Work from the material I provide. Explain the findings, cite the source files, and flag anything uncertain.",
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
  const [inspector, setInspector] = useState<"result" | "bot">("result");
  const bot = bots[selected];

  return (
    <div className={styles.workspaceScene} id="demo" role="region" aria-label="Try the Mindwire examples">
      <svg className={styles.workspaceWires} viewBox="0 0 1400 540" fill="none" aria-hidden="true"><path d="M0 70h68q22 0 22 22v130q0 22 22 22h65M0 460h55q35 0 35-35v-70q0-24 24-24h65M1400 80h-68q-22 0-22 22v70q0 22-22 22h-60M1400 470h-60q-30 0-30-30v-70q0-22-22-22h-50" /><circle cx="0" cy="70" r="4" /><circle cx="0" cy="460" r="4" /><circle cx="1400" cy="80" r="4" /><circle cx="1400" cy="470" r="4" /></svg>
      <div className={styles.workspace} data-accent={bot.color}>
        <aside className={styles.botSidebar}>
          <div className={styles.spaceLabel}><span className={styles.spaceMonogram}>m.</span>My space</div>
          <span className={styles.sidebarLabel}>YOUR PERSONAL BOTS</span>
          <div role="tablist" aria-label="Explore personal bots" aria-orientation="vertical" onKeyDown={navigateTabs} className={styles.botList}>
            {bots.map((item, index) => <button type="button" role="tab" key={item.id} id={`bot-${item.id}`} aria-selected={selected === index} aria-controls="bot-conversation" tabIndex={selected === index ? 0 : -1} onClick={() => { setSelected(index); setInspector("result"); }}><BotAvatar character={item.character} size={43} /><span><strong>{item.name}</strong><small>{item.short}</small></span><ChevronRight size={13} /></button>)}
          </div>
          <div className={styles.sidebarNote}><span>Your instructions.<br />Your files. Your results.</span><div className={styles.botMiniatures}><BotAvatar size={26} /><BotAvatar character="robot" size={26} /><BotAvatar character="sprout" size={26} /></div></div>
          <div className={styles.computerLink}><Laptop size={18} /><span>Your computer<small>Where your bots do the work</small></span></div>
        </aside>
        <div className={styles.conversation} id="bot-conversation" role="tabpanel" aria-labelledby={`bot-${bot.id}`}>
          <div className={styles.conversationHeader}><span>{bot.name}<small>{bot.role}</small></span><span className={styles.exampleLabel}>Example</span></div>
          <div key={bot.id} className={styles.chatBody}>
            <div className={styles.userBubble}>{bot.prompt}</div>
            <div className={styles.botResponse}><BotAvatar character={bot.character} size={34} /><span>{bot.name}<small>Made for you</small></span></div>
            <p className={styles.reply}>{bot.reply}</p>
            <div className={styles.completedSteps}>{bot.steps.map(step => <div key={step}><Check size={14} /><span>{step}</span></div>)}</div>
            <button type="button" className={styles.resultLink} onClick={() => { setInspector("result"); document.getElementById("bot-inspector-panel")?.scrollIntoView({ block: "nearest", behavior: "instant" }); document.getElementById("inspector-result")?.focus({ preventScroll: true }); }}><span><FileText size={18} />{bot.file}</span><ArrowUpRight size={16} /></button>
          </div>
          <div className={styles.conversationBottom}><span><ServiceLogo service={bot.service} size={16} />Your {bot.serviceName} account</span><button type="button" aria-label={`About ${bot.name}: instructions and memory`} onClick={() => setInspector(inspector === "bot" ? "result" : "bot")} aria-pressed={inspector === "bot"}><SlidersHorizontal size={17} /></button></div>
        </div>
        <div className={styles.inspector}>
          <div className={styles.inspectorTabs} role="tablist" aria-label="Bot preview details" onKeyDown={navigateTabs}>
            {(["result", "bot"] as const).map(item => <button type="button" key={item} role="tab" id={`inspector-${item}`} aria-selected={inspector === item} aria-controls="bot-inspector-panel" tabIndex={inspector === item ? 0 : -1} onClick={() => setInspector(item)}>{item === "result" ? "The result" : "Behind the bot"}</button>)}
          </div>
          <div id="bot-inspector-panel" role="tabpanel" aria-labelledby={`inspector-${inspector}`} className={styles.inspectorBody} key={`${bot.id}-${inspector}`}>
            {inspector === "result" ? selected === 0 ? <ClientTracker /> : selected === 1 ? <OrganizedFiles /> : <ProjectReport /> : <div className={styles.botProfile}>
              <div className={styles.profileIdentity}><BotAvatar character={bot.character} size={62} /><div><h3>{bot.name}</h3><span>{bot.role}</span></div></div>
              <h4><SlidersHorizontal size={14} />Instructions</h4><p>{bot.instructions}</p>
              <h4><CheckCheck size={15} />Personal memory</h4><ul>{bot.memory.map(memory => <li key={memory}>{memory}</li>)}</ul>
              <div className={styles.knowledgeLink}><FolderOpen size={16} /><span>Your knowledge & files</span><Paperclip size={14} /></div>
            </div>}
          </div>
        </div>
      </div>
    </div>
  );
}
