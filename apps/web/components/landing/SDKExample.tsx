"use client";

import { useId, useState } from "react";
import { ArrowRight, Check, Code2 } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import CopyButton from "./CopyButton";
import { navigateTabs } from "./tabs";
import styles from "./product.module.css";

const agents = [{ id: "codex", name: "Codex", engine: "codex" }, { id: "claude", name: "Claude Code", engine: "claude-code" }, { id: "opencode", name: "OpenCode", engine: "opencode" }] as const;
type Language = "TypeScript" | "Go" | "REST";

export function sdkExample(language: Language, agent: string) {
  if (language === "Go") return `package main

import (
  "context"
  "fmt"
  "log"
  mindwire "github.com/oblien/mindwire/packages/daemon/sdk"
)

func main() {
  ctx := context.Background()
  mw, err := mindwire.New(mindwire.Options{Agent: "${agent}"})
  if err != nil { log.Fatal(err) }
  defer mw.Close()

  run, err := mw.Turn(ctx, mindwire.TurnRequest{
    ChatID: "my-product",
    Message: "Summarize this project and save a report.md file.",
  })
  if err != nil { log.Fatal(err) }
  for event := range run.Stream(ctx) {
    if event.Type == mindwire.EventText {
      fmt.Print(event.Text)
    }
  }
}`;
  if (language === "REST") return `# Connect to your authenticated Mindwire runtime.
# Set MINDWIRE_URL and MINDWIRE_TOKEN on your server.

run_id=$(curl --fail-with-body -sS \\
  "$MINDWIRE_URL/turns?agent=${agent}" \\
  -H "Authorization: Bearer $MINDWIRE_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{
    "chatId": "my-product",
    "message": "Summarize this project and save a report.md file."
  }' | jq -r '.id')

# Follow the same event stream from any HTTP client.
curl --fail-with-body -N \\
  "$MINDWIRE_URL/runs/$run_id/stream" \\
  -H "Authorization: Bearer $MINDWIRE_TOKEN"`;
  return `import { Mindwire } from "mindwire";

const mw = new Mindwire({ agent: "${agent}" });

try {
  const run = await mw.turn({
    chatId: "my-product",
    message: "Summarize this project and save a report.md file.",
    cwd: process.cwd(),
  });

  for await (const event of run) {
    if (event.type === "text") {
      process.stdout.write(event.text ?? "");
    }
    if (event.type === "error") throw new Error(event.error);
  }
} finally {
  await mw.close();
}`;
}

// Text nodes only: these short examples don't need a client-side highlighting library.
function highlight(code: string) {
  return code.split(/("[^"\n]*"|'[^'\n]*'|\/\/[^\n]*|#[^\n]*|\b(?:import|from|const|new|try|await|for|if|throw|finally|package|func|defer|return|range)\b)/g).map((part, i) =>
    <span key={i} className={/^["']/.test(part) ? styles.codeString : /^(\/\/|#)/.test(part) ? styles.codeComment : /^(import|from|const|new|try|await|for|if|throw|finally|package|func|defer|return|range)$/.test(part) ? styles.codeKeyword : undefined}>{part}</span>);
}

export default function SDKExample({ compact = false }: { compact?: boolean }) {
  const [agent, setAgent] = useState<(typeof agents)[number]>(agents[0]);
  const [language, setLanguage] = useState<Language>("TypeScript");
  const id = useId();
  const code = sdkExample(language, agent.engine);
  return <div className={`${styles.sdkExample} ${compact ? styles.sdkCompact : ""}`}>
    <div className={styles.sdkWindowHeader}><span><Code2 size={17} /> Your product’s backend</span><span>mindwire</span></div>
    <div className={styles.agentPicker} role="group" aria-label="Example coding agent">
      {agents.map(item => <button type="button" key={item.id} aria-pressed={item.id === agent.id} onClick={() => setAgent(item)}><AgentLogo agent={item.id} size={17} />{item.name}</button>)}
    </div>
    <div className={styles.codeToolbar}>
      <div role="tablist" aria-label="SDK language" onKeyDown={navigateTabs}>{(["TypeScript", "Go", "REST"] as const).map(item => <button type="button" role="tab" key={item} id={`${id}-${item}`} aria-controls={`${id}-code`} aria-selected={language === item} tabIndex={language === item ? 0 : -1} onClick={() => setLanguage(item)}>{item}</button>)}</div>
      <CopyButton value={code} label="Copy SDK example" />
    </div>
    <div role="tabpanel" id={`${id}-code`} aria-labelledby={`${id}-${language}`} tabIndex={0} className={styles.codePanel}>
      <pre><code>{highlight(code)}</code></pre>
    </div>
    <div className={styles.sdkExampleFooter}><span><Check size={14} />Same API. Your choice of agent.</span><ArrowRight size={15} /></div>
  </div>;
}
