"use client";

import { useId, useRef, useState } from "react";
import Link from "next/link";
import AgentLogo from "../marketing/AgentLogo";
import SurfaceRoutes from "./SurfaceRoutes";
import { navigateTabs } from "./tabs";
import styles from "./app-showcase.module.css";

type Mode = "general" | "developer";
type TaskState = "review" | "approved" | "changes";
const photograph = "/images/launch-demo/fieldwork-retreat.webp";
const changedFiles = [
  "src / LaunchPage.tsx",
  "src / launch.css",
  "tests / launch.test.tsx",
];

export default function AppWorkspacePreview() {
  const id = useId();
  const [mode, setMode] = useState<Mode>("general");
  const [host, setHost] = useState<"local" | "cloud">("local");
  const [task, setTask] = useState<TaskState>("review");
  const [view, setView] = useState<"changes" | "terminal">("changes");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [followup, setFollowup] = useState("");
  const preview = useRef<HTMLDialogElement>(null);
  const developer = mode === "developer";
  const reset = () => {
    setTask("review");
    setEditing(false);
    setDraft("");
    setFollowup("");
  };
  const status =
    task === "approved"
      ? developer
        ? "Changes approved from your phone"
        : "This version is saved in your workspace"
      : task === "changes"
        ? "Your follow-up is in the conversation"
        : developer
          ? "3 files changed. Ready to review."
          : "Your page is ready. Make it yours.";

  return (
    <>
      <SurfaceRoutes
        kind="appWorkspace"
        className={styles.connections}
        id="anywhere"
      >
        <div className={styles.computer} data-sprue="app-host">
          <span className={styles.cardLabel}>Where it runs</span>
          <div
            className={styles.hostTabs}
            role="tablist"
            aria-label="Where the work runs"
            onKeyDown={navigateTabs}
          >
            {(["local", "cloud"] as const).map((value) => (
              <button
                type="button"
                role="tab"
                key={value}
                id={`${id}-host-${value}`}
                aria-controls={`${id}-host-panel`}
                aria-selected={host === value}
                tabIndex={host === value ? 0 : -1}
                onClick={() => setHost(value)}
              >
                {value === "local" ? "Your computer" : "Cloud"}
              </button>
            ))}
          </div>
          <div
            id={`${id}-host-panel`}
            role="tabpanel"
            tabIndex={0}
            aria-labelledby={`${id}-host-${host}`}
          >
            <h3>
              {host === "local" ? "Your Mac or server." : "A Cloud Computer."}
            </h3>
            <p>
              {host === "local"
                ? "The work happens here. Your files and projects stay together while you connect from another device."
                : "Keep your workspace online, even when your own laptop is closed. Connect from your desktop or phone."}
            </p>
            <div className={styles.projectFiles}>
              <strong>
                {developer ? "~/projects/fieldwork" : "Fieldwork retreat"}
              </strong>
              <ul>
                {(developer
                  ? changedFiles
                  : ["Retreat brief", "Venue photos", "Finished launch page"]
                ).map((file, index) => (
                  <li key={file}>
                    <span>{file}</span>
                    <span>
                      {developer ? "M" : ["Notes", "8 photos", "Draft"][index]}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
            <p className={styles.hostNote}>
              {host === "local"
                ? "Keep this computer online while work runs."
                : "An optional home for your workspace."}
            </p>
            <Link href={host === "local" ? "/get-started" : "/cloud"}>
              {host === "local" ? "Connect a computer" : "Explore Cloud"}
              <span aria-hidden="true">↗</span>
            </Link>
          </div>
        </div>

        <div className={styles.workspace} data-sprue="app-workspace">
          <div className={styles.workspaceBar}>
            <span className={styles.wordmark}>MINDWIRE</span>
            <span>On your desktop</span>
          </div>
          <div
            className={styles.modeTabs}
            id="modes"
            role="tablist"
            aria-label="Explore Mindwire modes"
            onKeyDown={navigateTabs}
          >
            {(["general", "developer"] as const).map((value) => (
              <button
                type="button"
                role="tab"
                key={value}
                id={`${id}-mode-${value}`}
                aria-controls={`${id}-work`}
                aria-selected={mode === value}
                tabIndex={mode === value ? 0 : -1}
                onClick={() => {
                  setMode(value);
                  setEditing(false);
                }}
              >
                {value === "general" ? "General" : "Developer"}
              </button>
            ))}
          </div>
          <div
            role="tabpanel"
            id={`${id}-work`}
            aria-labelledby={`${id}-mode-${mode}`}
            tabIndex={0}
          >
            <div className={styles.modeDescription}>
              <h3>
                {developer
                  ? "A workspace for your coding agents."
                  : "Make things from a conversation."}
              </h3>
              <p>
                {developer
                  ? "Agent sessions, code changes, Git, and your terminal."
                  : "Chat, your files, and the things you want to create."}
              </p>
            </div>
            <div className={styles.request}>
              <span>You</span>
              <p>
                {developer
                  ? "Build the retreat page from this brief. Add the gallery, run checks, and show me the diff."
                  : "Make a page for our design retreat. Use this brief and the venue photos."}
              </p>
              <span className={styles.attachment}>
                {developer
                  ? "retreat-brief.md · 8 photos"
                  : "Retreat brief · 8 photos"}
              </span>
            </div>
            <div className={styles.activity}>
              <div>
                {developer && <AgentLogo agent="codex" size={23} />}
                <span>{developer ? "Codex" : "Mindwire"}</span>
                {developer && (
                  <span className={styles.branch}>feat/retreat-page</span>
                )}
              </div>
              <p>
                {developer
                  ? "The page is built. All three checks pass. Here’s what changed."
                  : "Your launch page is ready. Preview it here, then tell me what you’d like to change."}
              </p>
            </div>
            {developer ? (
              <div className={styles.developerResult}>
                <div
                  className={styles.codeTabs}
                  role="tablist"
                  aria-label="Developer work"
                  onKeyDown={navigateTabs}
                >
                  {(["changes", "terminal"] as const).map((value) => (
                    <button
                      key={value}
                      type="button"
                      role="tab"
                      id={`${id}-${value}`}
                      aria-controls={`${id}-code-panel`}
                      aria-selected={view === value}
                      tabIndex={view === value ? 0 : -1}
                      onClick={() => setView(value)}
                    >
                      {value === "changes" ? "Changes" : "Terminal"}
                    </button>
                  ))}
                </div>
                <div
                  role="tabpanel"
                  id={`${id}-code-panel`}
                  aria-labelledby={`${id}-${view}`}
                  tabIndex={0}
                >
                  {view === "changes" ? (
                    <>
                      <div className={styles.codeHeading}>
                        <span>LaunchPage.tsx</span>
                        <span>Diff excerpt</span>
                      </div>
                      <pre className={styles.diff}>
                        <code>
                          <span className={styles.removed}>
                            {"- <h1>Fieldwork</h1>\n"}
                          </span>
                          <span className={styles.added}>
                            {
                              "+ <h1>A change of scene.</h1>\n+ <Gallery images={photos} />\n+ <EnquiryForm retreat={retreat} />"
                            }
                          </span>
                        </code>
                      </pre>
                    </>
                  ) : (
                    <pre className={styles.terminal}>
                      <code>
                        {
                          "$ bun run check\nTypeScript: no errors\n\n$ bun test\n3 passed · 0 failed"
                        }
                      </code>
                    </pre>
                  )}
                </div>
              </div>
            ) : (
              <button
                type="button"
                className={styles.resultPreview}
                onClick={() => preview.current?.showModal()}
                aria-label="Preview the Fieldwork launch page"
              >
                <img
                  src={photograph}
                  alt="Sunlit retreat terrace overlooking the sea"
                  width={768}
                  height={512}
                  loading="lazy"
                />
                <span className={styles.previewContent}>
                  <span>Fieldwork</span>
                  <strong>
                    A change
                    <br />
                    of scene.
                  </strong>
                </span>
                <span className={styles.previewLink}>
                  Preview your page <span aria-hidden="true">↗</span>
                </span>
              </button>
            )}
            {task === "changes" && (
              <div className={styles.followup}>
                <span>From your phone</span>
                <p>{followup}</p>
              </div>
            )}
          </div>
          <div className={styles.workStatus} role="status">
            {status}
          </div>
        </div>

        <div className={styles.phone} data-sprue="app-remote">
          <div className={styles.phoneSpeaker} aria-hidden="true" />
          <span className={styles.cardLabel}>On your phone</span>
          <span className={styles.phoneProject}>Fieldwork retreat</span>
          <h3>
            {task === "approved"
              ? developer
                ? "Changes approved."
                : "A version to keep."
              : task === "changes"
                ? "Your next idea is in."
                : developer
                  ? "Review from anywhere."
                  : "Keep creating, anywhere."}
          </h3>
          <p>
            {task === "approved"
              ? "Your decision is back in the workspace. Continue from any connected device."
              : task === "changes"
                ? "Your follow-up is in the same conversation, ready to pick up on your computer."
                : developer
                  ? "Review the changes, approve the next step, or send your agent another instruction."
                  : "Open the result, ask for a change, or start your next idea. Your work comes with you."}
          </p>
          {developer ? (
            <div className={styles.phoneReview}>
              <span>feat/retreat-page</span>
              <strong>3 files changed</strong>
              <span>3 checks passed</span>
            </div>
          ) : (
            <button
              type="button"
              className={styles.phonePreview}
              onClick={() => preview.current?.showModal()}
            >
              <img
                src={photograph}
                alt=""
                width={60}
                height={60}
                loading="lazy"
              />
              <span>
                Your launch page<small>Open preview</small>
              </span>
              <span aria-hidden="true">↗</span>
            </button>
          )}
          {task === "review" ? (
            <>
              {!editing ? (
                <div className={styles.remoteActions}>
                  <button
                    type="button"
                    className={styles.primary}
                    onClick={() => setTask("approved")}
                  >
                    {developer ? "Approve changes" : "Keep this version"}
                  </button>
                  <button
                    type="button"
                    className={styles.textButton}
                    onClick={() => setEditing(true)}
                  >
                    Ask for a change <span aria-hidden="true">→</span>
                  </button>
                </div>
              ) : (
                <form
                  className={styles.followupForm}
                  onSubmit={(event) => {
                    event.preventDefault();
                    const value = draft.trim();
                    if (!value) return;
                    setFollowup(value);
                    setTask("changes");
                    setEditing(false);
                  }}
                >
                  <label htmlFor={`${id}-followup`}>Your follow-up</label>
                  <textarea
                    id={`${id}-followup`}
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    maxLength={240}
                    rows={3}
                    placeholder={
                      developer
                        ? "Add a mobile layout check…"
                        : "Make the headline shorter…"
                    }
                    required
                  />
                  <button
                    type="submit"
                    className={styles.primary}
                    disabled={!draft.trim()}
                  >
                    Send follow-up <span aria-hidden="true">→</span>
                  </button>
                  <button
                    type="button"
                    className={styles.textButton}
                    onClick={() => setEditing(false)}
                  >
                    Cancel
                  </button>
                </form>
              )}
            </>
          ) : (
            <div className={styles.remoteActions}>
              <p className={styles.confirmation} role="status">
                {task === "approved"
                  ? developer
                    ? "Approved in this example"
                    : "Saved in this example"
                  : "Follow-up added to the example"}
              </p>
              <button
                type="button"
                className={styles.textButton}
                onClick={reset}
              >
                Reset example <span aria-hidden="true">↺</span>
              </button>
            </div>
          )}
        </div>
      </SurfaceRoutes>
      <div className={styles.caption}>
        <span>
          Two views of the same workspace. Keep your conversation and work as
          you switch.
        </span>
        <span>Interactive example · Sample project</span>
      </div>
      <dialog
        ref={preview}
        className={styles.previewDialog}
        aria-labelledby={`${id}-preview-title`}
        onClick={(event) => {
          if (event.target === event.currentTarget) preview.current?.close();
        }}
      >
        <div className={styles.dialogBar}>
          <span>Fieldwork / Page preview</span>
          <button type="button" onClick={() => preview.current?.close()}>
            Close <span aria-hidden="true">×</span>
          </button>
        </div>
        <div className={styles.previewPage}>
          <img
            src={photograph}
            alt="A quiet seaside terrace at the example Fieldwork retreat"
            width={1536}
            height={1024}
          />
          <div>
            <span>Fieldwork — A design retreat</span>
            <h2 id={`${id}-preview-title`}>
              A change of scene.
              <br />
              Room for new ideas.
            </h2>
            <p>
              A few days by the sea. Space to make things, share a table, and
              see your work a little differently.
            </p>
          </div>
        </div>
        <div className={styles.dialogFoot}>
          <span>Example page created in the workspace</span>
          <button type="button" onClick={() => preview.current?.close()}>
            Back to the conversation <span aria-hidden="true">→</span>
          </button>
        </div>
      </dialog>
    </>
  );
}
