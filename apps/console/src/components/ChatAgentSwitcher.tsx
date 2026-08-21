// The chat's OWN target switcher, living in the chat header. It picks which adapter the *conversation*
// runs against and writes the chat-scoped `chatAgentId` — deliberately UNLINKED from the sidebar's
// AgentSwitcher (which writes `activeAgentId`, the config scope). Configuring one adapter never re-points
// a live chat on another, and vice versa. The runtime (daemon) is session-global and already shown in the
// top-nav selector, so the agent is the ONLY axis surfaced here — no read-only runtime echo (that was
// redundant with the top nav). Degrades to a disabled label before a daemon is ready or with no agents.
import { useApp } from "@/lib/app-context";
import { cn } from "@/lib/utils";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { AgentIcon } from "@/components/AgentIcon";

export function ChatAgentSwitcher() {
  const { activeDaemon, catalog, chatAgentId, setChatAgentId } = useApp();

  const agents = catalog?.agents ?? [];
  const ready = activeDaemon?.state === "ready";
  // An explicit pick wins, else the daemon's baked-in default; the trigger renders its own label from
  // context so it stays truthful even before an agent is pinned.
  const value = chatAgentId ?? activeDaemon?.agent ?? "";
  const label = value || "agent";

  return (
    <Select
      value={value}
      onValueChange={(v) => setChatAgentId(v)}
      disabled={!ready || agents.length === 0}
    >
      <SelectTrigger
        aria-label="Chat agent"
        className={cn(
          "h-9 w-auto gap-2 border border-border bg-transparent px-2.5 text-xs shadow-none",
          "hover:bg-accent focus-visible:ring-0 disabled:opacity-100",
          "[&>svg]:size-3.5 [&>svg]:opacity-45",
        )}
      >
        <span className="flex min-w-0 items-center gap-2">
          <AgentIcon agentId={value} className="size-3.5 shrink-0" />
          <span className="truncate font-mono text-foreground">{label}</span>
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
