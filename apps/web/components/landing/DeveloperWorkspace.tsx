"use client";

import { useState } from "react";
import {
  ArrowRight,
  ArrowUp,
  BookOpen,
  Check,
  ChevronDown,
  FileCode2,
  Folder,
  GitBranch,
  Globe,
  Play,
  Plus,
  Terminal,
} from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import styles from "./developers.module.css";

const agents = [
  { id: "codex", name: "Codex" },
  { id: "claude", name: "Claude Code" },
  { id: "opencode", name: "OpenCode" },
] as const;
type Tool = "changes" | "terminal" | "preview";
const tools = [
  { id: "changes", name: "Changes", icon: FileCode2 },
  { id: "terminal", name: "Terminal", icon: Terminal },
  { id: "preview", name: "Preview", icon: Globe },
] as const;
const files = ["WeeklyGoal.tsx", "weekly-goal.test.ts"];

export default function DeveloperWorkspace() {
  const [agent, setAgent] = useState<(typeof agents)[number]["id"]>("codex");
  const [tool, setTool] = useState<Tool>("changes");
  const [file, setFile] = useState(0);
  const [checked, setChecked] = useState(false);
  const [pages, setPages] = useState(60);
  const [notice, setNotice] = useState("");

  return (
    <div className={styles.workbench} id="developer-workspace">
      <div className={styles.workbenchTop}>
        <div className={styles.windowDots} aria-hidden="true">
          <i />
          <i />
          <i />
        </div>
        <span>
          <Folder size={15} />
          reading-corner
          <ChevronDown size={13} />
        </span>
        <span className={styles.workspaceBranch}>
          <GitBranch size={13} />
          feat/weekly-goal
        </span>
        <small>Developer mode</small>
      </div>
      <div className={styles.workbenchBody}>
        <aside className={styles.fileRail} aria-label="Example project files">
          <span>EXPLORER</span>
          <div className={styles.folderRow}>
            <ChevronDown size={12} />
            <Folder size={14} />
            src
          </div>
          <div className={styles.folderRow}>
            <ChevronDown size={12} />
            <Folder size={14} />
            components
          </div>
          {files.map((name, i) => (
            <button
              type="button"
              key={name}
              aria-pressed={tool === "changes" && file === i}
              data-selected={tool === "changes" && file === i}
              onClick={() => {
                setFile(i);
                setTool("changes");
              }}
            >
              <FileCode2 size={13} />
              <span>{name}</span>
              <i>M</i>
            </button>
          ))}
          <div className={styles.projectInfo}>
            <Folder size={28} strokeWidth={1.5} />
            <strong>A little context.</strong>
            <p>
              Project files, instructions
              <br />
              and conversations.
            </p>
          </div>
        </aside>
        <div className={styles.buildChat}>
          <div className={styles.buildChatTitle}>
            <AgentLogo agent={agent} size={28} />
            <span>
              Mindwire<small>Your workspace</small>
            </span>
            <span className={styles.readyDot} />
          </div>
          <div
            className={styles.agentPicker}
            role="group"
            aria-label="Example agent"
          >
            {agents.map((item) => (
              <button
                type="button"
                key={item.id}
                aria-pressed={agent === item.id}
                onClick={() => setAgent(item.id)}
              >
                <AgentLogo agent={item.id} size={15} />
                {item.name}
              </button>
            ))}
          </div>
          <p className={styles.buildPrompt}>
            Add a weekly reading goal. Show my progress and test the edge cases.
          </p>
          <div className={styles.buildReply}>
            <AgentLogo agent={agent} size={20} />
            <div>
              <p>A small feature, ready for a closer look.</p>
              <ul>
                <li>
                  <Check size={13} />
                  Weekly goal component
                </li>
                <li>
                  <Check size={13} />
                  Progress capped at 100%
                </li>
                <li>
                  <Check size={13} />
                  Two focused tests
                </li>
              </ul>
            </div>
          </div>
          <button
            className={styles.reviewLink}
            type="button"
            onClick={() => setTool("preview")}
          >
            Take a look at the result
            <ArrowRight size={14} />
          </button>
          <div className={styles.exampleComposer} aria-hidden="true">
            <span>What shall we build next?</span>
            <div>
              <Plus size={14} />
              <span>{agents.find((item) => item.id === agent)?.name}</span>
              <ArrowUp size={17} />
            </div>
          </div>
        </div>
        <div className={styles.workArea}>
          <div
            className={styles.toolTabs}
            role="tablist"
            aria-label="Explore the workspace"
            onKeyDown={navigateTabs}
          >
            {tools.map(({ id, name, icon: Icon }) => (
              <button
                type="button"
                role="tab"
                id={`workbench-tab-${id}`}
                aria-controls={`workbench-${id}`}
                aria-selected={tool === id}
                tabIndex={tool === id ? 0 : -1}
                onClick={() => setTool(id)}
                key={id}
              >
                <Icon size={14} />
                {name}
              </button>
            ))}
          </div>
          <div
            id="workbench-changes"
            role="tabpanel"
            aria-labelledby="workbench-tab-changes"
            hidden={tool !== "changes"}
            className={styles.toolPanel}
          >
            <div
              className={styles.fileSwitcher}
              aria-label="Choose an example file"
            >
              {files.map((name, i) => (
                <button
                  type="button"
                  key={name}
                  aria-pressed={file === i}
                  onClick={() => setFile(i)}
                >
                  {name}
                </button>
              ))}
            </div>
            <div className={styles.changeMeta}>
              <FileCode2 size={13} />
              src/components<span>+8</span>
            </div>
            <pre className={styles.diff}>
              <code>
                {(file === 0
                  ? [
                      ["", "export function WeeklyGoal({ pages }) {"],
                      ["+", "  const goal = 100;"],
                      ["+", "  const progress = Math.min(pages / goal, 1);"],
                      ["", ""],
                      ["", "  return ("],
                      ["+", "    <GoalCard"],
                      ["+", '      title="A little every day."'],
                      ["+", "      current={pages}"],
                      ["+", "      target={goal}"],
                      ["+", "      progress={progress}"],
                      ["+", "    />"],
                      ["", "  );"],
                      ["", "}"],
                    ]
                  : [
                      ["+", 'test("shows reading progress", () => {'],
                      ["+", "  render(<WeeklyGoal pages={60} />);"],
                      ["+", "  expect(progress()).toBe(0.6);"],
                      ["+", "});"],
                      ["", ""],
                      ["+", 'test("caps progress at the goal", () => {'],
                      ["+", "  render(<WeeklyGoal pages={120} />);"],
                      ["+", "  expect(progress()).toBe(1);"],
                      ["+", "});"],
                    ]
                ).map(([mark, line], i) => (
                  <span key={`${file}-${i}`} data-added={mark === "+"}>
                    <i>{i + 1}</i>
                    <b>{mark || " "}</b>
                    <span>{line || " "}</span>
                  </span>
                ))}
              </code>
            </pre>
            <div className={styles.toolFooter}>
              <span>2 files changed</span>
              <button type="button" onClick={() => setTool("terminal")}>
                Try the checks
                <ArrowRight size={13} />
              </button>
            </div>
          </div>
          <div
            id="workbench-terminal"
            role="tabpanel"
            aria-labelledby="workbench-tab-terminal"
            hidden={tool !== "terminal"}
            className={styles.toolPanel}
          >
            <div className={styles.terminal}>
              <span>
                ~/reading-corner <em>feat/weekly-goal</em>
              </span>
              <code>$ bun run check</code>
              <p>TypeScript · no errors</p>
              <code>$ bun test</code>
              {checked ? (
                <>
                  <p className={styles.terminalPass}>
                    ✓ shows reading progress
                    <br />✓ caps progress at the goal
                  </p>
                  <span>2 passed · 0 failed</span>
                  <code>
                    $ <i className={styles.cursor} />
                  </code>
                </>
              ) : (
                <p>Ready to run the example checks.</p>
              )}
            </div>
            <div className={styles.toolFooter}>
              <span>Simulated output</span>
              <button
                type="button"
                onClick={() => {
                  setChecked(!checked);
                  setNotice(
                    checked
                      ? "Example checks reset."
                      : "Example checks complete: two passed, zero failed.",
                  );
                }}
              >
                {checked ? "Reset example" : "Run example checks"}
                <Play size={12} />
              </button>
            </div>
          </div>
          <div
            id="workbench-preview"
            role="tabpanel"
            aria-labelledby="workbench-tab-preview"
            hidden={tool !== "preview"}
            className={styles.toolPanel}
          >
            <div className={styles.previewAddress}>
              <Globe size={13} />
              reading-corner · example
            </div>
            <div className={styles.readingApp}>
              <span>
                <BookOpen size={16} />
                READING CORNER
              </span>
              <h3>A little every day.</h3>
              <p>Make room for your next chapter.</p>
              <div className={styles.goalNumber}>
                {pages}
                <small>/ 100 pages</small>
              </div>
              <div
                className={styles.goalTrack}
                role="progressbar"
                aria-valuenow={pages}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label="Example weekly reading goal"
              >
                <span style={{ width: `${pages}%` }} />
              </div>
              <div className={styles.goalCaption}>
                <span>This week</span>
                <strong>
                  {pages === 100 ? "You made it!" : `${100 - pages} to go`}
                </strong>
              </div>
              <button
                type="button"
                onClick={() => {
                  const next = pages === 100 ? 60 : pages + 10;
                  setPages(next);
                  setNotice(`Example reading goal: ${next} of 100 pages.`);
                }}
              >
                {pages === 100 ? "Start again" : "Log 10 pages"}
                {pages === 100 ? <ArrowRight size={15} /> : <Plus size={15} />}
              </button>
            </div>
          </div>
        </div>
      </div>
      <div className={styles.workbenchFoot}>
        <span>
          <Check size={12} />
          Your project. Every detail in reach.
        </span>
        <span>Interactive example · Runs only in this page</span>
      </div>
      <span className="sr-only" role="status">
        {notice}
      </span>
    </div>
  );
}
