"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronDown,
  FileText,
  GitBranch,
  RotateCcw,
} from "@/components/site/InterfaceIcon";
import AgentLogo from "@/components/marketing/AgentLogo";
import styles from "./conversation-handoff.module.css";

export default function ConversationHandoff() {
  const [stage, setStage] = useState(0);
  const [context, setContext] = useState(
    "Checkout implemented in checkout.tsx. The payment checks pass. Review retries and duplicate confirmations against the current project files.",
  );
  const [task, setTask] = useState(
    "Review the checkout changes and report the remaining payment edge cases.",
  );
  const [editing, setEditing] = useState(false);
  const result = useRef<HTMLHeadingElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const contextInput = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (stage !== 1) return;
    const timer = window.setTimeout(() => setStage(2), 1300);
    return () => window.clearTimeout(timer);
  }, [stage]);
  useEffect(() => {
    if (stage === 2) result.current?.focus({ preventScroll: true });
  }, [stage]);
  useEffect(() => {
    if (editing) contextInput.current?.focus({ preventScroll: true });
  }, [editing]);

  return (
    <section
      className={styles.section}
      id="handoffs"
      aria-labelledby="handoff-title"
    >
      <header className={styles.heading}>
        <h2 id="handoff-title">
          Different minds.
          <br />
          One continuous thread.
        </h2>
        <p>
          Start with one agent. Bring in another when the work calls for it.
          <br className={styles.desktopBreak} /> The goal, the progress, and the
          files go with you.
        </p>
      </header>

      <div className={styles.relay} data-stage={stage}>
        <div className={styles.agentRail}>
          <div className={styles.agentLabel}>
            <AgentLogo agent="codex" size={35} surface="light" />
            <div>
              <strong>Codex</strong>
              <span>Build the checkout</span>
            </div>
          </div>
          <div className={styles.railLine} aria-hidden="true">
            <i />
            <span>
              <GitBranch size={18} />
            </span>
            <i />
          </div>
          <div className={styles.agentLabel}>
            <AgentLogo agent="claude" size={35} surface="light" />
            <div>
              <strong>Claude Code</strong>
              <span>A second perspective</span>
            </div>
          </div>
        </div>

        <div className={styles.spread}>
          <div className={styles.source}>
            <span className={styles.chapter}>01 / The first conversation</span>
            <h3>
              The foundation
              <br />
              is in place.
            </h3>
            <p>
              Checkout implemented. Tests passing.
              <br />
              Ready for another pair of eyes.
            </p>
            <div className={styles.file}>
              <span>
                <FileText size={14} /> checkout.tsx <Check size={12} />
              </span>
              <code>
                <span>
                  <i>04</i>
                  <em>const</em> payment = <em>await</em> charge(order);
                </span>
                <span>
                  <i>05</i>
                  <em>await</em> saveConfirmation(payment);
                </span>
                <span>
                  <i>06</i>
                  <em>return</em> payment.receipt;
                </span>
              </code>
            </div>
            <span className={styles.sourceNote}>
              <Check size={13} /> Original conversation stays intact
            </span>
          </div>

          <div className={styles.destination} aria-busy={stage === 1}>
            <span className={styles.chapter}>02 / The linked conversation</span>
            <h3 ref={result} tabIndex={-1}>
              {stage === 2 ? (
                <>
                  One edge case.
                  <br />
                  Caught in review.
                </>
              ) : (
                <>
                  A fresh look.
                  <br />A head start.
                </>
              )}
            </h3>
            <p>
              {stage === 2
                ? "The payment can succeed before the confirmation saves. A retry needs to reuse the original request."
                : "Claude can pick up the project with the context you choose, and focus on what comes next."}
            </p>
            <div className={styles.reviewNote}>
              <span>
                <AgentLogo agent="claude" size={19} surface="light" />{" "}
                {stage === 2 ? "Review note" : "The next task"}
              </span>
              <p>
                {stage === 2
                  ? "Keep the payment’s idempotency key across retries. Add a test for an interrupted confirmation."
                  : task}
              </p>
              <span className={styles.reviewStatus}>
                {stage === 2 ? (
                  <>
                    <Check size={13} /> Context received. Review ready.
                  </>
                ) : stage === 1 ? (
                  "Carrying the context forward…"
                ) : (
                  "Ready when you are"
                )}
              </span>
            </div>
          </div>
        </div>

        <div className={styles.contextBar}>
          <div className={styles.contextSummary}>
            <span className={styles.contextSymbol}>
              <GitBranch size={22} />
            </span>
            <p>
              The thread continues.<span>Goal, progress & file references</span>
            </p>
          </div>
          <button
            type="button"
            className={styles.editContext}
            aria-expanded={editing}
            aria-controls="handoff-context-editor"
            onClick={() => setEditing((value) => !value)}
          >
            Choose the context <ChevronDown size={13} />
          </button>
          <button
            ref={trigger}
            type="button"
            className={styles.pass}
            aria-disabled={stage === 1 || !context.trim() || !task.trim()}
            onClick={() => {
              if (stage === 1 || !context.trim() || !task.trim()) return;
              if (stage === 2) {
                setStage(0);
                return;
              }
              setEditing(false);
              setStage(1);
            }}
          >
            {stage === 2 ? (
              <>
                <RotateCcw size={15} /> Try the handoff again
              </>
            ) : (
              <>
                {stage === 1 ? "Passing the context…" : "Pass it to Claude"}
                <ArrowRight size={16} />
              </>
            )}
          </button>
        </div>
        <div
          id="handoff-context-editor"
          className={styles.contextEditor}
          hidden={!editing}
        >
          <label>
            Context to carry forward
            <textarea
              ref={contextInput}
              value={context}
              maxLength={8192}
              rows={3}
              onChange={(event) => setContext(event.target.value)}
            />
          </label>
          <label>
            The next task
            <textarea
              value={task}
              maxLength={4096}
              rows={3}
              onChange={(event) => setTask(event.target.value)}
            />
          </label>
        </div>
        <span className="sr-only" role="status">
          {stage === 1
            ? "Passing the selected context to a linked conversation."
            : stage === 2
              ? "Handoff complete. The example review is ready."
              : "Interactive handoff example."}
        </span>
      </div>
      <div className={styles.continuity}>
        <Link href="/docs/guides/sessions">
          <RotateCcw size={17} />
          <span>Resume where you left off</span>
          <ArrowUpRight size={14} />
        </Link>
        <Link href="/docs/guides/sessions">
          <GitBranch size={17} />
          <span>Fork a new direction</span>
          <ArrowUpRight size={14} />
        </Link>
        <Link href="/docs/guides/compaction">
          <FileText size={17} />
          <span>Keep long-running work in context</span>
          <ArrowUpRight size={14} />
        </Link>
      </div>
      <Link className={styles.guide} href="/docs/guides/handoffs">
        Explore conversation handoffs <ArrowUpRight size={16} />
      </Link>
    </section>
  );
}
