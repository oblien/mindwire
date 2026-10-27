"use client";

import { useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, ChevronDown } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import styles from "./framework-home.module.css";

const agents = [
  {
    id: "codex",
    label: "Codex",
    engine: "codex",
    href: "/docs/reference/agents/codex",
  },
  {
    id: "claude",
    label: "Claude Code",
    engine: "claude-code",
    href: "/docs/reference/agents/claude",
  },
  {
    id: "opencode",
    label: "OpenCode",
    engine: "opencode",
    href: "/docs/reference/agents/opencode",
  },
] as const;

export default function AgentConnect() {
  const [open, setOpen] = useState(false),
    [selected, setSelected] = useState(0);
  const container = useRef<HTMLDivElement>(null),
    trigger = useRef<HTMLButtonElement>(null);
  const id = useId(),
    agent = agents[selected];
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => {
      if (!container.current?.contains(event.target as Node)) setOpen(false);
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        trigger.current?.focus();
      }
    };
    container.current
      ?.querySelector<HTMLButtonElement>("[role=tab][aria-selected=true]")
      ?.focus({ preventScroll: true });
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", key);
    };
  }, [open]);
  return (
    <div ref={container} className={styles.connectControl}>
      <button
        ref={trigger}
        type="button"
        className={styles.connectButton}
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((value) => !value)}
      >
        <span className={styles.agentStack} aria-hidden="true">
          <span>
            <AgentLogo agent="codex" size={18} surface="dark" />
          </span>
          <span>
            <AgentLogo agent="claude" size={18} surface="light" />
          </span>
          <span>
            <AgentLogo agent="opencode" size={18} surface="light" />
          </span>
        </span>
        Connect your agent <ChevronDown size={14} />
      </button>
      {open && (
        <div
          id={id}
          className={styles.agentPopover}
          role="dialog"
          aria-label="Connect your agent"
        >
          <div
            className={styles.connectionTabs}
            role="tablist"
            aria-label="Choose your agent"
            onKeyDown={navigateTabs}
          >
            {agents.map((item, index) => (
              <button
                type="button"
                role="tab"
                id={`${id}-${index}`}
                aria-selected={selected === index}
                aria-controls={`${id}-panel`}
                tabIndex={selected === index ? 0 : -1}
                key={item.id}
                onClick={() => setSelected(index)}
              >
                <AgentLogo
                  agent={item.id}
                  size={16}
                  surface={selected === index ? "dark" : "light"}
                />
                {item.label}
              </button>
            ))}
          </div>
          <div
            role="tabpanel"
            id={`${id}-panel`}
            aria-labelledby={`${id}-${selected}`}
            className={styles.popoverInstructions}
          >
            <div>
              <span className={styles.stepNumber}>1</span>
              <p>
                Install Mindwire and connect your {agent.label} account.
                <br />
                <Link href={agent.href}>
                  Set up {agent.label} <ArrowUpRight size={13} />
                </Link>
              </p>
            </div>
            <div>
              <span className={styles.stepNumber}>2</span>
              <p>
                Choose <code>{agent.engine}</code> in the SDK. Add your tools
                and start a conversation.
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
