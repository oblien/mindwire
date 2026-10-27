"use client";

import { useEffect, useId, useRef, useState } from "react";
import { ArrowRight, Check, ChevronDown, Copy, Download, FileText, Globe, Image as ImageIcon, Layers2, ListTodo, LoaderCircle, Play } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import { campaignCopy, campaignImage, campaignPoster, landingPage, launchTasks, taskCsv } from "./creation-artifacts";
import styles from "./product-demo.module.css";

const agents = [
  { id: "codex", name: "Codex" },
  { id: "claude", name: "Claude Code" },
  { id: "opencode", name: "OpenCode" },
] as const;
const artifacts = [
  { id: "page", label: "Landing page", icon: Globe },
  { id: "campaign", label: "Campaign", icon: ImageIcon },
  { id: "tasks", label: "Launch plan", icon: ListTodo },
] as const;
const steps = ["Read the brief and brand kit", "Build and check the landing page", "Create the campaign assets", "Assign the launch tasks"];
const previewHtml = landingPage(campaignImage);
const previewPoster = campaignPoster(campaignImage);
type Artifact = (typeof artifacts)[number]["id"];
type Agent = (typeof agents)[number];

async function embeddedPhoto() {
  const response = await fetch(campaignImage);
  if (!response.ok) throw new Error("Could not load the campaign artwork.");
  const blob = await response.blob();
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error("Could not prepare the artwork."));
    reader.readAsDataURL(blob);
  });
}

function saveArtifact(content: string, mime: string, name: string) {
  const url = URL.createObjectURL(new Blob([content], { type: mime }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export default function AgentEngine() {
  const id = useId();
  const [agent, setAgent] = useState<Agent>(agents[0]);
  const [artifact, setArtifact] = useState<Artifact>("page");
  const [step, setStep] = useState(steps.length);
  const [runId, setRunId] = useState(0);
  const [completed, setCompleted] = useState<string[]>([]);
  const [showWork, setShowWork] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [notice, setNotice] = useState("");
  const running = step < steps.length;
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  useEffect(() => {
    if (!running) return;
    const timer = window.setTimeout(() => setStep(value => value + 1), 750);
    return () => window.clearTimeout(timer);
  }, [running, step]);

  const run = (nextAgent = agent) => {
    setAgent(nextAgent);
    setArtifact("page");
    setCompleted([]);
    setNotice("");
    setRunId(value => value + 1);
    setStep(window.matchMedia("(prefers-reduced-motion: reduce)").matches ? steps.length : 0);
  };
  const selectArtifact = (next: Artifact) => { setArtifact(next); setNotice(""); };
  const toggleTask = (task: string) => setCompleted(previous => previous.includes(task) ? previous.filter(item => item !== task) : [...previous, task]);
  const download = async () => {
    setExporting(true);
    setNotice("");
    try {
      if (artifact === "tasks") saveArtifact(taskCsv(completed), "text/csv;charset=utf-8", "fieldwork-launch-plan.csv");
      else {
        const photo = await embeddedPhoto();
        if (!mounted.current) return;
        if (artifact === "page") saveArtifact(landingPage(photo), "text/html;charset=utf-8", "fieldwork-landing-page.html");
        else saveArtifact(campaignPoster(photo), "image/svg+xml;charset=utf-8", "fieldwork-campaign.svg");
      }
      if (mounted.current) setNotice(artifact === "tasks" ? "Launch plan downloaded." : artifact === "page" ? "Page downloaded, with its artwork included." : "Campaign poster downloaded, with its artwork included.");
    } catch {
      if (mounted.current) setNotice("The download could not be prepared. Try again.");
    } finally {
      if (mounted.current) setExporting(false);
    }
  };
  const copyCampaign = async () => {
    try { await navigator.clipboard.writeText(campaignCopy); setNotice("Campaign copy copied."); }
    catch { setNotice("Copy is unavailable here. You can select the campaign text instead."); }
  };

  return (
    <div className={styles.demo} id="in-action" role="region" aria-label="An agent creating a complete launch project inside your product">
      <div className={styles.product}>
        <header className={styles.topbar}>
          <span className={styles.productMark}><Layers2 size={21} aria-hidden /></span>
          <strong>Your product</strong><span className={styles.workspaceName}>Creative workspace</span>
          <span className={styles.projectName}>Fieldwork / Launch project</span>
        </header>

        <div className={styles.workspace}>
          <aside className={styles.brief} aria-label="The request and agent work">
            <span className={styles.requestLabel}>Your user asks</span>
            <h3>“Take this brief<br /> all the way to launch.”</h3>
            <p>Build the site, design the campaign, and give the team a plan to make it happen.</p>

            <button type="button" className={styles.workToggle} aria-expanded={showWork} aria-controls={id + "-work"} onClick={() => setShowWork(value => !value)}><span>Behind the build</span><ChevronDown size={14} aria-hidden /></button>
            <div className={styles.execution} id={id + "-work"} data-open={showWork}>
              <details className={styles.source}><summary><FileText size={17} aria-hidden /><span>launch-brief.md<small>With your brand kit</small></span><ChevronDown size={13} aria-hidden /></summary><p>Launch Fieldwork, a creative retreat by the sea. Make a responsive page with a waitlist, a social poster, and an announcement. Use warm stone colors, forest green, and editorial type. Assign design to Maya, the site to Omar, and community work to Leah.</p></details>
              <ol className={styles.steps} aria-label="Agent work">{steps.map((label, index) => <li key={label} data-state={index < step ? "done" : index === step ? "active" : "pending"}>{index < step ? <Check size={14} aria-hidden /> : index === step ? <LoaderCircle size={14} className={styles.spin} aria-hidden /> : <span className={styles.pendingStep} aria-hidden />}<span>{label}</span></li>)}</ol>
              <span className={styles.tools}>Using your files, browser, and task tools</span>
            </div>

            <button type="button" className={styles.runButton} disabled={running || exporting} onClick={() => run()}>{running ? <LoaderCircle size={14} className={styles.spin} aria-hidden /> : <Play size={13} aria-hidden />}{running ? "Creating…" : "Run example"}<ArrowRight size={14} aria-hidden /></button>
            <p className={styles.runStatus} role="status">{running ? steps[step] + "…" : "Three creations. One request."}</p>
          </aside>

          <section className={styles.results} aria-label="Created project">
            <div className={styles.resultToolbar}>
              <div className={styles.artifactTabs} role="tablist" aria-label="Explore the created outputs" onKeyDown={navigateTabs}>{artifacts.map(item => <button type="button" role="tab" key={item.id} id={id + "-" + item.id} aria-controls={id + "-result"} aria-selected={artifact === item.id} tabIndex={artifact === item.id ? 0 : -1} disabled={running || exporting} onClick={() => selectArtifact(item.id)}><item.icon size={14} aria-hidden /><span>{item.label}</span></button>)}</div>
              <button type="button" className={styles.download} aria-label={artifact === "page" ? "Download landing page HTML" : artifact === "campaign" ? "Download campaign poster SVG" : "Download launch plan CSV"} disabled={running || exporting} onClick={download}>{exporting ? <LoaderCircle size={14} className={styles.spin} aria-hidden /> : <Download size={14} aria-hidden />}<span>Download</span></button>
            </div>

            <div className={styles.resultPanel} role="tabpanel" id={id + "-result"} aria-labelledby={id + "-" + artifact} aria-busy={running}>
              {running ? <div className={styles.building}><div className={styles.buildFiles}><Globe size={30} aria-hidden /><ImageIcon size={28} aria-hidden /><ListTodo size={29} aria-hidden /></div><h4>A whole launch, taking shape.</h4><p>{steps[step]}…</p><progress max={steps.length} value={step} aria-label="Example creation progress" /><span>The results will open right here in your product.</span></div> : artifact === "page" ? <iframe key={runId} className={styles.sitePreview} title="Created Fieldwork landing page" srcDoc={previewHtml} sandbox="allow-scripts allow-forms" /> : artifact === "campaign" ? <div className={styles.campaign}>
                <div className={styles.poster} role="img" aria-label="Created Fieldwork campaign poster with coastal photography and the headline Make room for your next idea" dangerouslySetInnerHTML={{ __html: previewPoster }} />
                <div className={styles.campaignText}><span className={styles.outputLabel}>Ready for your channels</span><h4>One idea.<br />A whole campaign.</h4><p className={styles.campaignCopy}>{campaignCopy}</p><button type="button" onClick={copyCampaign}><Copy size={13} aria-hidden />Copy announcement</button><small>Editable SVG poster + launch copy</small></div>
              </div> : <div className={styles.plan}>
                <div className={styles.planHeading}><div><span className={styles.outputLabel}>Created in your task workspace</span><h4>The launch has a plan.</h4></div><span>{completed.length} / {launchTasks.length} done</span></div>
                <div className={styles.taskList}>{launchTasks.map(task => <label key={task.id} className={styles.task} data-done={completed.includes(task.id)}><input type="checkbox" checked={completed.includes(task.id)} onChange={() => toggleTask(task.id)} /><span><strong>{task.title}</strong><small>{task.owner} / {task.due}</small></span><span className={styles.taskState}>{completed.includes(task.id) ? "Done" : "To do"}</span></label>)}</div>
                <p className={styles.planNote}>Owners assigned from your brief. Ready for the team to pick up.</p>
              </div>}
            </div>
            <div className={styles.resultFooter}><span>{running ? "Working inside your product" : artifact === "page" ? "A responsive page. Try the waitlist form." : artifact === "campaign" ? "A matching poster and announcement, ready to use." : "Check off a task or export the plan."}</span><span>{running ? "Creating files…" : "Saved to project"}</span></div>
          </section>
        </div>
      </div>

      <div className={styles.caption}>
        <div className={styles.engine}><span>Built with</span><div className={styles.agentChoices} role="group" aria-label="Choose the example agent">{agents.map(item => <button type="button" key={item.id} aria-label={item.name} aria-pressed={agent.id === item.id} disabled={running || exporting} onClick={() => run(item)}><AgentLogo agent={item.id} size={16} /><span>{item.name}</span></button>)}</div><span>through Mindwire, inside your product.</span></div>
        <span className={styles.disclaimer}>Illustrative build. Working sample outputs.</span>
      </div>
      <p className={styles.exportStatus} role="status">{notice}</p>
    </div>
  );
}
