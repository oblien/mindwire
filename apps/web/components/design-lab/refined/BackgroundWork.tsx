"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  Cloud,
  FileText,
  MessageSquare,
  Play,
  RotateCcw,
} from "lucide-react";
import { MindwireMark } from "./Artwork";
import { fluidJoin, type Anchor, type Edge } from "./fluid-join";
import styles from "./background-work.module.css";

const activity = [
  {
    text: "Reading the latest feedback alongside your product notes and existing issues.",
    detail: "Reading 24 feedback notes",
  },
  {
    text: "Twelve reports describe the same export problem. Checking the backlog for an existing fix.",
    detail: "Comparing reports with the backlog",
  },
  {
    text: "Creating one issue with the evidence, then letting the product team know what needs attention.",
    detail: "Creating an issue in your product",
  },
  {
    text: "One issue created. Twelve reports connected. Your product team has the context to take it from here.",
    detail: "Work completed",
  },
];

function CloudWorkspaceArt() {
  return (
    <svg
      viewBox="0 0 360 200"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.15"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path
        d="M41 150H73M287 150H320M180 24V43"
        strokeDasharray="2 5"
        opacity=".5"
      />
      <circle cx="38" cy="150" r="3" fill="currentColor" stroke="none" />
      <circle cx="323" cy="150" r="3" fill="currentColor" stroke="none" />
      <path
        d="M102 114C74 114 67 79 89 64C91 35 129 23 150 44C168 20 210 24 222 52C247 42 271 62 270 85C292 95 286 125 261 127H112"
        fill="#e2e7d9"
      />
      <path
        d="M110 78C112 60 132 53 147 64M172 47C188 40 205 50 208 64"
        opacity=".4"
      />
      <rect x="95" y="92" width="185" height="87" rx="13" fill="#e2dce9" />
      <rect x="80" y="105" width="185" height="79" rx="13" fill="#f8f7f3" />
      <path d="M80 130H265" opacity=".5" />
      <circle cx="96" cy="117" r="2" fill="currentColor" stroke="none" />
      <circle cx="105" cy="117" r="2" />
      <circle cx="114" cy="117" r="2" />
      <path d="M240 116H250M245 112V120" opacity=".5" />
      <path d="M101 148H153M101 159H181M101 169H141" opacity=".6" />
      <g className={styles.artSignal}>
        <circle cx="232" cy="156" r="13" fill="#e2e7d9" />
        <path d="M226 156L230 160L238 152" />
      </g>
      <path d="M57 57H69M63 51V63M299 109H309M304 104V114" opacity=".4" />
    </svg>
  );
}

export default function BackgroundWork() {
  const board = useRef<HTMLDivElement>(null);
  const [joins, setJoins] = useState<string[]>([]);
  const [step, setStep] = useState(3);
  const [hasReplayed, setHasReplayed] = useState(false);
  const running = step < 3;
  const current = activity[step];

  useEffect(() => {
    if (!running) return;
    const timer = window.setTimeout(() => setStep((value) => value + 1), 1450);
    return () => window.clearTimeout(timer);
  }, [step, running]);

  useLayoutEffect(() => {
    const element = board.current;
    if (!element) return;
    let frame = 0;

    const measure = () => {
      const bounds = element.getBoundingClientRect();
      const anchor = (name: string, edge: Edge, fraction = 0.5): Anchor => {
        const node = element.querySelector<HTMLElement>(
          `[data-work-surface="${name}"]`,
        )!;
        const rect = node.getBoundingClientRect();
        const directions = {
          top: [0, -1],
          right: [1, 0],
          bottom: [0, 1],
          left: [-1, 0],
        };
        const [dx, dy] = directions[edge];
        return {
          x:
            rect.left -
            bounds.left +
            (edge === "left"
              ? 1
              : edge === "right"
                ? rect.width - 1
                : rect.width * fraction),
          y:
            rect.top -
            bounds.top +
            (edge === "top"
              ? 1
              : edge === "bottom"
                ? rect.height - 1
                : rect.height * fraction),
          dx,
          dy,
        };
      };

      if (window.innerWidth <= 640) {
        setJoins([
          fluidJoin(
            anchor("signal", "bottom", 0.4),
            anchor("workspace", "top", 0.6),
            24,
          ),
          fluidJoin(
            anchor("workspace", "bottom", 0.6),
            anchor("result", "top", 0.4),
            24,
          ),
        ]);
      } else if (window.innerWidth <= 1100) {
        setJoins([
          fluidJoin(
            anchor("signal", "right", 0.65),
            anchor("workspace", "left", 0.2),
            25,
          ),
          fluidJoin(
            anchor("workspace", "bottom", 0.6),
            anchor("result", "top", 0.4),
            26,
          ),
        ]);
      } else {
        setJoins([
          fluidJoin(
            anchor("signal", "right", 0.7),
            anchor("workspace", "left", 0.3),
            27,
          ),
          fluidJoin(
            anchor("workspace", "right", 0.75),
            anchor("result", "left", 0.72),
            27,
          ),
        ]);
      }
    };

    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    const observer = new ResizeObserver(schedule);
    observer.observe(element);
    element
      .querySelectorAll<HTMLElement>("[data-work-surface]")
      .forEach((node) => observer.observe(node));
    window.addEventListener("resize", schedule);
    measure();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", schedule);
    };
  }, []);

  function replay() {
    if (running) return;
    setHasReplayed(true);
    setStep(0);
  }

  return (
    <section
      className={styles.section}
      id="automation"
      aria-labelledby="automation-title"
    >
      <header className={styles.heading}>
        <h2 id="automation-title">
          Set it in motion. <br />
          Let it work.
        </h2>
        <div className={styles.intro}>
          <p>
            An event in your app. A schedule. A single request. Give your agent
            a goal and a cloud workspace. It works out the steps and brings the
            finished work back.
          </p>
          <Link href="/docs/guides/cloud-computers">
            Explore cloud work <ArrowUpRight size={17} />
          </Link>
        </div>
      </header>

      <div className={styles.stage} ref={board} data-running={running}>
        <svg
          className={styles.joins}
          width="100%"
          height="100%"
          aria-hidden="true"
        >
          {joins.map((path, index) => (
            <path key={index} d={path} />
          ))}
        </svg>

        <article className={styles.signal} data-work-surface="signal">
          <div className={styles.surfaceLabel}>
            <MessageSquare size={17} /> From your app
          </div>
          <h3>
            New feedback.
            <br />A clear goal.
          </h3>
          <p className={styles.request}>
            “Find the problems worth fixing. Check what’s already tracked, and
            create an issue with the evidence.”
          </p>
          <div className={styles.signalFooter}>
            <button
              type="button"
              onClick={replay}
              aria-disabled={running}
              aria-controls="background-work-result"
            >
              {hasReplayed ? <RotateCcw size={15} /> : <Play size={14} />}
              {running
                ? "Working…"
                : hasReplayed
                  ? "Replay example"
                  : "See it work"}
            </button>
            <span>Example</span>
          </div>
        </article>

        <article className={styles.workspace} data-work-surface="workspace">
          <div className={styles.workspaceTop}>
            <span className={styles.surfaceLabel}>
              <Cloud size={17} /> In the cloud
            </span>
            <span className={styles.mindwireMark}>
              <MindwireMark />
            </span>
          </div>
          <div className={styles.art}>
            <CloudWorkspaceArt />
          </div>
          <div className={styles.workspaceCopy}>
            <h3>An agent, on the job.</h3>
            <div className={styles.workspaceMessages}>
              {activity.map((entry, index) => (
                <p
                  key={entry.detail}
                  data-active={step === index}
                  aria-hidden={step !== index}
                >
                  {entry.text}
                </p>
              ))}
            </div>
          </div>
          <div
            className={styles.resources}
            aria-label="Context and tools available to the agent"
          >
            <span>
              <FileText size={14} /> Your context
            </span>
            <span>
              <ArrowUpRight size={15} /> Your tools
            </span>
          </div>
          <p className={styles.activity} aria-live="polite" aria-atomic="true">
            {running ? (
              <span className={styles.activityDot} aria-hidden="true" />
            ) : (
              <Check size={14} />
            )}
            {current.detail}
          </p>
        </article>

        <article
          className={styles.result}
          data-work-surface="result"
          id="background-work-result"
          aria-label="Example issue returned to your product"
          aria-busy={running}
        >
          <div className={styles.surfaceLabel}>
            <ArrowRight size={17} /> Back in your product
          </div>
          <div className={styles.resultPreview}>
            <div className={styles.issue} data-pending={running}>
              <div className={styles.issueMain}>
                <div className={styles.issueMeta}>
                  <span>PRODUCT / 128</span>
                  <span className={styles.issueStatus}>To do</span>
                </div>
                <h3>
                  Keep filters
                  <br />
                  in CSV exports.
                </h3>
                <p>
                  Filtered views export the entire dataset. The export should
                  match exactly what the customer sees.
                </p>
              </div>
              <div className={styles.issueDetail}>
                <div className={styles.evidence}>
                  <span className={styles.evidenceCount}>12</span>
                  <span>
                    customer reports
                    <br />
                    connected to this issue
                  </span>
                </div>
                <blockquote>
                  “I filtered the list, but the export still includes
                  everything.”
                  <cite>From customer feedback</cite>
                </blockquote>
                <div className={styles.issueOwner}>
                  <span className={styles.teamMark} aria-hidden="true">
                    <span />
                    <span />
                    <span />
                  </span>
                  <span>Product team</span>
                  <span>
                    Notified <Check size={12} />
                  </span>
                </div>
              </div>
            </div>
            {running && (
              <div className={styles.waiting}>
                <FileText size={29} strokeWidth={1.2} />
                <h3>
                  The result is
                  <br />
                  on its way.
                </h3>
                <p>The agent is working through the task in the cloud.</p>
              </div>
            )}
          </div>
          <div className={styles.resultNote}>
            <span className={styles.resultIcon} aria-hidden="true">
              {running ? <FileText size={16} /> : <Check size={16} />}
            </span>
            <p>
              {running
                ? "The agent is working on it."
                : "Useful work. Already in place."}
            </p>
          </div>
        </article>
      </div>
    </section>
  );
}
