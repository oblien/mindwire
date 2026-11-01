"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import AgentLogo from "@/components/marketing/AgentLogo";
import Image from "next/image";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  FileText,
  Monitor,
  RotateCcw,
} from "./InterfaceIcon";
import styles from "./product-possibilities.module.css";

const accounts = [
  { id: "codex", name: "Codex", plan: "Your ChatGPT subscription" },
  { id: "claude", name: "Claude Code", plan: "Your Claude subscription" },
] as const;

export default function ProductPossibilities() {
  const [selected, setSelected] = useState(0);
  const [connection, setConnection] = useState(0);
  const agent = accounts[selected];
  const result = useRef<HTMLHeadingElement>(null);
  const connect = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (connection !== 1) return;
    const timer = window.setTimeout(() => setConnection(2), 1100);
    return () => window.clearTimeout(timer);
  }, [connection]);

  useEffect(() => {
    if (connection === 2) result.current?.focus({ preventScroll: true });
  }, [connection]);

  return (
    <section
      className={styles.desktop}
      id="desktop"
      aria-labelledby="desktop-integration-title"
    >
      <Image
        src="/images/design-lab/materials/desktop-fluted-glass-v1.webp"
        alt=""
        fill
        sizes="100vw"
        className={styles.material}
      />
      <div className={styles.layout}>
        <div className={styles.copy}>
          <h2 id="desktop-integration-title">
            Their agents.
            <br />
            Your desktop app.
          </h2>
          <p className={styles.lead}>
            The subscription is already there.
            <br />
            So is the sign-in.
          </p>
          <p>
            MindWire discovers supported Codex and Claude accounts on the
            computer. Your desktop experience can use the connection they
            already have.
          </p>
          <Link href="/docs/guides/authentication#reuse-a-computers-native-connection">
            Build with native sign-in <ArrowUpRight size={18} />
          </Link>
          <div className={styles.nativeNote}>
            <span>
              <Check size={14} />
            </span>
            No API key to paste. No second subscription.
          </div>
        </div>

        <div className={styles.appScene}>
          <div className={styles.app} data-desktop-app>
            <div className={styles.windowBar}>
              <span className={styles.windowDots} aria-hidden="true">
                <i />
                <i />
                <i />
              </span>
              <span>folio</span>
              <span className={styles.windowRight}>Your workspace</span>
            </div>
            <div className={styles.appBody}>
              {connection < 2 ? (
                <div className={styles.welcome}>
                  <span className={styles.folioMark} aria-hidden="true">
                    f.
                  </span>
                  <h3>
                    Make yourself
                    <br />
                    at home.
                  </h3>
                  <p>Your agents are already here.</p>
                  <div
                    className={styles.accounts}
                    role="group"
                    aria-label="Choose an account for the desktop example"
                  >
                    {accounts.map((account, index) => (
                      <button
                        type="button"
                        key={account.id}
                        aria-pressed={selected === index}
                        aria-disabled={connection === 1}
                        onClick={() => {
                          if (connection !== 1) setSelected(index);
                        }}
                      >
                        <AgentLogo
                          agent={account.id}
                          size={29}
                          surface="light"
                        />
                        <span>
                          <strong>{account.name}</strong>
                          <span>{account.plan}</span>
                        </span>
                        <span className={styles.accountCheck}>
                          {selected === index && <Check size={13} />}
                        </span>
                      </button>
                    ))}
                  </div>
                  <button
                    ref={connect}
                    className={styles.continue}
                    type="button"
                    aria-disabled={connection === 1}
                    onClick={() => {
                      if (connection === 0) setConnection(1);
                    }}
                  >
                    <span>
                      {connection === 1
                        ? "Connecting your agent…"
                        : `Continue with ${agent.name}`}
                    </span>
                    <ArrowRight size={17} />
                  </button>
                  <p className={styles.localNote}>
                    Found on this computer. Ready for your app.
                  </p>
                </div>
              ) : (
                <div className={styles.connected}>
                  <div className={styles.connectedAgent}>
                    <AgentLogo agent={agent.id} size={27} surface="light" />
                    <span>{agent.name} in Folio</span>
                    <Check size={13} />
                  </div>
                  <p className={styles.request}>
                    What’s getting in the way of onboarding?
                  </p>
                  <h3 ref={result} tabIndex={-1}>
                    Six interviews.
                    <br />A clearer picture.
                  </h3>
                  <p>Three recurring themes from the notes in your project.</p>
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
                  <div className={styles.saved}>
                    <FileText size={17} />
                    <span>Findings saved in your workspace</span>
                    <Check size={13} />
                  </div>
                  <button
                    type="button"
                    className={styles.restart}
                    onClick={() => {
                      setConnection(0);
                      requestAnimationFrame(() =>
                        connect.current?.focus({ preventScroll: true }),
                      );
                    }}
                  >
                    <RotateCcw size={13} /> Try another account
                  </button>
                </div>
              )}
              <span role="status" className="sr-only">
                {connection === 1
                  ? "Verifying the example account"
                  : connection === 2
                    ? `${agent.name} connected in the example desktop app.`
                    : "Two example accounts found on this computer"}
              </span>
            </div>
            <div className={styles.appFooter}>
              <Monitor size={12} />
              <span>Interactive desktop example</span>
              <span>Local connection</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
