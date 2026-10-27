"use client";

import { useId, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, Check } from "lucide-react";
import CopyButton from "./CopyButton";
import { navigateTabs } from "./tabs";
import styles from "./framework-home.module.css";

const languages = [
  {
    name: "TypeScript",
    command: "npm install mindwire",
    description: "Add the SDK to your product’s backend.",
    href: "/docs/guides/quickstart",
  },
  {
    name: "Go",
    command: "go get github.com/oblien/mindwire/packages/daemon",
    description: "Connect from your Go service.",
    href: "/docs/guides/quickstart",
  },
  {
    name: "REST",
    command: "go -C packages/daemon run ./cmd/daemon",
    description: "From your checkout, start the HTTP runtime.",
    href: "/docs/guides/quickstart",
  },
] as const;

export default function RuntimeConnect() {
  const [selected, setSelected] = useState(0);
  const id = useId();
  const current = languages[selected];
  return (
    <div className={styles.connectionPanel} data-sprue="connect-code">
      <div
        className={styles.connectionTabs}
        role="tablist"
        aria-label="Connect to Mindwire"
        onKeyDown={navigateTabs}
      >
        {languages.map((item, index) => (
          <button
            key={item.name}
            type="button"
            role="tab"
            id={id + index}
            aria-controls={id + "-instructions"}
            aria-selected={index === selected}
            tabIndex={index === selected ? 0 : -1}
            onClick={() => setSelected(index)}
          >
            {item.name}
          </button>
        ))}
      </div>
      <div
        role="tabpanel"
        id={id + "-instructions"}
        aria-labelledby={id + selected}
        className={styles.connectionInstructions}
      >
        <div>
          <span className={styles.stepNumber}>1</span>
          <div>
            <p>{current.description}</p>
            <div className={styles.install}>
              <code>{current.command}</code>
              <CopyButton
                value={current.command}
                label="Copy install command"
              />
            </div>
          </div>
        </div>
        <div>
          <span className={styles.stepNumber}>2</span>
          <div>
            <p>Choose an agent. Connect your tools. Start a conversation.</p>
            <Link href={current.href}>
              Follow the quickstart <ArrowUpRight size={14} />
            </Link>
          </div>
        </div>
        <div className={styles.connectionNote}>
          <Check size={15} />
          Run locally, on your server, or in Mindwire Cloud.
        </div>
      </div>
    </div>
  );
}
