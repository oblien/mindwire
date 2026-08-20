// A daemon in the Console list — a single compact block, not an expanded panel. It shows only what you
// scan at a glance about the *location*: where it runs, which adapter it defaults to, whether it's the
// active one, and its lifecycle state. It links to the daemon's OWN page (`/daemons/:id`) for detail +
// lifecycle controls. Spend is deliberately NOT here: tokens/turns/cost accrue per *agent*, so they live
// on the Agents roster, not on the daemon. No hard outline: the block is a translucent white film that
// brightens on hover, and the active one is marked by a soft accent bar + a slightly stronger film.
import { ChevronRight } from "lucide-react";
import { Link } from "react-router-dom";

import { useApp } from "@/lib/app-context";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { AgentIcon } from "@/components/AgentIcon";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { PROVIDER_ICON, STATE_TONE, STATE_LABEL } from "@/components/fleet/daemon-state";
import type { AgentSummary, DaemonView } from "@shared/api";

/** An agent can run a turn only once its CLI is installed in the runtime AND its auth is configured. */
function isReady(a: AgentSummary): boolean {
  return Boolean(a.installedVersion) && a.authConfigured;
}

/** Why an agent isn't runnable yet — installed is the first gate, auth the second. */
function readinessLabel(a: AgentSummary): string {
  if (!a.installedVersion) return "not installed";
  if (!a.authConfigured) return "needs auth";
  return "ready";
}

export function DaemonBlock({ daemon, agents }: { daemon: DaemonView; agents?: AgentSummary[] }) {
  const { activeDaemon } = useApp();
  const isActive = activeDaemon?.id === daemon.id;
  const Icon = PROVIDER_ICON[daemon.provider];
  const readyCount = agents?.filter(isReady).length ?? 0;

  return (
    <Link
      to={`/daemons/${encodeURIComponent(daemon.id)}`}
      className={cn(
        "group relative flex w-full items-center gap-4 overflow-hidden px-4 py-3.5 text-left transition-colors",
        isActive ? "bg-ink/8 hover:bg-ink/10" : "bg-ink/3 hover:bg-ink/6",
      )}
    >
      {/* soft active accent — a low-opacity bar, not a hard outline */}
      {isActive && <span className="absolute inset-y-0 left-0 w-0.5 bg-ink/40" />}

      <div className="flex size-9 shrink-0 items-center justify-center bg-ink/6">
        <Icon className="size-4" />
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{daemon.label}</span>
          <Badge variant="outline">{daemon.provider}</Badge>
          {isActive && <Badge>active</Badge>}
        </div>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">
          {daemon.location.summary}
          <span className="px-1 text-ink/25">·</span>
          default <span className="font-mono text-foreground/70">{daemon.agent}</span>
        </p>
      </div>

      {/* Agent readiness — how many of this runtime's adapters can actually run a turn (installed + authed).
          Only knowable when the runtime is up and inspected; hidden otherwise. Colored mark = ready, dimmed
          = not. Hover for the per-agent breakdown. */}
      {agents && agents.length > 0 && (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="hidden shrink-0 items-center gap-2 sm:flex">
              <span className="flex items-center gap-1">
                {agents.map((a) => (
                  <AgentIcon
                    key={a.id}
                    agentId={a.id}
                    className={cn("size-3.5", !isReady(a) && "opacity-25 grayscale")}
                  />
                ))}
              </span>
              <span className="tabular-nums text-xs text-muted-foreground">
                {readyCount}/{agents.length} ready
              </span>
            </span>
          </TooltipTrigger>
          <TooltipContent side="top" className="p-2">
            <div className="space-y-1.5">
              {agents.map((a) => (
                <div key={a.id} className="flex items-center gap-2 text-xs">
                  <AgentIcon agentId={a.id} className="size-3.5 shrink-0" />
                  <span className="font-mono">{a.id}</span>
                  <span
                    className={cn(
                      "ml-auto pl-3",
                      isReady(a) ? "text-foreground" : "text-muted-foreground",
                    )}
                  >
                    {readinessLabel(a)}
                  </span>
                </div>
              ))}
            </div>
          </TooltipContent>
        </Tooltip>
      )}

      <div className="flex shrink-0 items-center gap-2">
        <span className={cn("size-2 rounded-full", STATE_TONE[daemon.state])} />
        <span className="hidden text-xs text-muted-foreground md:inline">
          {STATE_LABEL[daemon.state]}
        </span>
      </div>

      <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}
