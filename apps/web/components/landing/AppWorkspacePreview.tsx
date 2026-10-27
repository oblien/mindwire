"use client";

import { useId, useState } from "react";
import {
  ArrowRight,
  BookOpen,
  Check,
  Code2,
  FileText,
  Folder,
  GitBranch,
  MessageSquare,
  Plus,
  RotateCcw,
  Terminal,
} from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import styles from "./app-showcase.module.css";

const books = [
  { name: "The Creative Act", category: "Creativity", progress: 68 },
  { name: "Ways of Seeing", category: "Art & ideas", progress: 42 },
  { name: "The Art of Noticing", category: "Everyday practice", progress: 24 },
];
const diff = `export function WeeklyGoal({ pages, onLog }) {
  const target = 100;
  const progress = Math.min(pages / target, 1);

  return (
    <section aria-label="Weekly reading goal">
      <Progress value={progress} />
      <p>{pages} of {target} pages</p>
      <button onClick={() => onLog(10)}>
        Log 10 pages
      </button>
    </section>
  );
}`;
type Mode = "general" | "developer";
type View = "preview" | "files" | "changes" | "terminal";

export default function AppWorkspacePreview() {
  const id = useId();
  const [mode, setMode] = useState<Mode>("general");
  const [view, setView] = useState<View>("preview");
  const [pages, setPages] = useState(70);
  const developer = mode === "developer";
  const views = developer
    ? ([
        { id: "changes", name: "Changes", icon: Code2 },
        { id: "terminal", name: "Terminal", icon: Terminal },
      ] as const)
    : ([
        { id: "preview", name: "Preview", icon: BookOpen },
        { id: "files", name: "Files", icon: Folder },
      ] as const);

  return (
    <div className={styles.preview} data-sprue="app-workspace">
      <div className={styles.modeBar} id="modes">
        <div
          role="tablist"
          aria-label="Explore Mindwire modes"
          onKeyDown={navigateTabs}
          className={styles.modeTabs}
        >
          {(["general", "developer"] as const).map((item) => (
            <button
              type="button"
              role="tab"
              key={item}
              id={`${id}-${item}`}
              aria-selected={mode === item}
              aria-controls={`${id}-workspace`}
              tabIndex={mode === item ? 0 : -1}
              onClick={() => {
                setMode(item);
                setView(item === "general" ? "preview" : "changes");
              }}
            >
              {item === "general" ? (
                <MessageSquare size={18} />
              ) : (
                <Code2 size={18} />
              )}
              {item === "general" ? "General" : "Developer"}
            </button>
          ))}
        </div>
        <span>One workspace. Two ways to work.</span>
      </div>
      <div
        className={styles.window}
        role="tabpanel"
        id={`${id}-workspace`}
        aria-labelledby={`${id}-${mode}`}
      >
        <div className={styles.windowBar}>
          <span className={styles.wordmark}>MINDWIRE</span>
          <span>
            <Folder size={18} /> Reading room
          </span>
          <span className={styles.windowMeta}>
            {developer ? (
              <>
                <GitBranch size={17} /> feat/weekly-goal
              </>
            ) : (
              "Your personal workspace"
            )}
          </span>
        </div>
        <div className={styles.workspace}>
          <div className={styles.conversation}>
            <div className={styles.request}>
              <span>You</span>
              <p>
                {developer
                  ? "Add a weekly reading goal. Let me log pages, show my progress, and check that it works."
                  : "Turn my book list into a reading tracker. Add a weekly goal I can update as I go."}
              </p>
              <button
                type="button"
                onClick={() => setView(developer ? "changes" : "files")}
              >
                <FileText size={17} />
                {developer ? "reading-room / src" : "books.csv"}
                <ArrowRight size={16} />
              </button>
            </div>
            <div className={styles.reply}>
              <strong>
                <AgentLogo agent="codex" size={23} surface="light" />
                Codex
              </strong>
              <p>
                {developer
                  ? "The weekly goal is in place. You can log pages, track progress, and reset for a new week. The checks passed."
                  : "Your reading room is ready. Your books, a weekly goal, and a place to track your progress. All saved in your workspace."}
              </p>
            </div>
            <div className={styles.toolSteps}>
              {(developer
                ? [
                    "Updated WeeklyGoal.tsx",
                    "Added progress checks",
                    "Checked types and tests",
                  ]
                : [
                    "Read your book list",
                    "Built the reading tracker",
                    "Saved reading-room.html",
                  ]
              ).map((step) => (
                <span key={step}>
                  <Check size={16} />
                  {step}
                </span>
              ))}
            </div>
            <button
              className={styles.resultLink}
              type="button"
              onClick={() => setView(developer ? "changes" : "preview")}
            >
              {developer ? "Review the changes" : "Open your reading room"}
              <ArrowRight size={18} />
            </button>
          </div>
          <div className={styles.output}>
            <div className={styles.outputBar}>
              <div
                role="tablist"
                aria-label={
                  developer
                    ? "Example developer tools"
                    : "Explore the app preview"
                }
                onKeyDown={navigateTabs}
              >
                {views.map((item) => (
                  <button
                    type="button"
                    role="tab"
                    key={item.id}
                    id={`${id}-${item.id}`}
                    aria-selected={view === item.id}
                    aria-controls={`${id}-output`}
                    tabIndex={view === item.id ? 0 : -1}
                    onClick={() => setView(item.id)}
                  >
                    <item.icon size={17} />
                    {item.name}
                  </button>
                ))}
              </div>
              <span>{developer ? "2 files changed" : "Saved in Mindwire"}</span>
            </div>
            <div
              className={styles.outputContent}
              role="tabpanel"
              tabIndex={0}
              id={`${id}-output`}
              aria-labelledby={`${id}-${view}`}
            >
              {view === "preview" && (
                <div className={styles.readingRoom}>
                  <div className={styles.readingHeading}>
                    <h3>A little more reading.</h3>
                    <BookOpen size={32} strokeWidth={1.25} />
                  </div>
                  <p>Your shelf. Your pace.</p>
                  <div className={styles.goal}>
                    <div>
                      <span>This week</span>
                      <strong aria-live="polite">
                        {pages}
                        <span> / 100 pages</span>
                      </strong>
                    </div>
                    <div
                      className={styles.progress}
                      role="progressbar"
                      aria-label="Weekly reading goal"
                      aria-valuenow={pages}
                      aria-valuemin={0}
                      aria-valuemax={100}
                    >
                      <span style={{ width: `${pages}%` }} />
                    </div>
                    <div className={styles.goalActions}>
                      <span>
                        {pages === 100
                          ? "Weekly goal complete."
                          : "A few pages go a long way."}
                      </span>
                      <button
                        type="button"
                        onClick={() =>
                          setPages(
                            pages === 100 ? 0 : Math.min(100, pages + 10),
                          )
                        }
                      >
                        {pages === 100 ? (
                          <RotateCcw size={16} />
                        ) : (
                          <Plus size={16} />
                        )}
                        {pages === 100 ? "Start a new week" : "Log 10 pages"}
                      </button>
                    </div>
                  </div>
                  <div className={styles.books}>
                    {books.map((book, index) => (
                      <div key={book.name}>
                        <span className={styles.bookSpine} aria-hidden="true">
                          {String(index + 1).padStart(2, "0")}
                        </span>
                        <div>
                          <strong>{book.name}</strong>
                          <span>{book.category}</span>
                        </div>
                        <span>{book.progress}%</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {view === "files" && (
                <div className={styles.files}>
                  <h3>The files behind the work.</h3>
                  <p>Your source material and the finished result, together.</p>
                  <div className={styles.fileRow}>
                    <FileText size={22} />
                    <div>
                      <strong>books.csv</strong>
                      <span>Your original book list</span>
                    </div>
                  </div>
                  <div className={styles.csv}>
                    <span>title, category, progress</span>
                    {books.map((book) => (
                      <div key={book.name}>
                        {book.name}, {book.category}, {book.progress}%
                      </div>
                    ))}
                  </div>
                  <button
                    type="button"
                    className={styles.fileRow}
                    onClick={() => setView("preview")}
                  >
                    <Code2 size={22} />
                    <div>
                      <strong>reading-room.html</strong>
                      <span>Open the working tracker</span>
                    </div>
                    <ArrowRight size={18} />
                  </button>
                </div>
              )}
              {view === "changes" && (
                <div className={styles.codeView}>
                  <div className={styles.codeHeading}>
                    <FileText size={18} />
                    <span>src / WeeklyGoal.tsx</span>
                    <span>+14</span>
                  </div>
                  <pre>
                    <code>{diff}</code>
                  </pre>
                  <p>
                    <Check size={16} />
                    Ready to review in your project.
                  </p>
                </div>
              )}
              {view === "terminal" && (
                <div className={styles.terminal}>
                  <span>reading-room / feat/weekly-goal</span>
                  <pre>
                    <code>{`$ bun run check

TypeScript · No type errors

$ bun test

✓ adds pages to the weekly goal
✓ caps progress at 100 pages
✓ starts a new week at zero

3 passed · 0 failed`}</code>
                  </pre>
                  <p>
                    <Check size={16} />
                    All checks passed in this example.
                  </p>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
      <div className={styles.caption}>
        <span>Conversations, tools, and finished work. In one place.</span>
        <span>Interactive example · Sample data</span>
      </div>
    </div>
  );
}
