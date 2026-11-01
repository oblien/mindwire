"use client";

import { useState, type CSSProperties } from "react";
import Link from "next/link";
import AgentLogo from "@/components/marketing/AgentLogo";
import {
  ArrowUpRight,
  Brain,
  FileCode2,
  MessageSquare,
  Check,
  ChevronDown,
  FileText,
} from "@/components/site/InterfaceIcon";
import { productAgents, type ProductAgent } from "@/lib/product";
import styles from "./bot-studio.module.css";

const personalities = [
  {
    role: "Researcher",
    name: "Morrow",
    color: "#657a57",
    tint: "#e2e8d7",
    mindset:
      "Stay curious. Follow the evidence. Make complex things clear, and always show your sources.",
  },
  {
    role: "Builder",
    name: "Forma",
    color: "#79628a",
    tint: "#e6ddeb",
    mindset:
      "Think it through, then make it real. Work in small steps, check your changes, and explain the important decisions.",
  },
  {
    role: "Teammate",
    name: "Pace",
    color: "#a47052",
    tint: "#eddfcf",
    mindset:
      "Be practical and thoughtful. Keep the team’s priorities in mind. Suggest the next step and ask before making changes.",
  },
];
const availableTools = ["Search documents", "Update tasks", "Send updates"];
const availableContext = [
  "Product handbook",
  "Project notes",
  "Project memory",
];

export default function BotStudio() {
  const [personality, setPersonality] = useState(0);
  const [name, setName] = useState(personalities[0].name);
  const [mindset, setMindset] = useState(personalities[0].mindset);
  const [core, setCore] = useState<ProductAgent>("codex");
  const [tools, setTools] = useState([...availableTools]);
  const [context, setContext] = useState([...availableContext]);
  const current = personalities[personality];
  const botName = name.trim() || "Your agent";
  const toggle = (items: string[], item: string) =>
    items.includes(item)
      ? items.filter((value) => value !== item)
      : [...items, item];

  function choose(index: number) {
    setPersonality(index);
    setName(personalities[index].name);
    setMindset(personalities[index].mindset);
  }

  return (
    <section
      id="make-it-yours"
      className={styles.section}
      aria-labelledby="studio-title"
      style={
        {
          "--bot-accent": current.color,
          "--bot-tint": current.tint,
        } as CSSProperties
      }
    >
      <header className={styles.heading}>
        <h2 id="studio-title">
          Make it think
          <br />
          your way.
        </h2>
        <p className={styles.description}>
          A researcher. A builder. Your next teammate. Give it a name and a way
          of working. Keep the powerful agent underneath.
        </p>
      </header>
      <div className={styles.studioBody}>
        <div className={styles.setup}>
          <div
            className={styles.personalities}
            role="group"
            aria-label="Choose a starting personality"
          >
            {personalities.map((item, index) => (
              <button
                type="button"
                key={item.role}
                aria-pressed={personality === index}
                onClick={() => choose(index)}
              >
                {item.role}
              </button>
            ))}
          </div>
          <label className={styles.nameField}>
            <span>Name</span>
            <input
              aria-label="Bot name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={24}
            />
          </label>
          <label className={styles.mindsetField}>
            <span>Mindset</span>
            <textarea
              aria-label="Working instructions"
              rows={3}
              maxLength={240}
              value={mindset}
              onChange={(event) => setMindset(event.target.value)}
            />
          </label>
          <details className={styles.knowledge}>
            <summary>
              <span>Knowledge & tools</span>
              <span>{context.length + tools.length} connected</span>
              <ChevronDown size={15} />
            </summary>
            <div className={styles.connections}>
              <fieldset>
                <legend>What it knows</legend>
                {availableContext.map((item) => (
                  <label key={item}>
                    <input
                      type="checkbox"
                      checked={context.includes(item)}
                      onChange={() =>
                        setContext((previous) => toggle(previous, item))
                      }
                    />
                    <span>{item}</span>
                  </label>
                ))}
              </fieldset>
              <fieldset>
                <legend>What it can do</legend>
                {availableTools.map((item) => (
                  <label key={item}>
                    <input
                      type="checkbox"
                      checked={tools.includes(item)}
                      onChange={() =>
                        setTools((previous) => toggle(previous, item))
                      }
                    />
                    <span>{item}</span>
                  </label>
                ))}
              </fieldset>
            </div>
          </details>
          <div className={styles.core}>
            <div className={styles.coreLabel}>
              <span>Agent core</span>
              <strong>
                {productAgents.find((agent) => agent.id === core)?.name}
              </strong>
            </div>
            <div role="group" aria-label="Choose your bot’s agent core">
              {productAgents.map((agent) => (
                <button
                  key={agent.id}
                  type="button"
                  aria-label={agent.name}
                  title={agent.name}
                  aria-pressed={core === agent.id}
                  onClick={() => setCore(agent.id)}
                >
                  <AgentLogo agent={agent.id} size={24} surface="light" />
                </button>
              ))}
            </div>
            <span className="sr-only" aria-live="polite">
              {productAgents.find((agent) => agent.id === core)?.name} selected
              as the agent core.
            </span>
          </div>

          <Link className={styles.guide} href="/docs/guides/prompts-and-memory">
            Make an agent your own <ArrowUpRight size={16} />
          </Link>
        </div>

        <div
          className={styles.preview}
          data-personality={personality}
          aria-label="Example response using the selected personality"
        >
          <div className={styles.brand}>
            <div className={styles.brandName}>
              <span>
                {botName}
                <i>.</i>
              </span>
              <div className={styles.brandSymbol} aria-hidden="true">
                {personality === 0 ? (
                  <Brain size={92} />
                ) : personality === 1 ? (
                  <FileCode2 size={92} />
                ) : (
                  <MessageSquare size={92} />
                )}
              </div>
            </div>
          </div>
          <div className={styles.workspace}>
            <div className={styles.prompt}>
              <span className={styles.user}>Example task</span>
              <p>We’re launching on Friday. Where do we start?</p>
            </div>
            <div className={styles.answer} key={personality}>
              <h3>
                {personality === 0 ? (
                  <>
                    Start with what
                    <br />
                    we don’t know.
                  </>
                ) : personality === 1 ? (
                  <>
                    Make something
                    <br />
                    we can ship.
                  </>
                ) : (
                  <>
                    Give the next step
                    <br />
                    an owner.
                  </>
                )}
              </h3>
              {personality === 0 ? (
                <div className={styles.research}>
                  <p>One question worth answering before launch:</p>
                  <blockquote>
                    Can a new customer get to their first useful result?
                  </blockquote>
                  <span>
                    <FileText size={13} /> Based on your product handbook
                  </span>
                </div>
              ) : personality === 1 ? (
                <ol className={styles.taskList}>
                  <li>
                    <span>
                      <Check size={12} />
                    </span>
                    <span>Define the smallest useful release</span>
                    <small>Ready</small>
                  </li>
                  <li>
                    <span>02</span>
                    <span>Build the first complete journey</span>
                    <small>Next</small>
                  </li>
                  <li>
                    <span>03</span>
                    <span>Test it with a real customer</span>
                    <small>Then</small>
                  </li>
                </ol>
              ) : (
                <div className={styles.teamList}>
                  <div>
                    <span>JD</span>
                    <p>
                      Jordan<small>Confirm the release scope</small>
                    </p>
                    <span>Today</span>
                  </div>
                  <div>
                    <span>AL</span>
                    <p>
                      Alex<small>Walk through the customer journey</small>
                    </p>
                    <span>Thursday</span>
                  </div>
                  <div>
                    <span>SK</span>
                    <p>
                      Sam<small>Bring the launch update together</small>
                    </p>
                    <span>Friday</span>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
