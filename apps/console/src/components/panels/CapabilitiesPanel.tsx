// The Capabilities surface — the selected agent's feature matrix as the daemon reports it. Read-only:
// it's the ground truth every other panel gates against (a flag that's `false` here means the
// corresponding sidebar item never appears).
import { Check, Minus } from "lucide-react";

import { useApp } from "@/lib/app-context";
import { cn } from "@/lib/utils";
import { Panel, Section, Spinner, ErrorNote, DetailRow } from "@/components/common/Panel";
import { Badge } from "@/components/ui/badge";
import type { Capabilities } from "@shared/api";

// Boolean feature flags, grouped for display. Order is presentation-only.
const FLAG_GROUPS: { title: string; keys: (keyof Capabilities)[] }[] = [
  { title: "Turn control", keys: ["cancel", "interrupt", "respond", "input", "resume", "persistent"] },
  { title: "Live control", keys: ["setModel", "setPermissionMode", "compactNow", "resolve"] },
  { title: "Models & I/O", keys: ["models", "imageInput", "toolEvents", "customProviders"] },
  {
    title: "Turn options",
    keys: ["systemPrompt", "appendSystemPrompt", "mcpServers", "subagents", "claudeSettings"],
  },
  { title: "Persistent surfaces", keys: ["memory", "promptTemplates", "subagentDefs", "mcpConfig"] },
];

const LABELS: Partial<Record<keyof Capabilities, string>> = {
  cancel: "Cancel",
  interrupt: "Interrupt",
  respond: "Respond",
  input: "Follow-up input",
  resume: "Resume",
  persistent: "Persistent session",
  setModel: "Switch model",
  setPermissionMode: "Permission mode",
  compactNow: "Compact now",
  resolve: "Global resolve",
  models: "Model catalog",
  imageInput: "Vision input",
  toolEvents: "Tool events",
  customProviders: "Custom providers",
  systemPrompt: "System prompt",
  appendSystemPrompt: "Append prompt",
  mcpServers: "Per-turn MCP",
  subagents: "Per-turn subagents",
  claudeSettings: "Claude settings",
  memory: "Memory file",
  promptTemplates: "Prompt templates",
  subagentDefs: "Subagent defs",
  mcpConfig: "MCP config",
};

function Flag({ on, label }: { on: boolean; label: string }) {
  return (
    <div
      className={cn(
        "flex items-center gap-2 border px-3 py-2 text-xs",
        on ? "border-border" : "border-dashed border-border text-muted-foreground",
      )}
    >
      {on ? (
        <Check className="size-3.5 text-foreground" />
      ) : (
        <Minus className="size-3.5 text-muted-foreground" />
      )}
      {label}
    </div>
  );
}

export function CapabilitiesPanel() {
  const { agent, agentLoading, agentError } = useApp();

  if (agentLoading) return <Panel title="Capabilities"><Spinner /></Panel>;
  if (agentError || !agent)
    return (
      <Panel title="Capabilities">
        <ErrorNote message={agentError ?? "No agent info available."} />
      </Panel>
    );

  const c = agent.capabilities;

  return (
    <Panel title="Capabilities" description={`${agent.name} · v${agent.version}`}>
      <Section title="Agent">
        <div className="border border-border px-4">
          <DetailRow label="Type">
            <span className="font-mono">{agent.agentType}</span>
          </DetailRow>
          <DetailRow label="Installed version">
            {agent.installedVersion || <span className="text-muted-foreground">unknown</span>}
          </DetailRow>
          <DetailRow label="Configured">
            <Badge variant={agent.configured ? "default" : "secondary"}>
              {agent.configured ? "yes" : "no"}
            </Badge>
          </DetailRow>
          <DetailRow label="Protocol">
            <span className="font-mono">{c.protocol}</span>
          </DetailRow>
          <DetailRow label="Output">
            <span className="font-mono">{c.output}</span>
          </DetailRow>
          <DetailRow label="History">
            <span className="font-mono">{c.history}</span>
          </DetailRow>
          <DetailRow label="Sessions">
            <span className="font-mono">{c.sessions}</span>
          </DetailRow>
          <DetailRow label="Config path">
            <span className="break-all font-mono text-xs">{agent.configPath || "—"}</span>
          </DetailRow>
        </div>
      </Section>

      {FLAG_GROUPS.map((g) => (
        <Section key={g.title} title={g.title}>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {g.keys.map((k) => (
              <Flag key={k} on={Boolean(c[k])} label={LABELS[k] ?? k} />
            ))}
          </div>
        </Section>
      ))}
    </Panel>
  );
}
