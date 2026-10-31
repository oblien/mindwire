"use client";
import { useEffect, useRef, useState } from "react";
import { ArrowUpRight, Play } from "lucide-react";
import styles from "./studies.module.css";

const sequences = [
  { name: "Knowledge", command: 'mw.turn("Summarize the launch plan")', lines: [["context", "Mounted /workspace/launch"], ["read", "launch-plan.md → 2,140 tokens"], ["reason", "Identified milestones and decisions"], ["output", "Summary ready in your interface"]], result: "Launch plan summarized. Three priorities surfaced." },
  { name: "Creation", command: 'mw.turn("Draft our release notes")', lines: [["context", "Loaded brand instructions"], ["read", "changelog.md → 18 updates"], ["write", "Drafted release-notes.md"], ["output", "Draft ready for your review"]], result: "Release notes drafted in your brand voice." },
  { name: "Tools", command: 'mw.turn("Organize the launch tasks")', lines: [["context", "Connected workspace tools"], ["plan", "Grouped launch tasks by owner"], ["tool", "create_checklist → completed"], ["output", "Workspace updated with 6 tasks"]], result: "Six tasks organized. Your workspace is ready." },
];
export default function TerminalDemo() {
  const [active, setActive] = useState(0);
  const [count, setCount] = useState(4);
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);
  const sequence = sequences[active];
  const running = count < sequence.lines.length;
  const stop = () => { if (timer.current) clearInterval(timer.current); timer.current = null; };
  useEffect(() => () => { if (timer.current) clearInterval(timer.current); }, []);
  const run = () => {
    stop();
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) { setCount(4); return; }
    setCount(0);
    let next = 0;
    timer.current = setInterval(() => { next += 1; setCount(next); if (next >= 4) stop(); }, 450);
  };
  return <div className={styles.terminalDemo}>
    <div className={styles.terminalTop}><span><i/> MINDWIRE / RUNTIME</span><span>LOCAL DEMO</span></div>
    <div className={styles.terminalModes} aria-label="Terminal example">{sequences.map((item, index) => <button key={item.name} aria-pressed={active === index} onClick={() => { stop(); setActive(index); setCount(4); }}>{item.name}<ArrowUpRight size={12}/></button>)}</div>
    <div className={styles.terminalContent}><p className={styles.terminalCommand}><span>❯</span> {sequence.command}</p><div className={styles.terminalLog}>{sequence.lines.slice(0, count).map(([type, text], index) => <p key={`${active}-${index}`}><span>00:0{index}</span><b>{type}</b>{text}</p>)}</div><div className={styles.terminalResult} role="status">{running ? <><span className={styles.blink}>▌</span> Running example…</> : <><span>↳</span> {sequence.result}</>}</div></div>
    <div className={styles.terminalBottom}><span>ILLUSTRATIVE SESSION · NO LIVE REQUESTS</span><button onClick={run} disabled={running}><Play size={12}/>{running ? "Running…" : "Run sequence"}</button></div>
  </div>;
}
