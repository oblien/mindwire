"use client";

import { useState } from "react";
import { Check, Copy, Terminal } from "lucide-react";
import styles from "./marketing.module.css";

const command = "npm install -g mindwire\nmindwire connect";

export default function InstallCommand() {
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setFailed(false);
    } catch {
      setCopied(false);
      setFailed(true);
    }
  }

  return (
    <div className={styles.installCommand}>
      <div className={styles.installCommandHeader}>
        <span>
          <Terminal size={15} /> On your computer
        </span>
        <button
          type="button"
          onClick={copy}
          aria-label={
            copied ? "Commands copied" : "Copy install and connect commands"
          }
        >
          {copied ? <Check size={15} /> : <Copy size={15} />}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre>
        <code>
          <span>npm install -g</span> mindwire{"\n"}
          <span>mindwire</span> connect
        </code>
      </pre>
      <span className={styles.srOnly} role="status">
        {copied ? "Install and connect commands copied." : ""}
      </span>
      {failed && (
        <div className={styles.copyError} role="status">
          Select the commands above to copy them.
        </div>
      )}
    </div>
  );
}
