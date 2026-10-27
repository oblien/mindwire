"use client";
import { useState } from "react";
import { Check, Cloud, Folder, Laptop, Layers, Terminal } from "lucide-react";
import styles from "./resource-pages.module.css";

const environments = [
  {
    id: "local",
    label: "Your computer",
    icon: Laptop,
    title: "Right where you feel at home.",
    description:
      "Use your Mac or connect a server over SSH. Your agent works directly on the host, with its existing files and tools.",
    detail: "Direct host access",
    computer: "My computer",
    folder: "My projects",
    tone: "peach",
  },
  {
    id: "docker",
    label: "Docker workspace",
    icon: Layers,
    title: "A separate space for a new idea.",
    description:
      "Create a Docker workspace on your host. Give the project its own environment and tools, while keeping it on your infrastructure.",
    detail: "Container workspace",
    computer: "Project workspace",
    folder: "Project files",
    tone: "blue",
  },
  {
    id: "cloud",
    label: "Cloud Computer",
    icon: Cloud,
    title: "More room. Wherever you are.",
    description:
      "Choose a managed computer with a monthly plan. Connect through Mindwire and keep the project on its own machine.",
    detail: "Managed by Oblien",
    computer: "Cloud Computer",
    folder: "Workspace files",
    tone: "mint",
  },
] as const;

export default function SandboxPreview() {
  const [selected, setSelected] = useState(0);
  const current = environments[selected];
  return (
    <section
      id="environments"
      className={styles.environment}
      aria-label="Workspace options"
      data-tone={current.tone}
    >
      <div
        className={styles.environmentTabs}
        role="tablist"
        aria-label="Where your agent works"
      >
        {environments.map((item, index) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            id={`workspace-tab-${item.id}`}
            aria-controls={`workspace-panel-${item.id}`}
            aria-selected={selected === index}
            tabIndex={selected === index ? 0 : -1}
            onClick={() => setSelected(index)}
            onKeyDown={(event) => {
              const next =
                event.key === "ArrowRight"
                  ? (index + 1) % 3
                  : event.key === "ArrowLeft"
                    ? (index + 2) % 3
                    : event.key === "Home"
                      ? 0
                      : event.key === "End"
                        ? 2
                        : null;
              if (next !== null) {
                event.preventDefault();
                setSelected(next);
                document
                  .getElementById(`workspace-tab-${environments[next].id}`)
                  ?.focus();
              }
            }}
          >
            <item.icon size={18} />
            {item.label}
          </button>
        ))}
      </div>
      <div
        role="tabpanel"
        id={`workspace-panel-${current.id}`}
        aria-labelledby={`workspace-tab-${current.id}`}
        tabIndex={0}
        className={styles.environmentBody}
      >
        <div className={styles.environmentCopy}>
          <span>{current.detail}</span>
          <h2>{current.title}</h2>
          <p>{current.description}</p>
        </div>
        <div className={styles.computerScene} aria-hidden="true">
          <svg className={styles.wires} viewBox="0 0 600 340" fill="none">
            <path
              d="M0 85h65q20 0 20 20v38q0 20 20 20h45M600 260h-65q-20 0-20-20v-45q0-20-20-20h-40"
              stroke="currentColor"
              strokeWidth="1.5"
            />
            <circle cx="8" cy="85" r="4" fill="currentColor" />
            <circle cx="591" cy="260" r="4" fill="currentColor" />
          </svg>
          <div className={styles.computer}>
            <div className={styles.computerBar}>
              <current.icon size={17} />
              <span>{current.computer}</span>
              <span className={styles.connected}>
                <Check size={12} />
                Connected
              </span>
            </div>
            <div className={styles.computerBody}>
              <Terminal size={48} strokeWidth={1.2} />
              <div>
                <p>A place to make it happen.</p>
                <span>Your agent · Your tools</span>
              </div>
            </div>
            <div className={styles.computerTools}>
              <span>
                <Folder size={16} />
                {current.folder}
              </span>
              <span>
                <Terminal size={16} />
                Terminal
              </span>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
