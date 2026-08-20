// The active-agent switcher, which heads the sidebar's "Agent" group. It governs the CONFIG scope only —
// the agent-scoped config surfaces (Capabilities, Settings, Models, …) right below it — by reading/writing
// the global `activeAgentId`. It does NOT touch the chat: the conversation has its own target switcher in
// the chat header (see ChatAgentSwitcher / `chatAgentId`), so configuring one adapter here never re-points
// a live chat on another. The top nav keeps the runtime (daemon) selector; one control per axis, no overlap.
//
// The options are the active daemon's catalog of adapters. The trigger label is read straight from the
// live context (the selected agent id, or the daemon's baked-in default before one is pinned) rather than
// Radix's echoed value, so it stays truthful even when nothing is explicitly selected yet. Before a daemon
// is ready — or on a runtime that lists no agents — it degrades to a static, disabled label.
import { useApp } from "@/lib/app-context";
import { cn } from "@/lib/utils";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { AgentIcon } from "@/components/AgentIcon";

export function AgentSwitcher() {
  const { activeDaemon, catalog, activeAgentId, setActiveAgentId } = useApp();

  const agents = catalog?.agents ?? [];
  const ready = activeDaemon?.state === "ready";
  // Mirror the old ContextSwitcher fallbacks: an explicit pin wins, else the daemon's default agent, else a
  // bare placeholder. The value need not match an item — the trigger renders its own label from context.
  const value = activeAgentId ?? activeDaemon?.agent ?? "";
  const label = value || "agent";

  return (
    <Select
      value={value}
      onValueChange={(v) => setActiveAgentId(v)}
      disabled={!ready || agents.length === 0}
    >
      <SelectTrigger
        aria-label="Active agent"
        className={cn(
          // Sits just under the group's "Configuration" eyebrow: full-width, borderless, quiet — reads as
          // the group's lead row, acts as a control. Icon aligns with the nav items below it (same gap + inset).
          "h-8 w-full gap-2.5 border-0 bg-transparent px-2.5 text-xs shadow-none",
          "hover:bg-ink/3 focus-visible:ring-0 disabled:opacity-100",
          "[&>svg]:size-3.5 [&>svg]:opacity-45",
        )}
      >
        <span className="flex min-w-0 items-center gap-2.5">
          <AgentIcon agentId={value} className="size-4 shrink-0" />
          <span className="truncate font-mono font-medium text-foreground">{label}</span>
        </span>
      </SelectTrigger>
      <SelectContent align="start">
        {agents.map((a) => (
          <SelectItem key={a.id} value={a.id} className="font-mono">
            <span className="flex items-center gap-2">
              <AgentIcon agentId={a.id} className="size-3.5 shrink-0" />
              <span className="truncate">{a.id}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
