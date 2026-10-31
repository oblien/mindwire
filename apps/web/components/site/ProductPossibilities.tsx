"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  FileText,
  Folder,
  Monitor,
  PanelRight,
  RotateCcw,
} from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import styles from "./product-possibilities.module.css";

const localAgents = [
  { id: "codex", name: "Codex", account: "ChatGPT subscription" },
  { id: "claude", name: "Claude Code", account: "Claude subscription" },
] as const;

export default function ProductPossibilities() {
  const [account, setAccount] = useState(0);
  const [connection, setConnection] = useState(0);
  const agent = localAgents[account];

  useEffect(() => {
    if (connection !== 1) return;
    const delay = window.matchMedia("(prefers-reduced-motion: reduce)").matches
      ? 0
      : 1000;
    const timer = window.setTimeout(() => setConnection(2), delay);
    return () => window.clearTimeout(timer);
  }, [connection]);

  return (
    <>
      <section
        className={styles.desktop}
        aria-labelledby="desktop-integration-title"
      >
        <div className={styles.desktopCopy}>
          <p className={styles.eyebrow}>Desktop experiences</p>
          <h2 id="desktop-integration-title">
            Their subscription. <br />
            Your desktop app.
          </h2>
          <p>
            Mindwire discovers and verifies supported Codex and Claude accounts
            on the user’s computer. Bring their existing subscription into your
            desktop experience, without asking for an API key.
          </p>
          <Link href="/docs/guides/authentication#reuse-a-computers-native-connection">
            Build with native sign-in <ArrowUpRight size={18} />
          </Link>
        </div>

        <div className={styles.desktopExample}>
          <div className={styles.desktopWindow}>
            <div className={styles.windowBar}>
              <div className={styles.windowDots} aria-hidden="true">
                <i />
                <i />
                <i />
              </div>
              <span>folio</span>
              <PanelRight size={15} aria-hidden="true" />
            </div>
            <div className={styles.desktopWorkspace}>
              <div className={styles.project}>
                <div className={styles.projectCrumb}>
                  <Folder size={14} />
                  Your research
                </div>
                <h3>
                  A clearer picture.
                  <br />
                  Already in your files.
                </h3>
                <p>Launch research</p>
                <div className={styles.files}>
                  {[
                    "Customer interviews",
                    "Onboarding notes",
                    "Research brief",
                  ].map((name, index) => (
                    <div key={name}>
                      <FileText size={15} />
                      <span>{name}</span>
                      <span>{index === 0 ? "6" : "1"}</span>
                    </div>
                  ))}
                </div>
                <div className={styles.researchNote}>
                  <span>From the interviews</span>
                  <p>“I wasn’t sure what to do first.”</p>
                  <span>Interview 03 · Onboarding</span>
                </div>
              </div>
              <div className={styles.assistant}>
                {connection < 2 ? (
                  <>
                    <div className={styles.found}>
                      <Check size={13} />
                      Found on this computer
                    </div>
                    <h3>
                      Your agents.
                      <br />
                      Ready to work here.
                    </h3>
                    <div
                      className={styles.accounts}
                      role="group"
                      aria-label="Choose an account for the desktop example"
                    >
                      {localAgents.map((item, index) => (
                        <button
                          key={item.id}
                          type="button"
                          onClick={() => setAccount(index)}
                          aria-pressed={account === index}
                          disabled={connection === 1}
                        >
                          <AgentLogo
                            agent={item.id}
                            size={24}
                            surface="light"
                          />
                          <span>
                            <strong>{item.name}</strong>
                            <span>{item.account}</span>
                          </span>
                          <span className={styles.accountCheck}>
                            {account === index && <Check size={11} />}
                          </span>
                        </button>
                      ))}
                    </div>
                    <button
                      className={styles.connectButton}
                      type="button"
                      onClick={() => setConnection(1)}
                      disabled={connection === 1}
                    >
                      <span>
                        {connection === 1
                          ? "Verifying connection…"
                          : `Continue with ${agent.name}`}
                      </span>
                      <ArrowRight size={15} />
                    </button>
                    <p className={styles.accountNote}>
                      Your existing sign-in. Your app’s own workspace.
                    </p>
                  </>
                ) : (
                  <div className={styles.connected}>
                    <div className={styles.connectedHeader}>
                      <AgentLogo agent={agent.id} size={21} surface="light" />
                      <strong>{agent.name} in Folio</strong>
                      <Check size={13} />
                    </div>
                    <p className={styles.request}>
                      Find the biggest onboarding problems.
                    </p>
                    <h3>
                      Six interviews.
                      <br />
                      Three clear patterns.
                    </h3>
                    <ol>
                      <li>
                        <span>01</span>A clearer first step
                      </li>
                      <li>
                        <span>02</span>Less setup to get started
                      </li>
                      <li>
                        <span>03</span>Progress people can see
                      </li>
                    </ol>
                    <div className={styles.reportSaved}>
                      <FileText size={15} />
                      <span>Findings saved to your project</span>
                      <Check size={13} />
                    </div>
                    <button
                      className={styles.switchAccount}
                      type="button"
                      onClick={() => setConnection(0)}
                    >
                      <RotateCcw size={12} />
                      Try another account
                    </button>
                  </div>
                )}
                <span role="status" className="sr-only">
                  {connection === 1
                    ? "Verifying the example account"
                    : connection === 2
                      ? `${agent.name} connected. Research findings saved in the example workspace.`
                      : "Two example accounts found"}
                </span>
              </div>
            </div>
            <div className={styles.windowFooter}>
              <span>
                <Monitor size={12} />
                Local workspace
              </span>
              <span>Illustrative desktop app</span>
            </div>
          </div>
          <div className={styles.desktopCaption}>
            <span>Your interface. Their familiar agent.</span>
            <Link href="/use-cases/desktop-research">
              Explore this integration <ArrowUpRight size={14} />
            </Link>
          </div>
        </div>
      </section>
      <div className={styles.exploreMore}>
        <p>A starting point for what you’ll build next.</p>
        <Link href="/use-cases">
          Explore all product examples <ArrowUpRight size={18} />
        </Link>
      </div>
    </>
  );
}
