"use client";

import { useState } from "react";
import { ArrowUpRight, Check, ChevronRight, FileText, Folder, Search, Users } from "lucide-react";
import styles from "./hero-results.module.css";

const clients = [
  { name: "Alex Morgan", company: "Orbit Studio", initials: "AM", next: "Send proposal", followUp: true },
  { name: "Sam Rivera", company: "Forma", initials: "SR", next: "Project underway", followUp: false },
  { name: "Jamie Chen", company: "Northstar", initials: "JC", next: "Schedule check-in", followUp: true },
];

export function ClientTracker() {
  const [query, setQuery] = useState("");
  const [followUps, setFollowUps] = useState(false);
  const [completed, setCompleted] = useState<string[]>([]);
  const remaining = clients.filter(client => client.followUp && !completed.includes(client.name)).length;
  const visible = clients.filter(client =>
    `${client.name} ${client.company}`.toLowerCase().includes(query.toLowerCase()) &&
    (!followUps || client.followUp && !completed.includes(client.name)));

  return <div className={styles.result}>
    <header className={styles.resultHeader}><Users size={20} /><h3>Client tracker</h3><span>From clients.csv</span></header>
    <div className={styles.trackerSummary}><div><strong>{clients.length}</strong><span>Clients</span></div><div><strong>{remaining}</strong><span>To follow up</span></div></div>
    <label className={styles.search}><Search size={15} /><input aria-label="Search example clients" placeholder="Find a client…" value={query} onChange={event => setQuery(event.target.value)} /></label>
    <div className={styles.filters} role="group" aria-label="Filter example clients">
      <button type="button" aria-pressed={!followUps} onClick={() => setFollowUps(false)}>All clients</button>
      <button type="button" aria-pressed={followUps} onClick={() => setFollowUps(true)}>Follow-ups <span>{remaining}</span></button>
    </div>
    <div className={styles.clientList}>
      {visible.length ? visible.map(client => <div className={styles.client} key={client.name}>
        <span className={styles.initials} aria-hidden="true">{client.initials}</span>
        <div><strong>{client.name}</strong><span>{client.company}</span></div>
        {client.followUp ? <button type="button" aria-label={`${completed.includes(client.name) ? "Reopen" : "Complete"} ${client.name} follow-up`}
          title={completed.includes(client.name) ? "Reopen follow-up" : client.next}
          aria-pressed={completed.includes(client.name)}
          onClick={() => setCompleted(previous => previous.includes(client.name) ? previous.filter(name => name !== client.name) : [...previous, client.name])}>
          <Check size={15} />
        </button> : <span className={styles.clientStatus}>Active</span>}
      </div>) : <p className={styles.empty}>{query ? "No clients match your search." : "All follow-ups are done."}</p>}
    </div>
    <p className={styles.resultHint}>Try searching or completing a follow-up.</p>
  </div>;
}

const folders = [
  { name: "Projects", count: 3, files: ["Website brief.pdf", "Brand assets.zip", "Launch checklist.md"] },
  { name: "Invoices", count: 2, files: ["October invoice.pdf", "September invoice.pdf"] },
  { name: "Meeting notes", count: 2, files: ["Monday planning.md", "Client check-in.md"] },
];

export function OrganizedFiles() {
  const [selected, setSelected] = useState(0);
  return <div className={styles.result}>
    <header className={styles.resultHeader}><Folder size={20} /><h3>Workspace, sorted.</h3></header>
    <p className={styles.description}>7 files. Three clear places to find them.</p>
    <div className={styles.folderList} role="group" aria-label="Explore organized example files">
      {folders.map((folder, index) => <button type="button" key={folder.name} aria-pressed={selected === index} onClick={() => setSelected(index)}>
        <Folder size={18} /><strong>{folder.name}</strong><span>{folder.count}</span><ChevronRight size={14} />
      </button>)}
    </div>
    <div className={styles.fileList} aria-label={`${folders[selected].name} files`}>
      {folders[selected].files.map(file => <div key={file}><FileText size={14} /><span>{file}</span></div>)}
    </div>
    <p className={styles.resultHint}>Original files kept in a backup folder.</p>
  </div>;
}

const findings = [
  { label: "Summary", title: "A clearer path to launch.", body: "The product is ready for a small pilot. Finish the onboarding checklist and collect feedback from five customers before the wider launch.", detail: "From: launch-notes.md · customer-feedback.csv" },
  { label: "Next steps", title: "Three things to move forward.", body: "Finish the welcome flow. Invite the five pilot customers. Schedule a feedback review after their first week.", detail: "From: team-meeting.md · launch-notes.md" },
];

export function ProjectReport() {
  const [selected, setSelected] = useState(0);
  return <div className={styles.result}>
    <header className={styles.resultHeader}><FileText size={20} /><h3>Launch brief</h3><ArrowUpRight size={16} /></header>
    <p className={styles.description}>Prepared from your project files.</p>
    <div className={styles.filters} role="group" aria-label="Read the example report">
      {findings.map((finding, index) => <button type="button" key={finding.label} aria-pressed={selected === index} onClick={() => setSelected(index)}>{finding.label}</button>)}
    </div>
    <article className={styles.report}><span>PROJECT NORTHSTAR</span><h4>{findings[selected].title}</h4><p>{findings[selected].body}</p><footer>{findings[selected].detail}</footer></article>
  </div>;
}
