"use client";

import { useState } from "react";
import Link from "next/link";
import AgentLogo from "@/components/marketing/AgentLogo";
import SurfaceRoutes from "@/components/landing/SurfaceRoutes";
import styles from "./connection-patterns.module.css";

export default function ConnectionPatterns({
  platform,
  brand = "Your product",
}: {
  platform?: "Desktop" | "Cloud";
  brand?: string;
}) {
  const [selected, setSelected] = useState<"Desktop" | "Cloud">(
    platform ?? "Desktop",
  );
  const [connected, setConnected] = useState(false);
  const [agent, setAgent] = useState<"codex" | "claude">("codex");
  const desktop = (platform ?? selected) === "Desktop";
  return (
    <section
      className={styles.section}
      aria-label="Connect the customer’s agent"
    >
      <div className={styles.heading}>
        <h2>
          {platform ? (
            <>
              Their account.
              <br />
              Inside {brand}.
            </>
          ) : (
            <>
              They bring the intelligence.
              <br />
              You make it part of the product.
            </>
          )}
        </h2>
        <p>
          {desktop
            ? "Your desktop app discovers supported agents on the computer and verifies an existing connection. The customer can use the account they already have."
            : "Give each customer a private cloud workspace. Let them connect a supported account, then keep the conversation, tools, and results inside your application."}
        </p>
      </div>
      {!platform && (
        <div
          className={styles.switcher}
          role="group"
          aria-label="Choose an integration pattern"
        >
          {(["Desktop", "Cloud"] as const).map((value) => (
            <button
              type="button"
              key={value}
              aria-pressed={selected === value}
              onClick={() => {
                setSelected(value);
                setConnected(false);
              }}
            >
              {value} app
            </button>
          ))}
        </div>
      )}
      <SurfaceRoutes kind="exampleConnection" className={styles.flow}>
        <div className={styles.connection} data-sprue="account-source">
          <span className={styles.label}>
            {desktop
              ? "On the customer’s computer"
              : "In the customer’s workspace"}
          </span>
          <h3>
            {desktop
              ? "Already here. Ready to connect."
              : "Bring your ChatGPT account."}
          </h3>
          {desktop ? (
            <div
              className={styles.agents}
              role="group"
              aria-label="Example installed agents"
            >
              {(["codex", "claude"] as const).map((value) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={agent === value}
                  onClick={() => {
                    setAgent(value);
                    setConnected(false);
                  }}
                >
                  <AgentLogo agent={value} size={29} />
                  <span>
                    {value === "codex" ? "Codex" : "Claude Code"}
                    <small>Installed · Native account found</small>
                  </span>
                  <b aria-hidden="true">{agent === value ? "✓" : ""}</b>
                </button>
              ))}
            </div>
          ) : (
            <div className={styles.cloudAccount}>
              <AgentLogo agent="codex" size={42} />
              <p>
                Codex in your app.
                <br />
                <span>Connected to the customer’s eligible ChatGPT plan.</span>
              </p>
            </div>
          )}
          <button
            className={styles.connectButton}
            type="button"
            aria-label={
              desktop
                ? "Preview using an existing agent account"
                : "Preview Continue with ChatGPT connection"
            }
            onClick={() => setConnected(!connected)}
          >
            {connected
              ? "Reset connection preview"
              : desktop
                ? "Use existing account"
                : "Continue with ChatGPT"}
            <span aria-hidden="true">{connected ? "↺" : "↗"}</span>
          </button>
        </div>
        <div className={styles.product} data-sprue="account-product">
          <div className={styles.productHeader}>
            <strong>{brand}</strong>
            <span>
              {connected ? "Account connected" : "Your customer experience"}
            </span>
          </div>
          <div className={styles.productRequest}>
            “Turn this project into something I can use.”
          </div>
          <h3>
            The agent works
            <br />
            through your app.
          </h3>
          <p>
            Reads your product’s context. Uses your tools. Creates and updates
            things your customers can see.
          </p>
          <div className={styles.connectionState} role="status">
            {connected
              ? `${desktop && agent === "claude" ? "Claude Code" : "Codex"} connected in this example. The customer can start working in ${brand === "Your product" ? "your product" : brand}.`
              : "Your interface → Mindwire → their agent"}
          </div>
        </div>
      </SurfaceRoutes>
      <div className={styles.notes}>
        <p>
          Illustrated account flow; no sign-in is performed here.{" "}
          {desktop
            ? "Discovery reuses supported native connections, with a sign-in fallback when needed."
            : "This connects the agent’s account. Your application’s own sign-in stays separate."}{" "}
          Provider eligibility and usage limits still apply; cloud hosting is
          separate.
        </p>
        <Link href="/docs/guides/authentication">
          Connection guide <span aria-hidden="true">↗</span>
        </Link>
      </div>
    </section>
  );
}
