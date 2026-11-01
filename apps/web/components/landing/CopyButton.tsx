"use client";

import { useEffect, useState } from "react";
import { Check, Copy } from "@/components/site/InterfaceIcon";
import styles from "./product.module.css";

export default function CopyButton({ value, label = "Copy code" }: { value: string; label?: string }) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");
  useEffect(() => { setStatus("idle"); }, [value]);
  useEffect(() => {
    if (status === "idle") return;
    const timeout = setTimeout(() => setStatus("idle"), 3000);
    return () => clearTimeout(timeout);
  }, [status]);

  return <span className={styles.copyControl}>
    <button type="button" className={styles.copyButton} aria-label={status === "copied" ? "Copied" : label} onClick={async () => {
      try { await navigator.clipboard.writeText(value); setStatus("copied"); }
      catch { setStatus("failed"); }
    }}>{status === "copied" ? <Check size={16} /> : <Copy size={16} />}</button>
    <span role="status" className={status === "failed" ? styles.copyFailure : styles.srOnly}>{status === "copied" ? "Copied to clipboard." : status === "failed" ? "Select the text to copy it." : ""}</span>
  </span>;
}
