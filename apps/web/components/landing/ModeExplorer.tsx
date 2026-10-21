"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowRight, ArrowUpRight, Check, Code2, FileCode2, GitBranch, Laptop, MessageCircle, Smartphone, Terminal } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import ServiceLogo from "../marketing/ServiceLogo";
import BotAvatar from "./BotAvatar";
import { ReadingCorner } from "./HeroWorkspace";
import { navigateTabs } from "./tabs";
import styles from "./landing.module.css";

const copy = {
  general: { label: "General mode", title: <>Less managing.<br />More doing.</>, description: "Talk through an idea, hand over a folder, or give your bot a task. Get back useful things: a plan, a document, a working app.", details: ["A simple conversation", "Bots with memory and purpose", "Your files and finished work, together"], link: "Meet your personal assistant" },
  developer: { label: "Developer mode", title: <>Stay in the flow.<br />Get into the details.</>, description: "The same agents, with your whole workspace in reach. Move between projects, inspect code changes, and work in the terminal.", details: ["Projects and workspaces", "Git changes and terminal access", "Progress, tool output, and approvals"], link: "Explore the developer tools" },
};

function DeveloperPreview() {
  const [tab, setTab] = useState("changes");
  return <div className={styles.developerPreview}>
    <div className={styles.projectBar}><span><FolderIcon />reading-corner</span><span><GitBranch size={13} />feat/weekly-goal</span></div>
    <div className={styles.devTabs} role="tablist" aria-label="Example developer tools" onKeyDown={navigateTabs}>{["changes", "terminal"].map(item => <button type="button" role="tab" aria-selected={tab === item} tabIndex={tab === item ? 0 : -1} aria-controls="developer-output" id={`developer-${item}`} onClick={() => setTab(item)} key={item}>{item === "changes" ? <FileCode2 size={14} /> : <Terminal size={14} />}{item === "changes" ? "Changes" : "Terminal"}{item === "changes" && <small>2</small>}</button>)}</div>
    <div id="developer-output" role="tabpanel" aria-labelledby={`developer-${tab}`} className={styles.devOutput}>
      {tab === "changes" ? <><div className={styles.diffFile}><FileCode2 size={14} />src/components/WeeklyGoal.tsx<span>+12</span></div><pre className={styles.codeDiff}><code><span><i>1</i>export function WeeklyGoal() {"{"}</span><span data-added><i>2</i>+ const target = 100;</span><span data-added><i>3</i>+ const pages = useReadingProgress();</span><span><i>4</i></span><span><i>5</i>  return (</span><span data-added><i>6</i>+   &lt;GoalCard</span><span data-added><i>7</i>+     current={"{pages}"}</span><span data-added><i>8</i>+     target={"{target}"}</span><span data-added><i>9</i>+   /&gt;</span><span><i>10</i>  );</span><span><i>11</i>{"}"}</span></code></pre><div className={styles.checksPassed}><Check size={15} />All checks passed<span>2 files changed</span></div></> : <div className={styles.terminalOutput}><span><b>~/reading-corner</b> feat/weekly-goal</span><code>$ bun run check</code><p>Checking TypeScript…<br /><b>✓ No type errors</b></p><code>$ bun test</code><p>weekly-goal.test.ts<br /><b>✓ shows progress toward your goal<br />✓ handles a completed reading goal</b></p><span>2 passed · 0 failed</span><code>$ <i className={styles.terminalCursor} /></code></div>}
    </div>
  </div>;
}

function FolderIcon() {
  return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10H3Z" strokeLinejoin="round" /></svg>;
}

export default function ModeExplorer() {
  const [mode, setMode] = useState<"general" | "developer">("general");
  const current = copy[mode];
  return <section className={styles.modesSection} id="modes" aria-labelledby="modes-title">
    <div className={styles.modeIntroduction} id="features"><span>ONE MINDWIRE. TWO WAYS TO WORK.</span><h2 id="modes-title">Your everyday.<br className={styles.mobileBreak} /> Your deep work.</h2><p>Choose the space that fits you. Switch modes whenever you need.</p></div>
    <div className={styles.modeLayout}>
      <div className={styles.modeCopy}>
        <div className={styles.modeTabs} role="tablist" aria-label="Explore Mindwire modes" onKeyDown={navigateTabs}>
          {(["general", "developer"] as const).map(item => <button type="button" key={item} role="tab" id={`mode-${item}`} aria-selected={mode === item} aria-controls="mode-details" tabIndex={mode === item ? 0 : -1} onClick={() => setMode(item)}>{item === "general" ? <MessageCircle size={18} /> : <Code2 size={19} />}{item === "general" ? "General" : "Developer"}</button>)}
        </div>
        <div id="mode-details" role="tabpanel" aria-labelledby={`mode-${mode}`} className={styles.modeDetails} key={mode}>
          <h3>{current.title}</h3><p>{current.description}</p><ul>{current.details.map(detail => <li key={detail}><Check size={16} />{detail}</li>)}</ul>
          <Link href={mode === "general" ? "/get-started" : "/developers"} className={styles.modeLink}>{current.link}<ArrowUpRight size={17} /></Link>
        </div>
      </div>
      <div className={styles.modePreview} data-mode={mode} aria-label={`${current.label} example`}>
        <div className={styles.modeWindowHeader}><div className={styles.windowDots} aria-hidden="true"><i /><i /><i /></div><span>Mindwire <ArrowRight size={12} /> {current.label}</span><span className={styles.previewComputer}><Laptop size={14} /> This Mac</span></div>
        <div className={styles.modeWindowBody}>
          <div className={styles.modeChatHeader}><BotAvatar character="robot" size={39} /><span>Kit<small>Your app builder</small></span>{mode === "developer" ? <AgentLogo agent="codex" size={21} /> : <ServiceLogo service="chatgpt" size={21} />}</div>
          <div className={styles.modePrompt}>Add a weekly reading goal. Keep it simple.</div>
          <div className={styles.generalResult} hidden={mode !== "general"}><p>Done. A little encouragement for your next chapter.</p><ReadingCorner compact /></div>
          <div hidden={mode !== "developer"}><DeveloperPreview /></div>
        </div>
        <div className={styles.modePreviewFooter}><Check size={14} /><span>{mode === "general" ? "Your idea, made real." : "Your workspace, in full view."}</span><small>Illustrative example</small></div>
      </div>
    </div>
    <div className={styles.modeBridge}><div><BotAvatar size={28} /><BotAvatar character="robot" size={28} /><BotAvatar character="sprout" size={28} /></div><p>Same bots. Same memory. Pick up where you left off.</p><span><Laptop size={19} /><i /><Smartphone size={17} /></span></div>
  </section>;
}
