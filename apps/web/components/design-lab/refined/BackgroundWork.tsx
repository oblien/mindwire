"use client";

import { useEffect, useId, useState } from "react";
import Link from "next/link";
import AgentLogo from "@/components/marketing/AgentLogo";
import {
  ArrowUpRight,
  Bolt,
  Check,
  Clock,
  Cloud,
  FileText,
  GitBranch,
  Play,
  RotateCcw,
  Send,
} from "@/components/site/InterfaceIcon";
import { navigateTabs } from "@/components/landing/tabs";
import styles from "./background-work.module.css";

const examples = [
  {
    name: "On a schedule",
    icon: Clock,
    cue: "Every night, after the team signs off.",
    goal: "Keep the checkout healthy, overnight.",
    trigger: "Nightly maintenance",
    agent: "codex" as const,
    agentName: "Codex",
    stages: [
      "Running the project’s test suite",
      "Investigating the failing checkout test",
      "Preparing a fix for review",
    ],
    title: "Fix duplicate checkout orders.",
    subtitle:
      "The checkout retry was creating duplicate orders. The fix reuses the original request.",
    artifact: "Pull request / #128",
    result: "All checks passed",
    delivered: "Opened in your repository",
  },
  {
    name: "On an event",
    icon: Bolt,
    cue: "A new batch of customer feedback arrives.",
    goal: "Turn customer feedback into a clear next step.",
    trigger: "New feedback received",
    agent: "claude" as const,
    agentName: "Claude Code",
    stages: [
      "Reading the new feedback",
      "Comparing recurring themes with existing issues",
      "Writing the findings and notifying the team",
    ],
    title: "Make the first step unmistakable.",
    subtitle:
      "Customers are getting stuck at the same onboarding step. The evidence is now in one place.",
    artifact: "Research / Onboarding",
    result: "Sources attached",
    delivered: "Added to your team’s workspace",
  },
  {
    name: "On request",
    icon: Send,
    cue: "One request from a user in your product.",
    goal: "Research the options. Bring back a recommendation.",
    trigger: "Research request",
    agent: "codex" as const,
    agentName: "Codex",
    stages: [
      "Gathering source material",
      "Comparing the options against your criteria",
      "Preparing the recommendation",
    ],
    title: "Start smaller. Learn faster.",
    subtitle:
      "Three options compared against your priorities, with a recommendation and the sources behind it.",
    artifact: "Brief / Recommendation",
    result: "Ready to share",
    delivered: "Returned to your product",
  },
] as const;

export default function BackgroundWork() {
  const id = useId();
  const [selected, setSelected] = useState(0);
  const [step, setStep] = useState(3);
  const [replayed, setReplayed] = useState(false);
  const [resultOpen, setResultOpen] = useState(false);
  const current = examples[selected];
  const running = step < 3;

  useEffect(() => {
    if (!running) return;
    const timer = window.setTimeout(() => setStep((value) => value + 1), 1300);
    return () => window.clearTimeout(timer);
  }, [step, running, selected]);

  function select(index: number) {
    setSelected(index);
    setStep(3);
    setReplayed(false);
    setResultOpen(false);
  }

  const times =
    selected === 0
      ? ["02:00", "02:06", "02:14", "02:18"]
      : ["00:00", "00:06", "00:14", "00:18"];

  return (
    <section
      id="automation"
      className={styles.section}
      aria-labelledby="automation-title"
    >
      <header className={styles.heading}>
        <h2 id="automation-title">
          Put the work
          <br />
          on autopilot.
        </h2>
        <div>
          <p>
            A schedule. An event. A request.
            <br />
            Give an agent a goal and the tools to get there. It works in the
            cloud and brings the result back to your product.
          </p>
          <Link href="/docs/guides/resolve">
            Explore background work <ArrowUpRight size={16} />
          </Link>
        </div>
      </header>

      <div className={styles.workbench}>
        <div
          className={styles.triggers}
          role="tablist"
          aria-label="Ways to start background work"
          onKeyDown={navigateTabs}
        >
          {examples.map((example, index) => (
            <button
              type="button"
              key={example.name}
              role="tab"
              id={`${id}-trigger-${index}`}
              aria-controls={`${id}-example`}
              aria-selected={selected === index}
              tabIndex={selected === index ? 0 : -1}
              onClick={() => select(index)}
            >
              <example.icon size={18} />
              <span>{example.name}</span>
            </button>
          ))}
        </div>

        <div
          className={styles.stage}
          role="tabpanel"
          id={`${id}-example`}
          aria-labelledby={`${id}-trigger-${selected}`}
        >
          <div className={styles.brief}>
            <span className={styles.kicker}>You set the intention</span>
            <h3>{current.goal}</h3>
            <p className={styles.cue}>{current.cue}</p>
            <button
              className={styles.run}
              type="button"
              aria-disabled={running}
              onClick={() => {
                if (!running) {
                  setStep(0);
                  setReplayed(true);
                  setResultOpen(false);
                }
              }}
            >
              {replayed ? <RotateCcw size={16} /> : <Play size={16} />}
              {running
                ? "Running the example…"
                : replayed
                  ? "Replay the run"
                  : "See it run"}
            </button>
            <span className={styles.demoNote}>
              An example of work that carries on.
            </span>
          </div>

          <div className={styles.cloudRun} data-running={running}>
            <div className={styles.cloudLabel}>
              <Cloud size={17} />
              <span>Working in the cloud</span>
            </div>
            <div className={styles.dial}>
              <svg
                viewBox="0 0 300 300"
                aria-hidden="true"
                className={styles.dialDrawing}
              >
                <circle
                  cx="150"
                  cy="150"
                  r="137"
                  fill="none"
                  stroke="#c0c8b1"
                  strokeWidth=".8"
                  strokeDasharray="2 7"
                />
                {Array.from({ length: 60 }, (_, index) => {
                  const angle = (index * Math.PI) / 30;
                  const outer = index % 5 === 0 ? 121 : 118;
                  return (
                    <line
                      key={index}
                      x1={150 + Math.sin(angle) * 111}
                      y1={150 - Math.cos(angle) * 111}
                      x2={150 + Math.sin(angle) * outer}
                      y2={150 - Math.cos(angle) * outer}
                      stroke={index % 5 === 0 ? "#84966d" : "#c5cdb9"}
                      strokeWidth={index % 5 === 0 ? 1.4 : 1}
                    />
                  );
                })}
                <circle
                  cx="150"
                  cy="150"
                  r="98"
                  fill="none"
                  stroke="#f4f3ec"
                  strokeWidth="5"
                />
                <circle
                  cx="150"
                  cy="150"
                  r="98"
                  fill="none"
                  stroke="#99ae80"
                  strokeWidth="5"
                  pathLength="100"
                  strokeDasharray={`${((step + 1) / 4) * 100} 100`}
                  strokeLinecap="round"
                  transform="rotate(-90 150 150)"
                  className={styles.dialProgress}
                />
              </svg>
              <div className={styles.dialCenter}>
                <AgentLogo agent={current.agent} size={29} surface="light" />
                <span className={styles.time}>{times[step]}</span>
                <span>
                  {running
                    ? "Agent at work"
                    : selected === 0
                      ? "Ready before morning"
                      : "Work delivered"}
                </span>
              </div>
              <span className={styles.dialSatellite} aria-hidden="true">
                <Check size={16} />
              </span>
            </div>
            <div
              className={styles.activity}
              aria-live="polite"
              aria-atomic="true"
            >
              <span>{String(Math.min(step + 1, 3)).padStart(2, "0")} / 03</span>
              <p>
                {running
                  ? current.stages[step]
                  : "The task is complete. Your app has the result."}
              </p>
            </div>
            <span className={styles.runCaption}>
              {current.agentName} · Your context & tools
            </span>
          </div>

          <article className={styles.result} aria-busy={running}>
            <div className={styles.resultTop}>
              <span>
                {selected === 0 ? (
                  <GitBranch size={16} />
                ) : (
                  <FileText size={16} />
                )}
                {current.artifact}
              </span>
              <span className={styles.stamp}>
                {running ? "In progress" : "Delivered"}
              </span>
            </div>
            <h3>
              {running ? "The agent is taking it from here." : current.title}
            </h3>
            <p>
              {running
                ? "Reading the context, working through the task, and making the changes it needs."
                : current.subtitle}
            </p>
            <div className={styles.artifact} data-running={running}>
              {selected === 0 ? (
                <div className={styles.patch}>
                  <span>checkout / retry.ts</span>
                  <code>
                    <span>− createOrder(newRequest())</span>
                    <span>+ createOrder(originalRequest)</span>
                  </code>
                  <div>
                    <Check size={13} /> Regression checks passing
                  </div>
                </div>
              ) : selected === 1 ? (
                <blockquote>
                  “I finished setup, but wasn’t sure what to do next.”
                  <span>A recurring theme, with the evidence attached.</span>
                </blockquote>
              ) : (
                <div className={styles.comparison}>
                  <div>
                    <span>Focused pilot</span>
                    <i style={{ width: "83%" }} />
                    <Check size={12} />
                  </div>
                  <div>
                    <span>Full rollout</span>
                    <i style={{ width: "52%" }} />
                  </div>
                  <div>
                    <span>Build in-house</span>
                    <i style={{ width: "35%" }} />
                  </div>
                  <span>Compared against your priorities</span>
                </div>
              )}
            </div>
            <div className={styles.resultBottom}>
              <span>
                {running
                  ? "Your product will receive the outcome"
                  : current.delivered}
              </span>
              <button
                type="button"
                aria-disabled={running}
                aria-expanded={resultOpen}
                aria-controls={`${id}-result`}
                aria-label={
                  resultOpen
                    ? "Close the example result"
                    : "View the example result"
                }
                onClick={() => {
                  if (!running) setResultOpen((value) => !value);
                }}
              >
                <ArrowUpRight size={19} />
              </button>
            </div>
            <div
              id={`${id}-result`}
              className={styles.resultDetails}
              hidden={!resultOpen || running}
            >
              <p>
                {selected === 0
                  ? "The agent traced the failure, updated the retry behavior, and opened a pull request. Your team chooses when to merge."
                  : selected === 1
                    ? "The agent grouped the feedback, linked it to existing issues, and added the findings to the team’s workspace."
                    : "The agent gathered sources, compared three approaches, and returned a brief with its recommendation and tradeoffs."}
              </p>
              <span>
                <Check size={12} /> {current.result}
              </span>
            </div>
          </article>
        </div>
        <div className={styles.footer}>
          <span>Your app can close. The work keeps going.</span>
          <span>
            Example run · Your backend connects the trigger and tools.
          </span>
        </div>
      </div>
    </section>
  );
}
