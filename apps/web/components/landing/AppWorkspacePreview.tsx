"use client";

import { useId, useState } from "react";
import { Activity, ArrowRight, Check, FileText, Folder, LayoutGrid, Monitor, MoreHorizontal, RotateCcw, ShieldCheck, Smartphone } from "lucide-react";
import OblienLogo from "../OblienLogo";
import AgentLogo from "../marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import styles from "./app-showcase.module.css";

const views = [
  { id: "overview", name: "Overview", icon: LayoutGrid },
  { id: "files", name: "Files", icon: Folder },
  { id: "activity", name: "Activity", icon: Activity },
];
const files = [
  { name: "customer-brief.md", detail: "The finished brief", content: "Finish onboarding for the pilot group. Review the latest customer feedback. Confirm the follow-ups for next week." },
  { name: "launch-notes.md", detail: "Project context", content: "Launch week: a small pilot, a clear welcome flow, and time set aside to learn from the first customers." },
  { name: "customer-feedback.csv", detail: "Source material", content: "Five customer conversations supplied for this example. Priorities: simpler onboarding, clearer next steps, and a shared project view." },
];

export default function AppWorkspacePreview() {
  const id = useId();
  const [view, setView] = useState("overview");
  const [file, setFile] = useState(0);
  const [approved, setApproved] = useState(false);
  return <div className={styles.scene} aria-label="Interactive Mindwire app workspace preview">
    <div className={styles.desktop}>
      <div className={styles.windowBar}><span className={styles.windowDots} aria-hidden="true"><i /><i /><i /></span><span><OblienLogo size={17} />Mindwire</span><span><Monitor size={12} />My Mac</span></div>
      <div className={styles.workspace}>
        <aside className={styles.sidebar}><span>Workspace</span><strong><Folder size={16} />Launch week</strong><p>A place for your files,<br />context, and next steps.</p><div className={styles.projectFile}><FileText size={13} />launch-notes.md</div><div className={styles.projectFile}><FileText size={13} />customer-feedback.csv</div><div className={styles.agentConnected}><AgentLogo agent="codex" size={18} /><span>Codex<small>Connected on this Mac</small></span></div></aside>
        <div className={styles.workspaceMain}>
          <div className={styles.workspaceHeading}><span>Launch week <ArrowRight size={12} /> Customer brief</span><MoreHorizontal size={17} /></div>
          <div role="tablist" aria-label="Explore the app preview" className={styles.tabs} onKeyDown={navigateTabs}>{views.map(item => <button type="button" role="tab" key={item.id} id={`${id}-${item.id}`} aria-controls={`${id}-content`} aria-selected={view === item.id} tabIndex={view === item.id ? 0 : -1} onClick={() => setView(item.id)}><item.icon size={13} />{item.name}</button>)}</div>
          <div role="tabpanel" id={`${id}-content`} aria-labelledby={`${id}-${view}`} className={styles.workspaceContent}>
            {view === "overview" && <>
              <span className={styles.documentLabel}><FileText size={13} />Customer brief</span><h3>Everything you need.<br />Ready for the next step.</h3><p>Your project notes and customer feedback, brought together in one useful brief.</p>
              <div className={styles.briefRows}><div><Check size={14} /><span>Finish the pilot onboarding</span></div><div><Check size={14} /><span>Review the latest customer feedback</span></div><div><Check size={14} /><span>Confirm next week’s follow-ups</span></div></div>
              <button type="button" className={styles.openFile} onClick={() => { setView("files"); setFile(0); }}><FileText size={16} /><span>customer-brief.md<small>Open the example file</small></span><ArrowRight size={15} /></button>
            </>}
            {view === "files" && <><span className={styles.documentLabel}>Your files</span><div className={styles.fileList}>{files.map((item, index) => <button type="button" key={item.name} aria-pressed={file === index} onClick={() => setFile(index)}><FileText size={16} /><span>{item.name}<small>{item.detail}</small></span><ArrowRight size={13} /></button>)}</div><div className={styles.fileDetail}><h3>{files[file].name}</h3><p>{files[file].content}</p></div></>}
            {view === "activity" && <><span className={styles.documentLabel}>Activity</span><div className={styles.activityList}>{["Read launch-notes.md", "Reviewed the supplied customer feedback", "Saved customer-brief.md", approved ? "You reviewed and approved the next step" : "Waiting for your review"].map((item, index) => <div key={item}><span>{index === 3 && !approved ? <ShieldCheck size={14} /> : <Check size={14} />}</span><p>{item}<small>{index === 3 ? "Your decision, from either device" : "Codex · This workspace"}</small></p></div>)}</div></>}
          </div>
          <div className={styles.workspaceStatus} aria-live="polite">{approved ? <Check size={13} /> : <ShieldCheck size={13} />}{approved ? "Reviewed. Ready for the next step." : "Ready for your review."}<span>Saved in your workspace</span></div>
        </div>
      </div>
    </div>
    <div className={styles.phone}>
      <div className={styles.phoneCamera} aria-hidden="true" />
      <div className={styles.phoneHeader}><OblienLogo size={16} /><span>Mindwire</span><Smartphone size={13} /></div>
      <span className={styles.phoneProject}>Launch week</span><h3>Your work<br />came with you.</h3><p>The same project. The same context. Your next decision.</p>
      <div className={styles.phoneReview}><span>{approved ? <Check size={20} /> : <ShieldCheck size={20} />}</span><strong>{approved ? "You’re all set." : "A quick review."}</strong><p>{approved ? "Your decision is reflected in the workspace." : "The customer brief is ready. Review it before the next step."}</p><button type="button" onClick={() => setApproved(!approved)}>{approved ? <RotateCcw size={12} /> : <Check size={12} />}{approved ? "Reset preview" : "Approve example"}</button></div>
      <div className={styles.phoneHome} aria-hidden="true" />
    </div>
    <div className={styles.sceneCaption}><span>One workspace. Wherever you pick it up.</span><span>Interactive app preview · Sample data</span></div>
  </div>;
}
