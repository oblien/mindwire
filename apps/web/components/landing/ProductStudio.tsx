"use client";

import { useEffect, useId, useState, type CSSProperties } from "react";
import Link from "next/link";
import { ArrowRight, ArrowUpRight, BookOpen, Brain, Check, FileText, Palette, Target, Wrench } from "lucide-react";
import { navigateTabs } from "./tabs";
import styles from "./product-studio.module.css";

const tabs = [
  { name: "Brand", detail: "Make it look like you", icon: Palette },
  { name: "Tools", detail: "Give it real capabilities", icon: Wrench },
  { name: "Mindset", detail: "Shape how it works", icon: Brain },
  { name: "Context", detail: "Bring what you know", icon: BookOpen },
  { name: "Goals", detail: "Define what done means", icon: Target },
] as const;
const palettes = [
  { name: "Terracotta", color: "#b86642" },
  { name: "Forest", color: "#427766" },
  { name: "Blue", color: "#4c72aa" },
];
const toolOptions = ["Search documents", "Update tasks", "Save a report"];
const contextOptions = ["Product handbook.md", "Project preferences", "Project memory"];
const references = {
  Brand: { href: "/sdk", label: "Build under your brand" },
  Tools: { href: "/docs/guides/mcp", label: "Connect your tools" },
  Mindset: { href: "/docs/guides/prompts-and-memory", label: "Give it instructions" },
  Context: { href: "/docs/guides/prompts-and-memory", label: "Bring your context" },
  Goals: { href: "/docs/guides/quickstart", label: "Start with a goal" },
};
type Tab = (typeof tabs)[number]["name"];

export default function ProductStudio() {
  const id = useId();
  const [tab, setTab] = useState<Tab>("Brand");
  const [name, setName] = useState("Morrow");
  const [palette, setPalette] = useState(0);
  const [tools, setTools] = useState([...toolOptions]);
  const [context, setContext] = useState([...contextOptions]);
  const [mindset, setMindset] = useState("Be thoughtful and concise. Work from our project notes. Ask before changing a task.");
  const [goal, setGoal] = useState("Turn this week’s project notes into a release plan with clear owners and next steps.");
  const [preview, setPreview] = useState(false);
  const [compact, setCompact] = useState(false);
  useEffect(() => {
    const media = window.matchMedia("(max-width: 760px)");
    const update = () => setCompact(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  const toggle = (list: string[], value: string) => list.includes(value) ? list.filter(item => item !== value) : [...list, value];
  const brand = name.trim() || "Your product";
  const colors = palettes[palette];
  const reference = references[tab];

  return (
    <section className={styles.section} id="make-it-yours" aria-labelledby="studio-title">
      <div className={styles.intro}>
        <h2 id="studio-title">White-label it.<br /> <em>Make it yours.</em></h2>
        <p>Keep the agent’s capabilities. Customize its tools, knowledge, mindset, and goals. Build the whole experience around your product and your users.</p>
        <div className={styles.tabs} role="tablist" aria-label="Customize the example integration" aria-orientation={compact ? "horizontal" : "vertical"} onKeyDown={navigateTabs}>
          {tabs.map(item => (
            <button type="button" key={item.name} role="tab" id={`${id}-${item.name}`} aria-controls={`${id}-settings`} aria-labelledby={`${id}-${item.name}-label`} aria-selected={tab === item.name} tabIndex={tab === item.name ? 0 : -1} onClick={() => setTab(item.name)}>
              <item.icon size={18} aria-hidden /><span><strong id={`${id}-${item.name}-label`}>{item.name}</strong><small>{item.detail}</small></span><ArrowRight size={15} aria-hidden />
            </button>
          ))}
        </div>
      </div>

      <div className={styles.playground} style={{ "--brand": colors.color } as CSSProperties}>
        <div className={styles.stage}>
            <div className={styles.previewCaption}><span>Your brand, powered by a native agent</span><span>Live preview</span></div>
          <div className={styles.product}>
            <header className={styles.productHeader}><span className={styles.brandMark}>{brand.charAt(0).toLowerCase()}.</span><strong>{brand}</strong><span>Project workspace</span></header>
            <div className={styles.productBody}>
              <h3>{preview ? "A clear brief for the work." : "What can we move forward?"}</h3>
              <div className={styles.composer} data-highlight={tab === "Goals"}>
                <span>{preview ? "Your task" : "Start with a goal"}</span>
                {preview ? <p className={styles.taskSummary}>{goal.trim() || "Give the task a goal."}</p> : <textarea aria-label="Task in the product preview" value={goal} maxLength={240} rows={2} onChange={event => setGoal(event.target.value)} />}
                <div className={styles.composerFooter}><span><BookOpen size={13} aria-hidden />{context.length} context sources</span><button type="button" onClick={() => setPreview(value => !value)}>{preview ? "Edit task" : "Preview task"}<ArrowUpRight size={14} aria-hidden /></button></div>
              </div>
              <div className={styles.workingStyle} data-highlight={tab === "Mindset"}><Brain size={15} aria-hidden /><p>{mindset.trim() || "Add your working instructions."}</p></div>
              <div className={styles.capabilities} data-highlight={tab === "Tools"}>
                <span><Wrench size={13} aria-hidden />{tools.length} tools available</span>
                <div>{tools.length ? tools.map(tool => <span key={tool}><Check size={12} aria-hidden />{tool}</span>) : <small>Choose the tools this task can use.</small>}</div>
              </div>
            </div>
          </div>
          <div className={styles.contextShelf} data-highlight={tab === "Context"}>
            <span><BookOpen size={14} aria-hidden />Knowledge it can work with</span>
            <div>{context.length ? context.map(item => <span key={item}><FileText size={15} aria-hidden />{item}</span>) : <small>Add context to ground the work.</small>}</div>
          </div>
        </div>

        <div className={styles.settings} role="tabpanel" id={`${id}-settings`} aria-labelledby={`${id}-${tab}`}>
          {tab === "Brand" && <div className={styles.brandControls}>
            <label className={styles.field}>Product name<input value={name} maxLength={24} onChange={event => setName(event.target.value)} /></label>
            <fieldset className={styles.palette}><legend>Accent color</legend>{palettes.map((item, index) => <button type="button" key={item.name} aria-label={`${item.name} palette`} aria-pressed={palette === index} style={{ background: item.color }} onClick={() => setPalette(index)}>{palette === index && <Check size={16} aria-hidden />}</button>)}</fieldset>
          </div>}
          {tab === "Tools" && <fieldset className={styles.options}><legend>Give it access to your product’s tools.</legend>{toolOptions.map(tool => <label key={tool}><input type="checkbox" checked={tools.includes(tool)} onChange={() => setTools(previous => toggle(previous, tool))} /><span>{tool}</span></label>)}</fieldset>}
          {tab === "Mindset" && <label className={styles.field}>Working instructions<textarea rows={2} maxLength={240} value={mindset} onChange={event => setMindset(event.target.value)} /></label>}
          {tab === "Context" && <fieldset className={styles.options}><legend>Choose the knowledge behind the work.</legend>{contextOptions.map(item => <label key={item}><input type="checkbox" checked={context.includes(item)} onChange={() => setContext(previous => toggle(previous, item))} /><span>{item}</span></label>)}</fieldset>}
          {tab === "Goals" && <label className={styles.field}>The goal for this task<textarea rows={2} maxLength={240} value={goal} onChange={event => setGoal(event.target.value)} /></label>}
          <div className={styles.settingsFooter}><span>Try a change. See it in your product.</span><Link href={reference.href}>{reference.label}<ArrowUpRight size={13} aria-hidden /></Link></div>
        </div>
      </div>
      <p className={styles.footnote}>A white-label experience built on the SDK. These controls update a local example; supported tools and context vary by agent.</p>
    </section>
  );
}
