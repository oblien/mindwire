"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  ArrowDown,
  ArrowRight,
  ArrowUpRight,
  Check,
  FileText,
  GitBranch,
  RotateCcw,
} from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import WindowShape from "@/components/landing/WindowShape";
import styles from "./conversation-handoff.module.css";

export default function ConversationHandoff() {
  const [stage, setStage] = useState(0);
  const [context, setContext] = useState(
    "Checkout implemented in checkout.tsx. The payment checks pass. Review retries and duplicate confirmations against the current project files.",
  );
  const [task, setTask] = useState(
    "Review the checkout changes and report the remaining payment edge cases.",
  );
  const input = useRef<HTMLTextAreaElement>(null);
  const result = useRef<HTMLHeadingElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (stage === 1) input.current?.focus({ preventScroll: true });
    if (stage === 2) result.current?.focus({ preventScroll: true });
  }, [stage]);

  return (
    <section
      className={styles.section}
      id="handoffs"
      aria-labelledby="handoff-title"
    >
      <WindowShape kind="connect" color="#22291f" />
      <div className={styles.copy}>
        <span>Conversation handoff</span>
        <h2 id="handoff-title">A fresh perspective on the same work.</h2>
        <p>
          Let Codex build. Let Claude review. Move the goal, progress, and file
          references into a linked conversation, with the context you choose to
          carry forward.
        </p>
        <Link href="/docs/guides/handoffs">
          See how handoffs work <ArrowUpRight size={17} />
        </Link>
        <div className={styles.continuity}>
          <p>Keep the work moving.</p>
          <Link href="/docs/guides/sessions">
            Resume a conversation <ArrowUpRight size={14} />
          </Link>
          <Link href="/docs/guides/sessions">
            Fork a new direction <ArrowUpRight size={14} />
          </Link>
          <Link href="/docs/guides/compaction">
            Compact long-running work <ArrowUpRight size={14} />
          </Link>
        </div>
      </div>
      <div className={styles.thread}>
        <div className={styles.threadBar}>
          <GitBranch size={15} />
          <span>Checkout / linked conversations</span>
          <span>Example</span>
        </div>
        <div className={styles.source}>
          <div className={styles.agent}>
            <AgentLogo agent="codex" size={25} surface="light" />
            <strong>Codex</strong>
            <span>Implementation</span>
            <Check size={14} />
          </div>
          <p>
            The checkout is ready. The checks pass. Payment retries and
            duplicate confirmations could use a second look.
          </p>
          <div className={styles.files}>
            <span>
              <FileText size={14} />
              checkout.tsx
            </span>
            <span>
              <FileText size={14} />
              payments.test.ts
            </span>
          </div>
        </div>
        <div className={styles.bridge}>
          <span>
            <ArrowDown size={15} />
          </span>
          <p>
            {stage === 2
              ? "Context carried into a linked conversation"
              : "The goal, the progress, the files"}
          </p>
        </div>
        <div className={styles.destination}>
          <div className={styles.agent}>
            <AgentLogo agent="claude" size={25} surface="light" />
            <strong>Claude Code</strong>
            <span>Review</span>
          </div>
          {stage === 0 ? (
            <div className={styles.ready}>
              <h3>Pick up where the work left off.</h3>
              <p>
                Review the context and set the next task. The original
                conversation stays a click away.
              </p>
              <button ref={trigger} type="button" onClick={() => setStage(1)}>
                Preview the handoff <ArrowRight size={15} />
              </button>
            </div>
          ) : stage === 1 ? (
            <div className={styles.review}>
              <label htmlFor="handoff-context">Context to carry forward</label>
              <textarea
                ref={input}
                id="handoff-context"
                value={context}
                maxLength={8192}
                rows={3}
                onChange={(event) => setContext(event.target.value)}
              />
              <label htmlFor="handoff-task">The next task</label>
              <textarea
                id="handoff-task"
                value={task}
                maxLength={4096}
                rows={2}
                onChange={(event) => setTask(event.target.value)}
              />
              <button
                type="button"
                disabled={!context.trim() || !task.trim()}
                onClick={() => setStage(2)}
              >
                Hand off to Claude <ArrowRight size={15} />
              </button>
            </div>
          ) : (
            <div className={styles.done}>
              <h3 ref={result} tabIndex={-1}>
                Ready for the second look.
              </h3>
              <p>{task}</p>
              <details>
                <summary>Carried context</summary>
                <p>{context}</p>
              </details>
              <button
                type="button"
                onClick={() => {
                  setStage(0);
                  requestAnimationFrame(() =>
                    trigger.current?.focus({ preventScroll: true }),
                  );
                }}
              >
                <RotateCcw size={13} />
                Replay example
              </button>
            </div>
          )}
        </div>
        <div className={styles.threadFoot}>
          <span>Original conversation retained</span>
          <span>Same project & workspace</span>
        </div>
      </div>
    </section>
  );
}
