"use client";

import {
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import {
  ArrowRight,
  Bell,
  Check,
  ChevronDown,
  FileCode2,
  GitBranch,
  MessageSquare,
  Pause,
  Play,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
} from "lucide-react";
import Corners from "@/components/Corners";
import AgentLogo from "./AgentLogo";
import styles from "./WorkflowShowcase.module.css";

const steps = [
  {
    id: "chat",
    label: "Chat",
    icon: MessageSquare,
    title: "Pick up the conversation.",
    body: "Continue native Codex and Claude chats, or start something new. Your agent keeps working in the project you already know.",
    detail: "Your agents. Your real context.",
  },
  {
    id: "review",
    label: "Review",
    icon: GitBranch,
    title: "Every line, right here.",
    body: "Read the diff, send a line back to your agent, and stage what’s ready. Branches, commits, push, and pull come with you.",
    detail: "From the first edit to the final commit.",
  },
  {
    id: "approve",
    label: "Approve",
    icon: ShieldCheck,
    title: "The next move is yours.",
    body: "Answer a question or approve a command without losing your place. See what’s being asked before your agent continues.",
    detail: "A clear question. A simple answer.",
  },
  {
    id: "models",
    label: "Models",
    icon: SlidersHorizontal,
    title: "Your agent. Your settings.",
    body: "Choose models, reasoning effort, and permissions where supported. Bring your own subscription or API key.",
    detail: "The controls you use at your desk.",
  },
  {
    id: "terminal",
    label: "Terminal",
    icon: Terminal,
    title: "A real shell. In your pocket.",
    body: "Run a command on your workspace and come back to the same terminal while Mindwire keeps the session running.",
    detail: "Connected to the machine doing the work.",
  },
  {
    id: "updates",
    label: "Updates",
    icon: Bell,
    title: "Step away. Stay in the loop.",
    body: "Get a push when supported workspaces need your attention. Choose which chats and agents can notify you.",
    detail: "Less checking. More getting on with your day.",
  },
] as const;

type Step = (typeof steps)[number]["id"];

function Reveal({
  order = 0,
  className = "",
  children,
}: {
  order?: number;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={[styles.reveal, className].join(" ")}
      style={{ "--order": order } as CSSProperties}
    >
      {children}
    </div>
  );
}

function FileRow({ name, added, removed }: {
  name: string;
  added: number;
  removed: number;
}) {
  return (
    <div className={styles.fileRow}>
      <FileCode2 size={16} />
      <span>{name}</span>
      <b className={styles.added}>+{added}</b>
      <b className={styles.removed}>−{removed}</b>
    </div>
  );
}

function Scene({ step }: { step: Step }) {
  if (step === "chat") {
    return (
      <>
        <Reveal className={styles.message}>
          <span>You</span>
          <p>Add a dark mode toggle. Keep it simple.</p>
        </Reveal>
        <Reveal order={2} className={styles.response}>
          <AgentLogo agent="codex" size={22} />
          <div>
            <strong>Codex</strong>
            <p>I’ll use your existing theme and remember your preference.</p>
          </div>
        </Reveal>
        <Reveal order={4} className={styles.fileStack}>
          <FileRow name="theme.tsx" added={18} removed={4} />
          <FileRow name="settings.tsx" added={9} removed={2} />
        </Reveal>
      </>
    );
  }

  if (step === "review") {
    return (
      <>
        <Reveal className={styles.diffHeading}>
          <FileCode2 size={15} />
          <span>theme.tsx</span>
          <b className={styles.added}>+18</b>
          <b className={styles.removed}>−4</b>
        </Reveal>
        <div className={styles.code}>
          <Reveal order={1} className={styles.codeLine}>
            <span>12</span><i> </i><code>function useTheme() {"{"}</code>
          </Reveal>
          <Reveal order={2} className={[styles.codeLine, styles.diffRemoved].join(" ")}>
            <span>13</span><i>−</i><code>  const theme = "light";</code>
          </Reveal>
          <Reveal order={3} className={[styles.codeLine, styles.diffAdded].join(" ")}>
            <span>13</span><i>+</i><code>  const [theme, setTheme] =</code>
          </Reveal>
          <Reveal order={4} className={[styles.codeLine, styles.diffAdded].join(" ")}>
            <span>14</span><i>+</i><code>    useState(savedTheme);</code>
          </Reveal>
          <Reveal order={5} className={styles.codeLine}>
            <span>15</span><i> </i><code>{"}"}</code>
          </Reveal>
        </div>
        <Reveal order={6} className={styles.sceneFooter}>
          <span><MessageSquare size={14} /> Send a line to your agent</span>
          <span className={styles.miniAction}><Check size={14} /> Stage</span>
        </Reveal>
      </>
    );
  }

  if (step === "approve") {
    return (
      <>
        <Reveal className={styles.requestHeading}>
          <ShieldCheck size={21} />
          <div>
            <strong>Run the final checks?</strong>
            <span>Codex is waiting for your approval.</span>
          </div>
        </Reveal>
        <Reveal order={2} className={styles.command}>
          <Terminal size={16} /><code>npm test</code>
        </Reveal>
        <Reveal order={3} className={styles.choiceRow}>
          <span>Decline</span>
          <span className={styles.selectedChoice}>Allow once <Check size={14} /></span>
        </Reveal>
        <Reveal order={6} className={styles.success}>
          <Check size={15} /> Approved. Your agent can continue.
        </Reveal>
      </>
    );
  }

  if (step === "models") {
    return (
      <>
        <Reveal className={styles.settingsHeading}>
          <AgentLogo agent="codex" size={22} />
          <strong>Codex</strong>
          <span>Chat settings</span>
        </Reveal>
        <Reveal order={1} className={styles.settingRow}>
          <span>Model</span><strong>Agent default <ChevronDown size={13} /></strong>
        </Reveal>
        <Reveal order={2} className={styles.reasoning}>
          <span>Reasoning effort</span>
          <div>
            <span>Low</span><span>Medium</span><span className={styles.selectedEffort}>High</span>
          </div>
        </Reveal>
        <Reveal order={4} className={styles.settingRow}>
          <span>Permissions</span><strong>Ask before commands <ChevronDown size={13} /></strong>
        </Reveal>
        <Reveal order={5} className={styles.settingsNote}>Settings follow the agent’s capabilities.</Reveal>
      </>
    );
  }

  if (step === "terminal") {
    return (
      <div className={styles.terminalBody}>
        <Reveal className={styles.shellPrompt}><span>atlas</span> <i>❯</i> npm test</Reveal>
        <Reveal order={2} className={styles.testOutput}>
          <p><Check size={13} /> remembers the theme</p>
          <p><Check size={13} /> follows system appearance</p>
          <p><Check size={13} /> toggles from settings</p>
        </Reveal>
        <Reveal order={4} className={styles.testSummary}><strong>3 passed</strong><span>420 ms</span></Reveal>
        <Reveal order={5} className={styles.shellPrompt}><span>atlas</span> <i>❯</i> <b className={styles.cursor} /></Reveal>
        <Reveal order={6} className={styles.terminalFooter}><span /> Session running on your workspace</Reveal>
      </div>
    );
  }

  return (
    <>
      <Reveal className={styles.notification}>
        <AgentLogo agent="claude" size={29} />
        <div>
          <span>Claude Code <small>now</small></span>
          <strong>Your changes are ready.</strong>
          <p>atlas · Review the update when you’re ready.</p>
        </div>
      </Reveal>
      <Reveal order={3} className={styles.notification}>
        <AgentLogo agent="codex" size={28} />
        <div>
          <span>Codex <small>now</small></span>
          <strong>A quick question for you.</strong>
          <p>api · Choose how you’d like to continue.</p>
        </div>
      </Reveal>
      <Reveal order={5} className={styles.notificationNote}><Bell size={14} /> Your chats. Your notification settings.</Reveal>
    </>
  );
}

export default function WorkflowShowcase() {
  const [active, setActive] = useState(0);
  const [autoplay, setAutoplay] = useState(true);
  const [inView, setInView] = useState(false);
  const [pageVisible, setPageVisible] = useState(true);
  const [reducedMotion, setReducedMotion] = useState(false);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const id = useId();

  useEffect(() => {
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const updateMotion = () => setReducedMotion(media.matches);
    const updateVisibility = () => setPageVisible(!document.hidden);
    updateMotion();
    updateVisibility();
    media.addEventListener("change", updateMotion);
    document.addEventListener("visibilitychange", updateVisibility);
    const observer = new IntersectionObserver(
      ([entry]) => setInView(entry.isIntersecting && entry.intersectionRatio >= 0.3),
      { threshold: [0, 0.3] },
    );
    if (root.current) observer.observe(root.current);
    return () => {
      observer.disconnect();
      media.removeEventListener("change", updateMotion);
      document.removeEventListener("visibilitychange", updateVisibility);
    };
  }, []);

  useEffect(() => {
    if (!autoplay || !inView || !pageVisible || reducedMotion || hovered || focused) return;
    const timer = window.setTimeout(() => {
      setActive((current) => (current + 1) % steps.length);
    }, 6500);
    return () => window.clearTimeout(timer);
  }, [active, autoplay, inView, pageVisible, reducedMotion, hovered, focused]);

  function select(index: number) {
    setActive(index);
    setAutoplay(false);
  }

  function moveTab(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const next = event.key === "ArrowRight" ? (index + 1) % steps.length
      : event.key === "ArrowLeft" ? (index + steps.length - 1) % steps.length
      : event.key === "Home" ? 0
      : event.key === "End" ? steps.length - 1
      : null;
    if (next === null) return;
    event.preventDefault();
    select(next);
    tabs.current[next]?.focus();
  }

  return (
    <div
      ref={root}
      className={styles.showcase}
      data-in-view={inView && pageVisible}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocusCapture={(event) => setFocused(!(event.target as HTMLElement).closest("[data-tour-play]"))}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false);
      }}
    >
      <Corners />
      <div className={styles.tabs} role="tablist" aria-label="Explore the workflow">
        {steps.map((step, index) => (
          <button
            key={step.id}
            ref={(element) => { tabs.current[index] = element; }}
            type="button"
            role="tab"
            id={id + "-" + step.id + "-tab"}
            aria-controls={id + "-" + step.id + "-panel"}
            aria-selected={active === index}
            tabIndex={active === index ? 0 : -1}
            onClick={() => select(index)}
            onKeyDown={(event) => moveTab(event, index)}
          >
            <step.icon size={17} strokeWidth={1.5} aria-hidden="true" />
            {step.label}
          </button>
        ))}
      </div>
      {steps.map((step, index) => (
        <div
          key={step.id}
          className={styles.panel}
          role="tabpanel"
          tabIndex={0}
          id={id + "-" + step.id + "-panel"}
          aria-labelledby={id + "-" + step.id + "-tab"}
          hidden={active !== index}
        >
          <div className={styles.copy}>
            <span className={styles.stepNumber}>{String(index + 1).padStart(2, "0")} / {step.label}</span>
            <h3>{step.title}</h3>
            <p>{step.body}</p>
            <span className={styles.detail}><ArrowRight size={14} aria-hidden="true" />{step.detail}</span>
          </div>
          <div className={styles.visual} aria-hidden="true">
            <div className={styles.frame}>
              <div className={styles.frameHeader}>
                <span><step.icon size={14} strokeWidth={1.5} />{step.id === "terminal" ? "atlas — zsh" : step.label}</span>
                <span>{step.id === "updates" ? "Mindwire" : "atlas"}</span>
              </div>
              <div className={styles.scene}><Scene step={step.id} /></div>
            </div>
          </div>
        </div>
      ))}
      <div className={styles.footer}>
        <span>One project. Every step.</span>
        {reducedMotion ? (
          <span>Select a step to explore</span>
        ) : (
          <button
            type="button"
            data-tour-play
            onClick={() => setAutoplay(!autoplay)}
            aria-label={autoplay ? "Pause workflow animation" : "Play workflow animation"}
          >
            {autoplay ? <Pause size={13} aria-hidden="true" /> : <Play size={13} aria-hidden="true" />}
            {autoplay ? "Pause" : "Play"}
          </button>
        )}
      </div>
    </div>
  );
}
