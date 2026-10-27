"use client";

import { useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, BookOpen, Check, Clock3, Database, FileText, GitBranch, LoaderCircle, Play, Radio, RotateCcw, ShieldCheck, type LucideIcon } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import styles from "./automation-flow.module.css";

const triggers = [
  { label: "On demand", icon: Play, title: "A user asks", event: "Prepare a customer brief", detail: "A request in your product starts the workflow." },
  { label: "On a schedule", icon: Clock3, title: "Monday morning", event: "Every Monday, 09:00 UTC", detail: "A configured schedule starts work on a host you keep online." },
  { label: "From your app", icon: Radio, title: "Customer created", event: "customer.created", detail: "Your backend starts a task when a new customer arrives." },
];
const agents = [{ id: "claude", name: "Claude Code" }, { id: "codex", name: "Codex" }, { id: "opencode", name: "OpenCode" }] as const;
type NodeId = "trigger" | "context" | "tools" | "agent" | "decision" | "review" | "result";
type Phase = "idle" | "trigger" | "context" | "agent" | "decision" | "review" | "save" | "done";
type Connection = { id: string; from: NodeId; out: string; to: NodeId; input: string; label?: string };
type Wire = Connection & { path: string; labelX: number; labelY: number };
const connections: Connection[] = [
  { id: "start", from: "trigger", out: "out", to: "agent", input: "in" },
  { id: "knowledge", from: "context", out: "out", to: "agent", input: "context" },
  { id: "lookup", from: "tools", out: "out", to: "agent", input: "tools" },
  { id: "route", from: "agent", out: "out", to: "decision", input: "in" },
  { id: "approve", from: "decision", out: "yes", to: "review", input: "in", label: "Yes" },
  { id: "direct", from: "decision", out: "no", to: "result", input: "in", label: "No" },
  { id: "finish", from: "review", out: "out", to: "result", input: "review" },
];
const nodeNames: Record<NodeId, string> = { trigger: "trigger", context: "context", tools: "customer tool", agent: "agent task", decision: "review condition", review: "human review", result: "result" };

function useWires() {
  const ref = useRef<HTMLDivElement>(null);
  const [wires, setWires] = useState<Wire[]>([]);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    let frame = 0;
    let mounted = true;
    const draw = () => {
      if (!mounted) return;
      const bounds = canvas.getBoundingClientRect();
      const compact = window.matchMedia("(max-width: 700px)").matches;
      const point = (node: NodeId, port: string) => {
        const element = canvas.querySelector<HTMLElement>(`[data-node="${node}"] [data-port="${port}"]`)!;
        const box = element.getBoundingClientRect();
        const direction = getComputedStyle(element).getPropertyValue("--direction").trim();
        const normal = ({ left: [-1, 0], right: [1, 0], top: [0, -1], bottom: [0, 1] } as Record<string, number[]>)[direction] ?? [1, 0];
        return { x: box.left + box.width / 2 - bounds.left, y: box.top + box.height / 2 - bounds.top, normal };
      };
      setWires(connections.map(edge => {
        const start = point(edge.from, edge.out);
        const end = point(edge.to, edge.input);
        const horizontal = start.normal[0] !== 0 && end.normal[0] !== 0;
        const vertical = start.normal[1] !== 0 && end.normal[1] !== 0;
        const separation = horizontal ? Math.abs(end.x - start.x) : vertical ? Math.abs(end.y - start.y) : (Math.abs(end.x - start.x) + Math.abs(end.y - start.y)) / 2;
        const bend = Math.max(12, Math.min(80, separation / 2));
        const c1 = { x: start.x + start.normal[0] * bend, y: start.y + start.normal[1] * bend };
        const c2 = { x: end.x + end.normal[0] * bend, y: end.y + end.normal[1] * bend };
        if (compact && edge.id === "direct") c1.x = c2.x = bounds.width - 10;
        return { ...edge, path: `M ${start.x} ${start.y} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${end.x} ${end.y}`, labelX: (start.x + 3 * c1.x + 3 * c2.x + end.x) / 8, labelY: (start.y + 3 * c1.y + 3 * c2.y + end.y) / 8 - 9 };
      }));
    };
    const schedule = () => { if (!mounted) return; cancelAnimationFrame(frame); frame = requestAnimationFrame(draw); };
    const observer = new ResizeObserver(schedule);
    observer.observe(canvas);
    canvas.querySelectorAll('[data-node]').forEach(node => observer.observe(node));
    document.fonts.ready.then(schedule);
    schedule();
    return () => { mounted = false; observer.disconnect(); cancelAnimationFrame(frame); };
  }, []);
  return { ref, wires };
}

export default function AutomationFlow() {
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  const { ref, wires } = useWires();
  const [trigger, setTrigger] = useState(2);
  const [agent, setAgent] = useState<(typeof agents)[number]>(agents[0]);
  const [selected, setSelected] = useState<NodeId>("agent");
  const [requiresReview, setRequiresReview] = useState(true);
  const [phase, setPhase] = useState<Phase>("idle");
  const running = !["idle", "review", "done"].includes(phase);
  const waiting = phase === "review";
  const busy = running || waiting;

  useEffect(() => {
    const next: Partial<Record<Phase, Phase>> = { trigger: "context", context: "agent", agent: "decision", decision: requiresReview ? "review" : "save", save: "done" };
    if (!next[phase]) return;
    const timer = window.setTimeout(() => setPhase(next[phase]!), phase === "agent" ? 1000 : 550);
    return () => window.clearTimeout(timer);
  }, [phase, requiresReview]);

  const start = () => setPhase(window.matchMedia("(prefers-reduced-motion: reduce)").matches ? requiresReview ? "review" : "done" : "trigger");
  const approve = () => setPhase(window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "done" : "save");
  const order = ["trigger", "context", "agent", "decision", "review", "save", "done"];
  const position = order.indexOf(phase);
  const complete = new Set<NodeId>();
  if (position > 0) complete.add("trigger");
  if (position > 1) { complete.add("context"); complete.add("tools"); }
  if (position > 2) complete.add("agent");
  if (position > 3) complete.add("decision");
  if (position > 4 && requiresReview) complete.add("review");
  if (phase === "done") complete.add("result");
  const active = new Set<NodeId>(phase === "context" ? ["context", "tools"] : phase === "save" ? ["result"] : ["idle", "done"].includes(phase) ? [] : [phase as NodeId]);
  const status = phase === "idle" ? "Choose a node to explore the flow."
    : phase === "review" ? "The brief is ready. Waiting for your approval."
      : phase === "done" ? requiresReview ? "Approved. The result is ready for your app." : "Saved. The result is ready for your app."
        : ({ trigger: "Starting from your trigger…", context: "Reading context and looking up the customer…", agent: `${agent.name} is preparing the brief…`, decision: "Checking whether review is needed…", save: "Saving the brief to your workspace…" } as Partial<Record<Phase, string>>)[phase];

  const nodes: Record<NodeId, { kind: string; title: string; detail: string; icon: LucideIcon; ports: string[] }> = {
    trigger: { kind: triggers[trigger].label, title: triggers[trigger].title, detail: triggers[trigger].event, icon: triggers[trigger].icon, ports: ["out"] },
    context: { kind: "Your context", title: "Project knowledge", detail: "Notes & preferences", icon: BookOpen, ports: ["out"] },
    tools: { kind: "Your tool", title: "Find customer", detail: "Your product’s API", icon: Database, ports: ["out"] },
    agent: { kind: agent.name, title: "Prepare a brief", detail: "Reason, use tools, create the result", icon: FileText, ports: ["in", "out", "context", "tools"] },
    decision: { kind: "Condition", title: "Needs a review?", detail: requiresReview ? "Ask before saving" : "Save directly", icon: GitBranch, ports: ["in", "yes", "no"] },
    review: { kind: "Human approval", title: "Review the draft", detail: waiting ? "Waiting for you" : requiresReview ? "You have the final say" : "Skipped in this flow", icon: ShieldCheck, ports: ["in", "out"] },
    result: { kind: "Back in your product", title: "Save to workspace", detail: phase === "done" ? "customer-brief.md is ready" : "A brief with clear next steps", icon: FileText, ports: ["in", "review"] },
  };
  const selectedNode = nodes[selected];

  return (
    <section className={styles.section} id="automation" aria-labelledby="automation-title">
      <div className={styles.intro}>
        <div><h2 id="automation-title">Put your product to work.</h2><p>The same agent can carry a workflow from trigger to result. Connect your tools, supply the context, and choose where people review the work.</p></div>
        <Link href="/docs/guides/quickstart">Build your own flow<ArrowUpRight size={16} aria-hidden /></Link>
      </div>
      <div className={styles.editor}>
        <div className={styles.toolbar}>
          <div className={styles.flowName}><GitBranch size={20} aria-hidden /><span><strong>Customer onboarding</strong><small>Your app + Mindwire</small></span></div>
          <div className={styles.triggers} role="tablist" aria-label="Ways to start agent work" onKeyDown={navigateTabs}>
            {triggers.map((item, index) => <button type="button" role="tab" key={item.label} id={`${id}-trigger-${index}`} aria-selected={trigger === index} aria-controls={`${id}-flow`} disabled={busy} tabIndex={trigger === index ? 0 : -1} onClick={() => { setTrigger(index); setPhase("idle"); }}><item.icon size={13} aria-hidden />{item.label}</button>)}
          </div>
          <div className={styles.runActions}><button type="button" className={styles.reset} aria-label="Reset workflow example" title="Reset" onClick={() => setPhase("idle")}><RotateCcw size={15} aria-hidden /></button><button type="button" className={styles.run} disabled={running} onClick={waiting ? approve : start}>{running ? <LoaderCircle className={styles.spin} size={14} aria-hidden /> : waiting ? <ShieldCheck size={14} aria-hidden /> : <Play size={13} aria-hidden />}{running ? "Running…" : waiting ? "Approve example" : phase === "done" ? "Run again" : "Run preview"}</button></div>
        </div>

        <div role="tabpanel" id={`${id}-flow`} aria-labelledby={`${id}-trigger-${trigger}`}>
          <div className={styles.canvas} ref={ref} role="group" aria-label="Interactive customer onboarding workflow" aria-describedby={`${id}-description`}>
            <svg className={styles.wires} aria-hidden="true">
              {wires.map(edge => {
                const skipped = requiresReview ? edge.id === "direct" : edge.id === "approve" || edge.id === "finish";
                const state = skipped ? "muted" : active.has(edge.from) || active.has(edge.to) ? "active" : complete.has(edge.from) && complete.has(edge.to) ? "complete" : "idle";
                return <g key={edge.id} data-edge={edge.id} data-state={state}><path d={edge.path} />{edge.label && <text x={edge.labelX} y={edge.labelY} textAnchor="middle">{edge.label}</text>}</g>;
              })}
            </svg>
            {(Object.keys(nodes) as NodeId[]).map(key => {
              const node = nodes[key];
              const skipped = key === "review" && !requiresReview;
              return <button type="button" key={key} data-node={key} data-active={active.has(key)} data-complete={complete.has(key)} data-skipped={skipped} aria-label={`Inspect ${nodeNames[key]}`} aria-pressed={selected === key} className={`${styles.node} ${styles[key]}`} onClick={() => setSelected(key)}>
                {node.ports.map(port => <span key={port} data-port={port} className={styles.port} aria-hidden />)}
                <span className={styles.nodeTop}><span className={styles.nodeIcon}>{key === "agent" ? <AgentLogo agent={agent.id} size={25} surface="light" /> : <node.icon size={20} aria-hidden />}</span><span><span className={styles.nodeKind}>{node.kind}</span><strong>{node.title}</strong></span></span>
                <span className={styles.nodeDetail}>{node.detail}</span>
                {(active.has(key) || complete.has(key)) && <span className={styles.nodeProgress} aria-hidden>{complete.has(key) ? <Check size={13} /> : waiting && key === "review" ? <Clock3 size={13} /> : <LoaderCircle className={styles.spin} size={13} />}</span>}
              </button>;
            })}
          </div>
        </div>

        <div className={styles.inspector} aria-label="Selected workflow node">
          <div className={styles.inspectorDescription}><span className={styles.inspectorIcon}>{selected === "agent" ? <AgentLogo agent={agent.id} size={21} /> : <selectedNode.icon size={20} aria-hidden />}</span><div><strong>{selectedNode.title}</strong><p>{selected === "trigger" ? triggers[trigger].detail : selected === "agent" ? "Choose the brain behind this task. Keep the rest of your workflow." : selected === "context" ? "Ground the task in files and knowledge from your product." : selected === "tools" ? "The agent calls your tool to get the customer record." : selected === "decision" ? "Choose whether the result needs a person’s approval." : selected === "review" ? "The workflow waits here until someone approves the result." : "Return the result to the interface your users already know."}</p></div></div>
          <div className={styles.nodeSettings}>
            {selected === "agent" && <div className={styles.agents} role="group" aria-label="Choose the workflow agent">{agents.map(item => <button type="button" key={item.id} aria-pressed={agent.id === item.id} disabled={busy} onClick={() => { setAgent(item); setPhase("idle"); }}><AgentLogo agent={item.id} size={16} />{item.name}</button>)}</div>}
            {selected === "decision" && <label className={styles.reviewToggle}><input type="checkbox" checked={requiresReview} disabled={busy} onChange={event => { setRequiresReview(event.target.checked); setPhase("idle"); }} /><span>Require approval</span></label>}
            {selected === "trigger" && <code>{triggers[trigger].event}</code>}
            {selected === "context" && <span><FileText size={14} aria-hidden />project-notes.md</span>}
            {selected === "tools" && <code>find_customer(customerId)</code>}
            {selected === "review" && <span><ShieldCheck size={15} aria-hidden />{requiresReview ? waiting ? "Ready for your approval" : "Approval required" : "Approval skipped"}</span>}
            {selected === "result" && <span><FileText size={15} aria-hidden />customer-brief.md</span>}
          </div>
        </div>
        <div className={styles.statusBar}><span role="status">{phase === "done" && <Check size={13} aria-hidden />}{status}</span><span>Interactive example with sample data</span></div>
      </div>
      <p className={styles.note} id={`${id}-description`}>Your app owns the workflow. Mindwire runs the agent work. Context and tools feed the task; its result {requiresReview ? "waits for approval before returning to your workspace." : "returns directly to your workspace."}</p>
    </section>
  );
}
