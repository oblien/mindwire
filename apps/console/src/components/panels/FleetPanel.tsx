// The Console — the app's home and watching point. It answers, at a glance: what daemons are deployed
// and up, how many agents have run, and how many tokens / how much cost the fleet has burned this
// session (the overview strip). Below that, one live card per daemon — where it runs and what it's doing
// right now — plus the lifecycle controls (add / activate / spin up / down). Spend is deliberately NOT on
// the daemon cards: it accrues per agent, so it lives on the Agents roster; only the fleet-wide roll-up
// shows here. Usage is polled on a calm cadence so the numbers keep ticking without the user lifting a finger.
import { useEffect, useMemo } from "react";
import { ArrowUpRight, RefreshCw, Server } from "lucide-react";

import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner, ErrorNote, EmptyState, SURFACE_HEADER } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { DaemonBlock } from "@/components/fleet/DaemonBlock";
import { ConsoleStats } from "@/components/fleet/ConsoleStats";
import { AddDaemonDialog } from "@/components/fleet/AddDaemonDialog";
import type { AgentSummary } from "@shared/api";

const OBLIEN_URL = "https://oblien.com/dashboard";

export function FleetPanel() {
  const { fleet, fleetError, fleetLoading, reloadFleet, applyFleet, providers, reloadUsage } =
    useApp();

  const daemons = useMemo(() => fleet?.daemons ?? [], [fleet]);

  // Keep the token/cost figures live without hammering the server — a calm poll while the console is up.
  useEffect(() => {
    const id = setInterval(reloadUsage, 8000);
    return () => clearInterval(id);
  }, [reloadUsage]);

  // Inspect each READY runtime once so its card can show how many of its agents are runnable
  // (installed + authed). Re-inspect only when a runtime appears / disappears or flips readiness — not on
  // a timer; the manual refresh reloads it. Not-ready runtimes can't be probed, so they show no count.
  const depKey = daemons.map((d) => `${d.id}:${d.state}`).join("|");
  const inspections = useAsync<Record<string, AgentSummary[]>>(async () => {
    const entries = await Promise.all(
      daemons
        .filter((d) => d.state === "ready")
        .map(async (d): Promise<[string, AgentSummary[]]> => {
          try {
            return [d.id, (await api.inspectDaemon(d.id)).agents];
          } catch {
            return [d.id, []];
          }
        }),
    );
    return Object.fromEntries(entries);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [depKey]);
  const agentsByDaemon = inspections.data ?? {};

  return (
    <div className="flex h-full flex-col">
      <header className={cn(SURFACE_HEADER, "px-6")}>
        <div className="min-w-0">
          <h1 className="text-sm font-semibold tracking-tight">Console</h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Every runtime in this session — where it runs and what it&apos;s doing. The active one runs
            your turns; per-agent spend lives on Agents.
          </p>
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => {
              reloadFleet();
              reloadUsage();
              inspections.reload();
            }}
            aria-label="Refresh"
          >
            <RefreshCw className={cn("size-4", fleetLoading && "animate-spin")} />
          </Button>
          <AddDaemonDialog providers={providers} onAdded={applyFleet} />
        </div>
      </header>

      <ScrollArea className="flex-1">
        <div className="space-y-8 px-6 py-6">
          {fleetError && <ErrorNote message={fleetError} />}

          <ConsoleStats />

          <section>
            <div className="mb-3 flex items-center justify-between">
              <h2 className="text-xs font-semibold uppercase tracking-wide">Runtimes</h2>
              <span className="text-xs text-muted-foreground">
                {daemons.length} deployed
                {providers && !providers.docker && " · Docker unavailable"}
              </span>
            </div>

            {fleetLoading && daemons.length === 0 ? (
              <Spinner label="Loading fleet…" />
            ) : daemons.length === 0 ? (
              <EmptyState>
                <div className="flex flex-col items-center gap-3">
                  <Server className="size-6 text-muted-foreground" />
                  <p>No runtimes yet. Add one to get started.</p>
                </div>
              </EmptyState>
            ) : (
              <div className="flex flex-col gap-2">
                {daemons.map((d) => (
                  <DaemonBlock key={d.id} daemon={d} agents={agentsByDaemon[d.id]} />
                ))}
              </div>
            )}
          </section>

          <div className="flex items-center justify-between border-t border-border pt-4 text-xs text-muted-foreground">
            <span>Runtimes run wherever you deploy them — remote, Docker, or an Oblien sandbox.</span>
            <a
              href={OBLIEN_URL}
              target="_blank"
              rel="noreferrer"
              className="inline-flex shrink-0 items-center gap-1 transition-colors hover:text-foreground"
            >
              Manage on Oblien
              <ArrowUpRight className="size-3.5" />
            </a>
          </div>
        </div>
      </ScrollArea>
    </div>
  );
}
