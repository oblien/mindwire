"use client";

import { useId, useState } from "react";
import Link from "next/link";
import {
  ArrowUpRight,
  Check,
  Cloud,
  Code2,
  FileText,
  Laptop,
  RotateCcw,
  Smartphone,
} from "lucide-react";
import SurfaceRoutes from "./SurfaceRoutes";
import { navigateTabs } from "./tabs";
import styles from "./app-framework.module.css";

export default function AppContinuity() {
  const id = useId();
  const [host, setHost] = useState<"local" | "cloud">("local");
  const [approved, setApproved] = useState(false);
  return (
    <section
      className={styles.continuity}
      id="anywhere"
      aria-labelledby="anywhere-title"
    >
      <div className={styles.sectionHeading}>
        <h2 id="anywhere-title">
          The same workspace.
          <br />A different screen.
        </h2>
        <p>
          Your computer runs the agent. Your phone keeps you in the
          conversation. Follow progress and make the next decision wherever you
          are.
        </p>
      </div>
      <SurfaceRoutes kind="appDevices" className={styles.devices}>
        <div className={styles.hostPanel} data-sprue="app-computer">
          <div className={styles.hostHeading}>
            <img
              src="/images/framework/website-icon.svg"
              alt=""
              width={46}
              height={46}
              loading="lazy"
            />
            <span>A home for your work</span>
          </div>
          <div
            className={styles.hostTabs}
            role="tablist"
            aria-label="Where the work runs"
            onKeyDown={navigateTabs}
          >
            {(["local", "cloud"] as const).map((item) => (
              <button
                type="button"
                role="tab"
                key={item}
                id={`${id}-${item}`}
                aria-selected={host === item}
                aria-controls={`${id}-host`}
                tabIndex={host === item ? 0 : -1}
                onClick={() => setHost(item)}
              >
                {item === "local" ? <Laptop size={19} /> : <Cloud size={19} />}
                {item === "local" ? "Your computer" : "Mindwire Cloud"}
              </button>
            ))}
          </div>
          <div
            role="tabpanel"
            id={`${id}-host`}
            aria-labelledby={`${id}-${host}`}
            className={styles.hostDescription}
          >
            <h3>
              {host === "local"
                ? "Start right where you are."
                : "Give the work its own computer."}
            </h3>
            <p>
              {host === "local"
                ? "Connect your Mac or a server you manage. Keep it awake and online while your agent works with your files and tools."
                : "Use an optional monthly Cloud Computer. Your workspace can stay online when your own computer takes a break."}
            </p>
            <Link href={host === "local" ? "/get-started" : "/cloud"}>
              {host === "local"
                ? "Connect your computer"
                : "Explore Mindwire Cloud"}
              <ArrowUpRight size={17} />
            </Link>
          </div>
          <div className={styles.projectTray}>
            <strong>Reading room</strong>
            <span>
              <FileText size={18} />
              books.csv<span>Source file</span>
            </span>
            <span>
              <FileText size={18} />
              reading-room.html<span>Saved result</span>
            </span>
            <p aria-live="polite">
              <Check size={17} />
              {approved
                ? "Weekly goal approved from your phone."
                : "Weekly goal ready for your review."}
            </p>
          </div>
        </div>
        <div className={styles.phonePanel} data-sprue="app-phone">
          <div className={styles.phoneHeading}>
            <Smartphone size={22} />
            <span>On your phone</span>
          </div>
          <div className={styles.phoneProject}>
            <span>Reading room</span>
            <h3>
              {approved ? "You’re all caught up." : "One quick decision."}
            </h3>
            <p>
              {approved
                ? "Your approval is back in the workspace. Pick up from here on any connected device."
                : "The weekly goal is ready. Review the change and send your decision back to the workspace."}
            </p>
          </div>
          <div className={styles.phoneChange}>
            <Code2 size={18} />
            <span>WeeklyGoal.tsx</span>
            <span>+14</span>
          </div>
          <button
            type="button"
            className={styles.phoneAction}
            onClick={() => setApproved(!approved)}
          >
            {approved ? <RotateCcw size={18} /> : <Check size={18} />}
            {approved ? "Reset example" : "Approve example"}
          </button>
          <p className={styles.phoneCaption}>Interactive continuity example</p>
        </div>
      </SurfaceRoutes>
    </section>
  );
}
