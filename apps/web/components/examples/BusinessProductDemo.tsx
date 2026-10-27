"use client";

import { useId, useState } from "react";
import type { UseCase } from "@/lib/use-cases";
import SurfaceRoutes from "@/components/landing/SurfaceRoutes";
import CourseDemo from "./CourseDemo";
import styles from "./business-product-demo.module.css";

type ResultProps = { onUpdate: (message: string) => void };

export default function BusinessProductDemo({ item }: { item: UseCase }) {
  const [reply, setReply] = useState(item.reply);
  const [revision, setRevision] = useState(0);
  return (
    <div
      className={styles.demo}
      id={item.kind === "learning" ? "course-results" : "product-demo"}
    >
      <div className={styles.caption}>
        <span>Inside {item.brand}</span>
        <span>Interactive example · Sample data</span>
      </div>
      <SurfaceRoutes kind="businessExample" className={styles.layout}>
        <aside className={styles.conversation} data-sprue="business-brief">
          <div className={styles.chatHeading}>
            <strong>Ask {item.brand}</strong>
            <button
              type="button"
              onClick={() => {
                setReply(item.reply);
                setRevision(revision + 1);
              }}
              aria-label="Reset product example"
            >
              ↺
            </button>
          </div>
          <div className={styles.request}>
            <span>You</span>
            <p>{item.request}</p>
          </div>
          <div className={styles.context}>
            <span>Working with</span>
            <ul>
              {item.context.map((context) => (
                <li key={context}>{context}</li>
              ))}
            </ul>
          </div>
          <div className={styles.reply}>
            <strong>{item.brand}</strong>
            <p aria-live="polite">{reply}</p>
          </div>
          <div className={styles.chatFooter}>
            Your conversation. Your product’s tools.
          </div>
        </aside>
        <div className={styles.product} data-sprue="business-product">
          <div className={styles.productBar}>
            <strong>{item.brand.toLowerCase()}</strong>
            <span>{item.category}</span>
          </div>
          <div key={revision} className={styles.result}>
            {item.kind === "research" && <ResearchResult onUpdate={setReply} />}
            {item.kind === "builder" && <BuilderResult onUpdate={setReply} />}
            {item.kind === "support" && <SupportResult onUpdate={setReply} />}
            {item.kind === "analytics" && <AnalyticsResult />}
            {item.kind === "learning" && <CourseDemo compact />}
            {item.kind === "commerce" && <CommerceResult onUpdate={setReply} />}
          </div>
        </div>
      </SurfaceRoutes>
    </div>
  );
}

const findings = [
  {
    title: "Teams need a clearer first step.",
    count: "3 of 6 interviews",
    quote:
      "I connected the account, then wasn’t sure what I was meant to do next.",
    source: "Interview 02 · Operations lead",
    recommendation:
      "Open new workspaces with a short checklist and one suggested first project.",
  },
  {
    title: "Sample data would make the value easier to see.",
    count: "2 of 6 interviews",
    quote:
      "We had to import our whole spreadsheet before we could tell whether it was useful.",
    source: "Interview 04 · Studio founder",
    recommendation:
      "Offer an editable sample project before asking a team to import its own data.",
  },
  {
    title: "Inviting the team feels like a separate task.",
    count: "2 of 6 interviews",
    quote:
      "I set everything up myself. I didn’t notice the invite option until the next day.",
    source: "Interview 06 · Project manager",
    recommendation:
      "Put team invites next to the first shared task, where collaboration starts.",
  },
];

function ResearchResult({ onUpdate }: ResultProps) {
  const [finding, setFinding] = useState(0);
  const [recommendations, setRecommendations] = useState(false);
  const current = findings[finding];
  return (
    <div className={styles.research}>
      <span className={styles.breadcrumb}>
        Research / Onboarding / Findings
      </span>
      <h3>
        Make the first
        <br />
        ten minutes count.
      </h3>
      <p className={styles.lede}>
        Six customer interviews. Three opportunities to help teams get started.
      </p>
      <div
        className={styles.findings}
        role="group"
        aria-label="Explore research findings"
      >
        {findings.map((entry, index) => (
          <button
            type="button"
            key={entry.title}
            aria-pressed={finding === index}
            onClick={() => setFinding(index)}
          >
            <span>0{index + 1}</span>
            <span>
              {entry.title}
              <small>{entry.count}</small>
            </span>
            <span aria-hidden="true">↗</span>
          </button>
        ))}
      </div>
      <div className={styles.evidence}>
        <span>From the source</span>
        <blockquote>“{current.quote}”</blockquote>
        <p>{current.source}</p>
      </div>
      {recommendations ? (
        <div className={styles.recommendation}>
          <strong>Recommended change</strong>
          <p>{current.recommendation}</p>
        </div>
      ) : (
        <button
          type="button"
          className={styles.textButton}
          onClick={() => {
            setRecommendations(true);
            onUpdate(
              "I added a practical recommendation to each finding. Select a finding to review its source and the proposed change together.",
            );
          }}
        >
          Add a recommendation to each finding <span aria-hidden="true">↗</span>
        </button>
      )}
    </div>
  );
}

function BuilderResult({ onUpdate }: ResultProps) {
  const [view, setView] = useState("Preview");
  const [dark, setDark] = useState(false);
  const [published, setPublished] = useState(false);
  const [itinerary, setItinerary] = useState(false);
  return (
    <div className={styles.builder}>
      <div className={styles.toolbar}>
        <div role="group" aria-label="Website output view">
          {["Preview", "Files"].map((label) => (
            <button
              key={label}
              type="button"
              aria-pressed={view === label}
              onClick={() => setView(label)}
            >
              {label}
            </button>
          ))}
        </div>
        <span>
          {published ? "Published in demo" : "Draft / Ready to review"}
        </span>
      </div>
      {view === "Preview" ? (
        <div className={styles.sitePreview} data-dark={dark}>
          <div className={styles.siteNav}>
            <strong>FIELDWORK</strong>
            <span>A coastal retreat</span>
          </div>
          <div className={styles.siteHero}>
            <div>
              <span>Three days. A different pace.</span>
              <h3>
                Room to
                <br />
                <em>think again.</em>
              </h3>
              <p>
                Step away from the everyday. Make space for good conversations,
                long lunches, and your next idea.
              </p>
              <button type="button" onClick={() => setItinerary(!itinerary)}>
                {itinerary ? "Close itinerary" : "Explore the retreat"}{" "}
                <span aria-hidden="true">↗</span>
              </button>
            </div>
            <img
              src="/images/launch-demo/fieldwork-retreat.webp"
              alt="A stone coastal retreat with a terrace overlooking the sea"
              loading="lazy"
            />
          </div>
          {itinerary && (
            <ol className={styles.itinerary}>
              <li>
                <span>Day one</span>Arrive. Unpack. Meet the table.
              </li>
              <li>
                <span>Day two</span>Walk the coast. Make something.
              </li>
              <li>
                <span>Day three</span>A slow morning. A fresh perspective.
              </li>
            </ol>
          )}
          <div className={styles.siteBottom}>
            <span>Coastal walks</span>
            <span>Shared tables</span>
            <span>Space to create</span>
          </div>
        </div>
      ) : (
        <div className={styles.files}>
          <h3>The working project</h3>
          <ul>
            <li>
              <code>src/page.tsx</code>
              <span>Retreat page & itinerary</span>
            </li>
            <li>
              <code>src/booking.tsx</code>
              <span>Booking section</span>
            </li>
            <li>
              <code>src/styles.css</code>
              <span>Responsive layouts</span>
            </li>
            <li>
              <code>public/retreat.webp</code>
              <span>Your supplied photograph</span>
            </li>
          </ul>
          <p>
            In your product, these files live in the customer’s workspace and
            feed the preview.
          </p>
        </div>
      )}
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.secondaryButton}
          onClick={() => {
            setDark(!dark);
            setView("Preview");
            setPublished(false);
            onUpdate(
              dark
                ? "I switched the site back to the light layout. The updated preview is ready to review."
                : "I gave the site a darker, editorial layout. The content and page structure are unchanged. Take a look at the preview.",
            );
          }}
        >
          {dark ? "Try the light layout" : "Try a darker layout"}
        </button>
        <button
          type="button"
          className={styles.primaryButton}
          disabled={published}
          onClick={() => {
            setPublished(true);
            onUpdate(
              "The reviewed site is now marked published in this demo. In your product, this action calls your publishing tool with the approved version.",
            );
          }}
        >
          {published ? "Published ✓" : "Approve & publish"}
        </button>
      </div>
      <p className={styles.demoNote}>
        Sample preview. Publishing changes this demo only.
      </p>
    </div>
  );
}

function SupportResult({ onUpdate }: ResultProps) {
  const [resolved, setResolved] = useState(false);
  const [reply, setReply] = useState(
    "Hi Alex — your export is ready. The original delivery used your previous email address. I’ve sent it again to the verified address on your account. You can also open it from Exports in your workspace.",
  );
  const id = useId();
  return (
    <div className={styles.support}>
      <div className={styles.caseHeader}>
        <span>Ticket #1048</span>
        <span>{resolved ? "Resolved" : "Needs review"}</span>
      </div>
      <h3>“Where’s my export?”</h3>
      <p className={styles.lede}>Alex · Northstar Studio</p>
      <div className={styles.ticketMessage}>
        I requested the monthly export this morning. It says complete, but
        nothing has arrived in my inbox.
      </div>
      <details className={styles.delivery}>
        <summary>
          View delivery evidence <span aria-hidden="true">↗</span>
        </summary>
        <ol>
          <li>
            <time>09:12</time>
            <span>Export completed successfully.</span>
          </li>
          <li>
            <time>09:13</time>
            <span>
              Delivery failed: previous address no longer accepts email.
            </span>
          </li>
          <li>
            <time>09:18</time>
            <span>Customer verified the updated address.</span>
          </li>
        </ol>
      </details>
      <label className={styles.replyLabel} htmlFor={id}>
        {resolved ? "Reply sent in demo" : "Review the reply"}
      </label>
      <textarea
        id={id}
        value={reply}
        onChange={(event) => setReply(event.target.value)}
        disabled={resolved}
        rows={5}
      />
      <div className={styles.actions}>
        <span>
          {resolved
            ? "Export redelivered · Ticket resolved"
            : "Redeliver export + send this reply"}
        </span>
        <button
          className={styles.primaryButton}
          type="button"
          disabled={resolved || !reply.trim()}
          onClick={() => {
            setResolved(true);
            onUpdate(
              "Approved. The export is marked redelivered and your reviewed reply is attached to the resolved ticket in this demo.",
            );
          }}
        >
          {resolved ? "Resolved ✓" : "Approve & resolve"}
        </button>
      </div>
      <p className={styles.demoNote}>Sample ticket. No email is sent.</p>
    </div>
  );
}

const sales = [
  { month: "January", direct: 18000, partners: 8000, marketplace: 4000 },
  { month: "February", direct: 21000, partners: 9000, marketplace: 5000 },
  { month: "March", direct: 27000, partners: 10000, marketplace: 6000 },
];
const dollars = (value: number) =>
  new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 0,
  }).format(value);

function AnalyticsResult() {
  const [month, setMonth] = useState("Quarter");
  const [source, setSource] = useState(false);
  const rows =
    month === "Quarter" ? sales : sales.filter((row) => row.month === month);
  const channels = [
    { key: "direct", label: "Direct" },
    { key: "partners", label: "Partners" },
    { key: "marketplace", label: "Marketplace" },
  ] as const;
  const totals = channels.map((channel) => ({
    ...channel,
    value: rows.reduce((sum, row) => sum + row[channel.key], 0),
  }));
  const total = totals.reduce((sum, channel) => sum + channel.value, 0);
  const maximum = Math.max(...totals.map((channel) => channel.value));
  return (
    <div className={styles.analytics}>
      <div className={styles.toolbar}>
        <span>Sales / Channel performance</span>
        <label>
          <span className="sr-only">Filter sales by month</span>
          <select
            value={month}
            onChange={(event) => setMonth(event.target.value)}
          >
            {["Quarter", ...sales.map((row) => row.month)].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
      </div>
      <div className={styles.revenue}>
        <span>
          {month === "Quarter" ? "Quarterly revenue" : `${month} revenue`}
        </span>
        <h3>{dollars(total)}</h3>
        <p>{Math.round((totals[0].value / total) * 100)}% from direct sales</p>
      </div>
      <div
        className={styles.chart}
        aria-label={`Revenue by channel for ${month.toLowerCase()}`}
      >
        {totals.map((channel, index) => (
          <div key={channel.key}>
            <span>{channel.label}</span>
            <div>
              <span
                style={{ width: `${(channel.value / maximum) * 100}%` }}
                data-channel={index}
              />
            </div>
            <strong>{dollars(channel.value)}</strong>
          </div>
        ))}
      </div>
      <div className={styles.insight}>
        <strong>Direct sales are driving the growth.</strong>
        <p>
          Monthly revenue rose from $30k in January to $43k in March. Direct
          sales account for $9k of that $13k increase.
        </p>
      </div>
      <button
        className={styles.textButton}
        type="button"
        aria-expanded={source}
        onClick={() => setSource(!source)}
      >
        {source ? "Hide source rows" : "Inspect the source rows"}{" "}
        <span aria-hidden="true">↗</span>
      </button>
      {source && (
        <div className={styles.tableWrap}>
          <table>
            <caption>quarterly-sales.csv / {month}</caption>
            <thead>
              <tr>
                <th>Month</th>
                {channels.map((channel) => (
                  <th key={channel.key}>{channel.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.month}>
                  <th>{row.month}</th>
                  {channels.map((channel) => (
                    <td key={channel.key}>{dollars(row[channel.key])}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const catalogChanges = [
  {
    id: "shirt-title",
    product: "LN-01 · Shirt",
    field: "Title",
    before: "LINEN SHIRT NAT",
    after: "Everyday linen shirt — Natural",
  },
  {
    id: "shirt-description",
    product: "LN-01 · Shirt",
    field: "Description",
    before: "No description",
    after:
      "An easy everyday shirt in breathable linen. A relaxed fit for warm days and unhurried evenings.",
  },
  {
    id: "trouser-title",
    product: "LN-02 · Trousers",
    field: "Title",
    before: "PANT LINEN SAND",
    after: "Relaxed linen trousers — Sand",
  },
  {
    id: "trouser-description",
    product: "LN-02 · Trousers",
    field: "Description",
    before: "No description",
    after:
      "Lightweight linen trousers with room to move. An everyday staple, from the first coffee to the last light.",
  },
  {
    id: "overshirt-title",
    product: "LN-03 · Overshirt",
    field: "Title",
    before: "OVER SHIRT LINEN",
    after: "Linen overshirt — Chalk",
  },
];

function CommerceResult({ onUpdate }: ResultProps) {
  const [selected, setSelected] = useState(
    catalogChanges.map((change) => change.id),
  );
  const [applied, setApplied] = useState<string[]>([]);
  const pending = selected.filter((id) => !applied.includes(id));
  return (
    <div className={styles.commerce}>
      <span className={styles.breadcrumb}>Catalog / Linen collection</span>
      <h3>A little more considered.</h3>
      <p className={styles.lede}>
        Three products. Five proposed edits. You choose what goes live.
      </p>
      <div className={styles.catalogChanges}>
        {catalogChanges.map((change) => (
          <label
            key={change.id}
            className={styles.catalogChange}
            data-applied={applied.includes(change.id)}
          >
            <input
              type="checkbox"
              aria-label={`Apply ${change.product} ${change.field.toLowerCase()}`}
              checked={selected.includes(change.id)}
              disabled={applied.includes(change.id)}
              onChange={() =>
                setSelected((current) =>
                  current.includes(change.id)
                    ? current.filter((id) => id !== change.id)
                    : [...current, change.id],
                )
              }
            />
            <span>
              <small>
                {change.product} / {change.field}
                {applied.includes(change.id) ? " / Updated" : ""}
              </small>
              {!applied.includes(change.id) && <del>{change.before}</del>}
              <span>{change.after}</span>
            </span>
          </label>
        ))}
      </div>
      <div className={styles.actions}>
        <span role="status">
          {applied.length
            ? `${applied.length} edits applied`
            : `${pending.length} edits selected`}
        </span>
        <button
          className={styles.primaryButton}
          type="button"
          disabled={!pending.length}
          onClick={() => {
            const count = applied.length + pending.length;
            setApplied([...applied, ...pending]);
            onUpdate(
              `Applied ${count} approved ${count === 1 ? "edit" : "edits"} to the sample collection. Your product can keep the field-level changes with each record for later review.`,
            );
          }}
        >
          Apply {pending.length} {pending.length === 1 ? "edit" : "edits"}
        </button>
      </div>
      <p className={styles.demoNote}>
        Sample catalog. Changes stay in this demo.
      </p>
    </div>
  );
}
