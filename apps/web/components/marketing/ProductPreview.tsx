"use client";

import { useId, useRef, useState, type KeyboardEvent } from "react";
import {
  ArrowUp,
  BatteryFull,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Cloud,
  FileCode2,
  GitBranch,
  ImagePlus,
  Laptop,
  LockKeyhole,
  MessageSquare,
  MoreHorizontal,
  Server,
  ShieldCheck,
  Signal,
  SlidersHorizontal,
  Terminal,
  Wifi,
} from "lucide-react";
import AgentLogo from "./AgentLogo";
import styles from "./ProductPreview.module.css";

const views = [
  { id: "chat", name: "Chat", icon: MessageSquare },
  { id: "changes", name: "Changes", icon: GitBranch },
  { id: "terminal", name: "Terminal", icon: Terminal },
] as const;
type View = (typeof views)[number]["id"];

function ChatPreview() {
  const [commandsOpen, setCommandsOpen] = useState(false);
  const [decision, setDecision] = useState<"allowed" | "declined" | null>(null);
  const commandsId = useId();

  return (
    <>
      <div className={styles.conversation}>
        <div className={styles.timestamp}>Today</div>
        <div className={styles.userMessage}>
          Add a dark mode toggle. Keep it simple.
        </div>
        <div className={styles.agentLabel}>
          <AgentLogo agent="codex" size={17} /> Codex
        </div>
        <p className={styles.reply}>
          I’ll use the existing theme and remember your last selection.
        </p>
        <div className={styles.toolRow}>
          <FileCode2 size={16} />
          <span>theme.tsx</span>
          <b className={styles.added}>+18</b>
          <b className={styles.removed}>−4</b>
        </div>
        <div className={styles.toolRow}>
          <FileCode2 size={16} />
          <span>settings.tsx</span>
          <b className={styles.added}>+9</b>
          <span className={styles.removed}>−2</span>
        </div>
        <button
          type="button"
          className={styles.commandToggle}
          onClick={() => setCommandsOpen(!commandsOpen)}
          aria-expanded={commandsOpen}
          aria-controls={commandsId}
        >
          <Terminal size={16} />
          <span>2 commands</span>
          <ChevronDown
            size={14}
            className={commandsOpen ? styles.rotated : ""}
          />
        </button>
        <div
          id={commandsId}
          hidden={!commandsOpen}
          className={styles.commandOutput}
        >
          <code>git diff --stat</code>
          <code>npm run typecheck</code>
          <span>Type check passed.</span>
        </div>
        <p className={styles.reply}>
          The toggle is ready. I’ll check that the preference survives a reload.
        </p>
      </div>
      <div className={styles.approval} aria-live="polite">
        {decision ? (
          <>
            <div className={styles.approvalTitle}>
              <ShieldCheck size={16} />{" "}
              {decision === "allowed"
                ? "Approved. Checks passed."
                : "Declined. You’re in control."}
            </div>
            <button
              type="button"
              className={styles.resetDemo}
              onClick={() => setDecision(null)}
            >
              Try the approval again
            </button>
          </>
        ) : (
          <>
            <div className={styles.approvalTitle}>
              <ShieldCheck size={16} /> Run the final checks?
            </div>
            <code className={styles.approvalCommand}>npm test</code>
            <div className={styles.approvalActions}>
              <button type="button" onClick={() => setDecision("declined")}>
                Decline
              </button>
              <button type="button" onClick={() => setDecision("allowed")}>
                Allow once <Check size={14} />
              </button>
            </div>
          </>
        )}
      </div>
      <div className={styles.composer} aria-hidden="true">
        <span>Message Codex…</span>
        <div>
          <SlidersHorizontal size={17} />
          <ImagePlus size={17} />
          <span>
            Model · Auto <ChevronDown size={10} />
          </span>
          <i>
            <ArrowUp size={16} />
          </i>
        </div>
      </div>
    </>
  );
}

const diff = [
  { number: "12", kind: "context", text: "export function useTheme() {" },
  { number: "13", kind: "removed", text: '  const theme = "light";' },
  { number: "13", kind: "added", text: "  const [theme, setTheme] =" },
  { number: "14", kind: "added", text: "    useState(savedTheme);" },
  { number: "15", kind: "context", text: "" },
  { number: "16", kind: "added", text: "  useEffect(() => {" },
  { number: "17", kind: "added", text: "    saveTheme(theme);" },
  { number: "18", kind: "added", text: "  }, [theme]);" },
  { number: "19", kind: "context", text: "" },
  { number: "20", kind: "context", text: "  return { theme, setTheme };" },
  { number: "21", kind: "context", text: "}" },
] as const;

function ChangesPreview() {
  const [staged, setStaged] = useState(false);
  return (
    <div className={styles.changes}>
      <div className={styles.branch}>
        <GitBranch size={16} />
        <span>feature/dark-mode</span>
        <ChevronDown size={13} />
      </div>
      <div className={styles.changeHeading}>
        <span>
          Changes <small>2</small>
        </span>
        <span className={styles.added}>
          +27 <b className={styles.removed}>−6</b>
        </span>
      </div>
      <div className={styles.fileHeading}>
        <FileCode2 size={16} /> theme.tsx <span>Modified</span>
      </div>
      <div className={styles.diffCode} aria-label="Example code diff">
        <div className={styles.diffHunk}>@@ useTheme</div>
        {diff.map((line, index) => (
          <div
            key={index}
            className={`${styles.diffLine} ${line.kind === "added" ? styles.diffAdded : line.kind === "removed" ? styles.diffRemoved : ""}`}
          >
            <span>{line.number}</span>
            <span>
              {line.kind === "added"
                ? "+"
                : line.kind === "removed"
                  ? "−"
                  : " "}
            </span>
            <code>{line.text || " "}</code>
          </div>
        ))}
      </div>
      <div className={styles.toolRow}>
        <FileCode2 size={16} />
        <span>settings.tsx</span>
        <b className={styles.added}>+9</b>
        <b className={styles.removed}>−2</b>
        <ChevronRight size={12} />
      </div>
      <div className={styles.diffNote}>
        <MessageSquare size={17} />
        <p>
          See something to change?
          <br />
          <strong>Send a line back to your agent.</strong>
        </p>
      </div>
      <button
        type="button"
        className={styles.stageButton}
        onClick={() => setStaged(!staged)}
        aria-pressed={staged}
      >
        {staged ? (
          <>
            <Check size={16} /> Staged · tap to unstage
          </>
        ) : (
          "Stage changes"
        )}
      </button>
    </div>
  );
}

function TerminalPreview() {
  return (
    <div className={styles.terminal}>
      <div className={styles.terminalTab}>
        <Terminal size={14} /> atlas — zsh <span>1</span>
      </div>
      <div className={styles.terminalBody}>
        <p className={styles.terminalMuted}>~/projects/atlas</p>
        <p>
          <span className={styles.terminalPrompt}>❯</span> npm test
        </p>
        <br />
        <p className={styles.terminalMuted}>Running tests…</p>
        <p>
          <span className={styles.added}>✓</span> remembers the theme
        </p>
        <p>
          <span className={styles.added}>✓</span> follows system appearance
        </p>
        <p>
          <span className={styles.added}>✓</span> toggles from settings
        </p>
        <br />
        <p>
          <span className={styles.added}>3 passed</span>{" "}
          <span className={styles.terminalMuted}>(420 ms)</span>
        </p>
        <br />
        <p>
          <span className={styles.terminalPrompt}>❯</span>{" "}
          <span className={styles.cursor} />
        </p>
      </div>
      <div className={styles.terminalConnection}>
        <span /> My MacBook <ShieldCheck size={13} /> Encrypted SSH
      </div>
      <div className={styles.terminalKeys} aria-hidden="true">
        {["esc", "ctrl", "tab", "⌘", "←", "↓", "↑", "→"].map((key) => (
          <span key={key}>{key}</span>
        ))}
      </div>
    </div>
  );
}

export default function ProductPreview() {
  const [view, setView] = useState<View>("chat");
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const id = useId();

  function moveTab(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const next =
      event.key === "ArrowRight"
        ? (index + 1) % views.length
        : event.key === "ArrowLeft"
          ? (index + views.length - 1) % views.length
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? views.length - 1
              : null;
    if (next === null) return;
    event.preventDefault();
    setView(views[next].id);
    tabRefs.current[next]?.focus();
  }

  return (
    <figure className={styles.preview}>
      <div className={styles.stage}>
        <div className={styles.connection} aria-hidden="true">
          <svg viewBox="0 0 210 300" fill="none" className={styles.connectionLines}>
            <path d="M38 50H68V250H38M38 150H140" />
            <path className={styles.connectionPulse} d="M38 50H68V150H140" pathLength="1" />
          </svg>
          <span className={`${styles.connectionNode} ${styles.computerNode}`}>
            <Laptop size={20} strokeWidth={1.5} />
          </span>
          <span className={`${styles.connectionNode} ${styles.serverNode}`}>
            <Server size={19} strokeWidth={1.5} />
          </span>
          <span className={`${styles.connectionNode} ${styles.cloudNode}`}>
            <Cloud size={20} strokeWidth={1.5} />
          </span>
          <span className={styles.secureNode}>
            <LockKeyhole size={15} strokeWidth={1.5} />
          </span>
        </div>
        <div className={styles.phoneFrame}>
          <div className={styles.phone}>
            <div className={styles.phoneScreen}>
              <div className={styles.statusBar} aria-hidden="true">
                <b>9:41</b>
                <div className={styles.island} />
                <div>
                  <Signal size={13} />
                  <Wifi size={14} />
                  <BatteryFull size={19} />
                </div>
              </div>
              <div className={styles.appHeader} aria-hidden="true">
                <ChevronLeft size={23} />
                <div>
                  <strong>
                    {view === "chat"
                      ? "Dark mode"
                      : view === "changes"
                        ? "Source control"
                        : "Terminal"}
                  </strong>
                  <span>
                    <AgentLogo agent="codex" size={11} /> Codex <i /> atlas
                  </span>
                </div>
                <MoreHorizontal size={21} />
              </div>
              {views.map((item) => (
                <div
                  key={item.id}
                  role="tabpanel"
                  id={`${id}-${item.id}-panel`}
                  aria-labelledby={`${id}-${item.id}-tab`}
                  hidden={view !== item.id}
                  tabIndex={0}
                  className={styles.panel}
                >
                  {item.id === "chat" ? (
                    <ChatPreview />
                  ) : item.id === "changes" ? (
                    <ChangesPreview />
                  ) : (
                    <TerminalPreview />
                  )}
                </div>
              ))}
              <div className={styles.homeIndicator} aria-hidden="true" />
            </div>
          </div>
        </div>
      </div>
      <div
        className={styles.tabs}
        role="tablist"
        aria-label="Explore the app preview"
      >
        {views.map((item, index) => (
          <button
            key={item.id}
            ref={(el) => {
              tabRefs.current[index] = el;
            }}
            type="button"
            role="tab"
            id={`${id}-${item.id}-tab`}
            aria-controls={`${id}-${item.id}-panel`}
            aria-selected={view === item.id}
            tabIndex={view === item.id ? 0 : -1}
            onClick={() => setView(item.id)}
            onKeyDown={(event) => moveTab(event, index)}
          >
            <item.icon size={15} />
            {item.name}
          </button>
        ))}
      </div>
      <figcaption className={styles.caption}>
        Interactive preview · example project
      </figcaption>
    </figure>
  );
}
