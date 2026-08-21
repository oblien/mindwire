// A single daemon's own page — opened from the Console block, not expanded inline. It gathers
// everything about one daemon in a full surface: where it runs, its live activity (agent types +
// running chats), its per-agent token/cost accounting, provisioning logs, and the full lifecycle
// controls (activate / spin up / stop / duplicate / remove). A back link returns to the Console; if the
// daemon is removed (here or elsewhere) the page falls back to a not-found state.
import { useCallback, useEffect, useState, type ComponentType, type ReactNode } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  ArrowUpRight,
  Play,
  Power,
  Copy,
  Trash2,
  RefreshCw,
  Loader2,
  ShieldCheck,
  ShieldAlert,
  Activity,
  Bot,
  Cpu,
  ServerOff,
} from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { useProvisionStream } from "@/lib/useProvisionStream";
import { cn } from "@/lib/utils";
import { compact, formatBytes, formatUptime, totalTokens, usd } from "@/lib/format";
import { AgentIcon } from "@/components/AgentIcon";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ErrorNote, SURFACE_HEADER } from "@/components/common/Panel";
import { toast } from "@/components/ui/sonner";
import { PROVIDER_ICON, STATE_TONE, STATE_LABEL } from "@/components/fleet/daemon-state";
import type {
  AgentSummary,
  AgentUsage,
  DaemonInspection,
  DaemonState,
  DaemonView,
  EnsureEvent,
  SetupStatus,
  Stats,
} from "@shared/api";

// The Console owns this presentation catalog so an installable harness is never hidden merely because
// its CLI is absent. The daemon inspection enriches these rows with live version/auth state.
const KNOWN_AGENTS: AgentSummary[] = [
  { id: "claude-code", name: "Claude Code", tagline: "Anthropic's coding agent", configured: false, authConfigured: false },
  { id: "codex", name: "Codex", tagline: "OpenAI's coding agent", configured: false, authConfigured: false },
  { id: "grok", name: "Grok Build", tagline: "xAI's coding agent", configured: false, authConfigured: false },
  { id: "opencode", name: "opencode", tagline: "Open-source coding agent", configured: false, authConfigured: false },
];

export function DaemonPage() {
  const navigate = useNavigate();
  const { id = "" } = useParams<{ id: string }>();
  const onBack = useCallback(() => navigate("/"), [navigate]);

  const {
    fleet,
    applyFleet,
    reloadFleet,
    activeDaemon,
    activeAgentId,
    reloadAgent,
    reloadHealth,
    reloadCatalog,
    usage,
  } = useApp();

  const daemon = fleet?.daemons.find((d) => d.id === id) ?? null;
  const isActive = activeDaemon?.id === id;
  const canRemove = true;

  // After a spin-up settles, refresh the fleet and — if this is the active daemon — its live view.
  const onProvisioned = useCallback(() => {
    reloadFleet();
    if (isActive) {
      reloadAgent();
      reloadHealth();
      reloadCatalog();
    }
  }, [reloadFleet, isActive, reloadAgent, reloadHealth, reloadCatalog]);

  const provision = useProvisionStream(onProvisioned);

  // Live inspection: adapter types hosted + chats running. Only meaningful once ready.
  const insp = useAsync<DaemonInspection | null>(
    () => (daemon?.state === "ready" ? api.inspectDaemon(id) : Promise.resolve(null)),
    [id, daemon?.state],
  );

  // Installation is a daemon-owned job, not a page-local button click. Read its status again whenever
  // this runtime page mounts (for example after navigating back from an agent page), and poll only
  // while a job is active. That keeps the roster truthful and prevents a second install request from
  // racing an install already in progress.
  const [setupByAgent, setSetupByAgent] = useState<Partial<Record<string, SetupStatus>>>({});
  useEffect(() => {
    if (daemon?.state !== "ready") {
      setSetupByAgent({});
      return;
    }
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      const entries = await Promise.all(
        KNOWN_AGENTS.map(async ({ id: agentId }) => {
          try {
            return [agentId, await api.daemonAgentSetupStatus(id, agentId)] as const;
          } catch {
            // A daemon that does not expose setup for an adapter still has its inspection row. Do not
            // let one unsupported status endpoint hide the rest of the roster.
            return [agentId, undefined] as const;
          }
        }),
      );
      if (cancelled) return;
      const next = Object.fromEntries(entries.filter(([, status]) => status)) as Partial<Record<string, SetupStatus>>;
      setSetupByAgent(next);
      if (Object.values(next).some((status) => status?.running)) {
        timer = setTimeout(() => void poll(), 900);
      } else if (Object.values(next).some((status) => status?.started && status.ok)) {
        // The install just completed (or completed while this page was away). Refresh the daemon's
        // inspection so the installed version replaces this transient status.
        insp.reload();
      }
    };
    void poll();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [daemon?.state, id, insp.reload]);

  // Resource usage auto-loads the first time the daemon page opens on a ready daemon (client-side, keyed
  // on the daemon + its state) and then only re-reads when you hit refresh — still never polled, so the
  // daemon does the work when someone's looking, not on a timer. A failure surfaces inline (e.g. a daemon
  // that predates the /stats endpoint) without blocking the rest of the page.
  const statsQ = useAsync<Stats | null>(
    () => (daemon?.state === "ready" ? api.daemonStats(id) : Promise.resolve(null)),
    [id, daemon?.state],
  );

  // Per-agent spend for this runtime — an agent-level fact that rides on each roster row below.
  const usageFor = (agentId: string): AgentUsage | undefined =>
    usage?.agents.find((u) => u.daemonId === id && u.agent === agentId);

  async function installAgent(agentId: string) {
    try {
      await api.daemonAgentSetup(id, agentId);
      navigate(`/daemons/${id}/agents/${agentId}`);
      toast.success(`Installing ${KNOWN_AGENTS.find((a) => a.id === agentId)?.name ?? agentId}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start installation");
    }
  }

  // Hooks are all above this guard so the not-found fallback never changes hook order.
  if (!daemon) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader onBack={onBack} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
          <ServerOff className="size-6" />
          <p>This runtime is no longer in the fleet.</p>
          <Button variant="outline" size="sm" onClick={onBack}>
            Back to Console
          </Button>
        </div>
      </div>
    );
  }

  const state: DaemonState = provision.status === "provisioning" ? "provisioning" : daemon.state;
  // A persisted `provisioning` state can outlive this browser (or a Console restart). Only the stream
  // owned by THIS page is a local busy lock; otherwise offer a safe resume check and let the server's
  // per-runtime lock reject an actually concurrent provision.
  const busy = provision.status === "provisioning";
  // remote/local are always on — nothing to spin up or tear down. ssh/docker/oblien provision.
  const provisionable = daemon.provider !== "remote" && daemon.provider !== "local";
  const Icon = PROVIDER_ICON[daemon.provider];

  async function activate() {
    try {
      applyFleet(await api.activateDaemon(id));
      toast.success(`Switched to ${daemon!.label}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not activate");
    }
  }
  async function duplicate() {
    try {
      applyFleet(await api.duplicateDaemon(id));
      toast.success("Runtime duplicated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not duplicate");
    }
  }
  async function remove() {
    try {
      applyFleet(await api.removeDaemon(id));
      toast.success("Runtime removed");
      onBack();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not remove");
    }
  }
  async function stop() {
    try {
      applyFleet(await api.daemonDown(id));
      provision.reset();
      toast.success("Runtime stopped");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not stop");
    }
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader onBack={onBack}>
        <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-ink/5">
          <Icon className="size-4" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h1 className="truncate text-sm font-semibold tracking-tight">{daemon.label}</h1>
            <Badge variant="outline">{daemon.provider}</Badge>
            {isActive && <Badge>active</Badge>}
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">{daemon.location.summary}</p>
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <span className={cn("size-2 rounded-full", STATE_TONE[state])} />
          <span className="text-xs text-muted-foreground">{STATE_LABEL[state]}</span>
        </div>
      </PageHeader>

      <ScrollArea className="flex-1">
        <div className="space-y-4 px-6 py-6">
          {/* lifecycle controls */}
          <div className="flex flex-wrap items-center gap-2">
            {!isActive && (
              <Button size="sm" onClick={() => void activate()} disabled={busy}>
                Set active
              </Button>
            )}
            {provisionable && state !== "ready" && (
              <Button size="sm" variant="outline" onClick={() => void provision.start(id)} disabled={busy}>
                {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                {busy ? "Provisioning" : state === "error" ? "Retry" : state === "provisioning" ? "Check & resume" : "Spin up"}
              </Button>
            )}
            {provisionable && state === "ready" && (
              <Button size="sm" variant="outline" onClick={() => void stop()} disabled={busy}>
                <Power className="size-3.5" />
                Stop
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => void duplicate()} disabled={busy}>
              <Copy className="size-3.5" />
              Duplicate
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => void remove()}
              disabled={busy || !canRemove}
              title={canRemove ? undefined : "The fleet needs at least one runtime"}
              className="text-muted-foreground hover:text-destructive"
            >
              <Trash2 className="size-3.5" />
              Remove
            </Button>
          </div>

          {busy && <ProvisionProgress logs={provision.logs} />}

          {state === "error" && (daemon.message || provision.error) && (
            <ErrorNote message={provision.error ?? daemon.message ?? "Provisioning failed."} />
          )}

          <div className="grid gap-4 lg:grid-cols-2">
            {/* where it runs */}
            <Section title="Location">
              <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-xs sm:grid-cols-3">
                <Detail label="Default agent" value={daemon.agent} mono />
                <LocationDetails daemon={daemon} />
              </dl>
            </Section>

            {/* live activity */}
            {daemon.state === "ready" && (
              <Section
                title="Activity"
                action={
                  <button
                    type="button"
                    onClick={insp.reload}
                    className="inline-flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
                    aria-label="Refresh activity"
                  >
                    <RefreshCw className={cn("size-3", insp.loading && "animate-spin")} />
                  </button>
                }
              >
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Activity className="size-3.5" />
                  {insp.loading ? (
                    <span>Inspecting…</span>
                  ) : insp.data && insp.data.online ? (
                    <span>
                      <span className="text-foreground">{insp.data.agentCount}</span> agent
                      {insp.data.agentCount === 1 ? "" : "s"} ·{" "}
                      {insp.data.runningChats.length > 0 ? (
                        <span className="text-foreground">
                          {insp.data.runningChats.length} running
                        </span>
                      ) : (
                        "idle"
                      )}
                    </span>
                  ) : (
                    <span>{insp.data?.error ?? "Unreachable"}</span>
                  )}
                </div>

                {insp.data?.runningChats && insp.data.runningChats.length > 0 && (
                  <div className="mt-3 space-y-1">
                    {insp.data.runningChats.map((ch) => (
                      <div key={ch.chatId} className="flex items-center gap-2 text-xs">
                        <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
                        <span className="truncate">{ch.title || ch.chatId}</span>
                        {ch.agent && (
                          <span className="font-mono text-muted-foreground">{ch.agent}</span>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </Section>
            )}

            {/* deployed agents — the roster lives HERE now (there's no separate fleet-wide Agents page).
                Installed-only ("the ones deployed, not just authed"); each row opens that agent and carries
                its own spend. Choosing what the chat runs against stays a deliberate act on the agent page. */}
            {daemon.state === "ready" && (
              <Section
                title="Agents"
                icon={Bot}
                className="lg:col-span-2"
                action={
                  <button
                    type="button"
                    onClick={insp.reload}
                    className="inline-flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
                    aria-label="Refresh agents"
                  >
                    <RefreshCw className={cn("size-3", insp.loading && "animate-spin")} />
                  </button>
                }
              >
                <AgentRoster
                  inspection={insp.data}
                  loading={insp.loading}
                  setupByAgent={setupByAgent}
                  usageFor={usageFor}
                  isInUse={(agentId, isDefault) =>
                    isActive && (activeAgentId ? activeAgentId === agentId : isDefault)
                  }
                  onOpen={(agentId) => navigate(`/daemons/${id}/agents/${agentId}`)}
                  onInstall={(agentId) => void installAgent(agentId)}
                />
              </Section>
            )}

            {/* resource usage — auto-loaded once the page opens, then refresh-only (never polled) */}
            {daemon.state === "ready" && (
              <Section
                title="Resources"
                icon={Cpu}
                className="lg:col-span-2"
                action={
                  <button
                    type="button"
                    onClick={statsQ.reload}
                    className="inline-flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
                    aria-label="Refresh resources"
                  >
                    <RefreshCw className={cn("size-3", statsQ.loading && "animate-spin")} />
                  </button>
                }
              >
                {statsQ.loading && !statsQ.data ? (
                  <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                    <Loader2 className="size-3.5 animate-spin" />
                    Reading…
                  </div>
                ) : statsQ.error ? (
                  <p className="text-xs text-muted-foreground">{statsQ.error}</p>
                ) : statsQ.data ? (
                  <div className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
                    <ResMetric label="Memory in use" value={formatBytes(statsQ.data.memAllocBytes)} />
                    <ResMetric label="Reserved (OS)" value={formatBytes(statsQ.data.memSysBytes)} />
                    <ResMetric label="CPU cores" value={String(statsQ.data.numCpu)} />
                    <ResMetric label="Goroutines" value={String(statsQ.data.numGoroutine)} />
                    <ResMetric label="GC cycles" value={String(statsQ.data.numGc)} />
                    <ResMetric label="Uptime" value={formatUptime(statsQ.data.uptimeSeconds)} />
                    <ResMetric label="Platform" value={`${statsQ.data.os}/${statsQ.data.arch}`} mono />
                    <ResMetric label="Runtime" value={statsQ.data.goVersion} mono />
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">No resource data.</p>
                )}
                <p className="mt-3 text-[10px] text-muted-foreground">
                  The process itself — read live from its Go runtime.
                </p>
              </Section>
            )}

            {/* per-agent token accounting — each row opens that agent's page */}
          </div>

          {/* provisioning logs */}
          {(provision.logs.length > 0 || provision.status === "provisioning") && (
            <Section title="Provisioning">
              <div className="max-h-64 space-y-1 overflow-auto font-mono text-xs">
                {provision.logs.map((ev, i) => (
                  <div key={i} className="flex gap-2">
                    <span className="w-20 shrink-0 text-muted-foreground">{ev.phase}</span>
                    <span className="min-w-0 break-words">{ev.message}</span>
                  </div>
                ))}
                {provision.logs.length === 0 && (
                  <span className="text-muted-foreground">Starting…</span>
                )}
              </div>
            </Section>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

const PROVISION_PHASES: EnsureEvent["phase"][] = [
  "connect",
  "install",
  "provision",
  "pull",
  "probe",
  "download",
  "upload",
  "launch",
  "ready",
];

/** Real provisioning phases, not a timer-based or fabricated percentage. */
function ProvisionProgress({ logs }: { logs: EnsureEvent[] }) {
  const latest = logs.at(-1);
  const index = Math.max(0, latest ? PROVISION_PHASES.indexOf(latest.phase) : 0);

  return (
    <div className="space-y-2 border border-amber-500/25 bg-amber-500/5 px-3 py-2.5" role="status" aria-live="polite">
      <div className="flex items-center justify-between gap-3 text-xs">
        <span className="flex items-center gap-2 font-medium">
          <Loader2 className="size-3.5 animate-spin" /> Provisioning runtime
        </span>
        <span className="font-mono text-muted-foreground">{latest?.phase ?? "starting"}</span>
      </div>
      <div className="grid grid-cols-9 gap-1" aria-label="Provisioning phase progress">
        {PROVISION_PHASES.map((phase, phaseIndex) => (
          <span
            key={phase}
            title={phase}
            className={cn(
              "h-1.5 transition-colors",
              phaseIndex <= index ? "bg-amber-500" : "bg-ink/10",
            )}
          />
        ))}
      </div>
      <p className="text-xs text-muted-foreground">{latest?.message ?? "Preparing the runtime…"}</p>
    </div>
  );
}

function PageHeader({ onBack, children }: { onBack: () => void; children?: ReactNode }) {
  return (
    <header className={cn(SURFACE_HEADER, "px-6")}>
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="size-3.5" />
        Console
      </button>
      {children && <span className="text-muted-foreground/40">/</span>}
      {children}
    </header>
  );
}

function Section({
  title,
  icon: Icon,
  action,
  className,
  children,
}: {
  title: string;
  icon?: ComponentType<{ className?: string }>;
  action?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={cn("border border-border bg-card p-4", className)}>
      <div className="mb-3 flex items-center gap-2">
        {Icon && <Icon className="size-3.5 text-muted-foreground" />}
        <h2 className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {title}
        </h2>
        {action && <span className="ml-auto">{action}</span>}
      </div>
      {children}
    </section>
  );
}

function ResMetric({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
      <span className={cn("truncate text-sm tabular-nums", mono && "font-mono text-xs")}>
        {value}
      </span>
    </div>
  );
}

function Detail({ label, value, mono }: { label: string; value?: string | number; mono?: boolean }) {
  if (value === undefined || value === "") return null;
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("truncate", mono && "font-mono")}>{value}</dd>
    </div>
  );
}

function LocationDetails({ daemon }: { daemon: DaemonView }) {
  const l = daemon.location;
  switch (l.provider) {
    case "remote":
      return (
        <>
          <Detail label="URL" value={l.url} mono />
          <Detail label="Auth" value={l.secured ? "bearer token" : "none"} />
        </>
      );
    case "local":
      return (
        <>
          <Detail label="Location" value="This host" />
          <Detail label="Working dir" value={l.cwd ?? "server default"} mono />
        </>
      );
    case "ssh":
      return (
        <>
          <Detail
            label="Host"
            value={`${l.sshUser ? `${l.sshUser}@` : ""}${l.host}${l.sshPort ? `:${l.sshPort}` : ""}`}
            mono
          />
          <Detail label="Auth" value={l.sshAuth} />
          {l.containerized && <Detail label="Runs in" value="Docker container" />}
        </>
      );
    case "docker":
      return (
        <>
          <Detail
            label={l.attached ? "Container" : "Image"}
            value={l.attached ? l.containerId : l.image}
            mono
          />
          <Detail label="Engine" value={l.engine} mono />
          {l.hostPort !== undefined && <Detail label="Host port" value={l.hostPort} mono />}
        </>
      );
    case "oblien":
      return (
        <>
          <Detail label="Image" value={l.image} mono />
          <Detail label="Mode" value={l.mode} />
          {l.workspaceId && <Detail label="Workspace" value={l.workspaceId} mono />}
          {(l.cpus || l.memoryMb) && (
            <Detail
              label="Size"
              value={[l.cpus && `${l.cpus} vCPU`, l.memoryMb && `${l.memoryMb} MB`]
                .filter(Boolean)
                .join(" · ")}
            />
          )}
        </>
      );
  }
}

// ---- deployed-agents roster (lives on the runtime page) --------------------

/** Every supported harness, merged with the runtime's live installation/auth inspection. */
function AgentRoster({
  inspection,
  loading,
  setupByAgent,
  usageFor,
  isInUse,
  onOpen,
  onInstall,
}: {
  inspection: DaemonInspection | null;
  loading: boolean;
  setupByAgent: Partial<Record<string, SetupStatus>>;
  usageFor: (agentId: string) => AgentUsage | undefined;
  isInUse: (agentId: string, isDefault: boolean) => boolean;
  onOpen: (agentId: string) => void;
  onInstall: (agentId: string) => void;
}) {
  if (loading && !inspection) {
    return (
      <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        Inspecting…
      </div>
    );
  }
  if (!inspection || !inspection.online) {
    return (
      <p className="text-xs text-muted-foreground">{inspection?.error ?? "Runtime unreachable."}</p>
    );
  }

  const reported = inspection.agents ?? [];
  const byID = new Map(reported.map((agent) => [agent.id, agent]));
  const agents = [
    ...KNOWN_AGENTS.map((agent) => byID.get(agent.id) ?? agent),
    ...reported.filter((agent) => !KNOWN_AGENTS.some((known) => known.id === agent.id)),
  ];
  const defaultAgent = inspection.defaultAgent;

  return (
    <div className="-mx-4 -mb-4 divide-y divide-border border-t border-border">
      {agents.map((agent) => (
        <AgentRow
          key={agent.id}
          agent={agent}
          setup={setupByAgent[agent.id]}
          isDefault={defaultAgent === agent.id}
          active={isInUse(agent.id, defaultAgent === agent.id)}
          usage={usageFor(agent.id)}
          onOpen={() => onOpen(agent.id)}
          onInstall={() => onInstall(agent.id)}
        />
      ))}
    </div>
  );
}

/** One deployed agent — the whole row opens its overview (no context switch, no second action). */
function AgentRow({
  agent,
  setup,
  isDefault,
  active,
  usage,
  onOpen,
  onInstall,
}: {
  agent: AgentSummary;
  setup?: SetupStatus;
  isDefault: boolean;
  active: boolean;
  usage?: AgentUsage;
  onOpen: () => void;
  onInstall: () => void;
}) {
  const tokens = usage ? totalTokens(usage.usage) : 0;
  const installing = setup?.running ?? false;
  const installFailed = !!setup?.started && !setup.running && !setup.ok;
  return (
    <div className={cn("group flex w-full items-center gap-3 px-4 py-3.5 transition-colors hover:bg-ink/2", !agent.installedVersion && "bg-muted/20")}>
      <button
        type="button"
        onClick={onOpen}
        aria-label={`Open ${agent.name} overview`}
        className="flex min-w-0 flex-1 items-center gap-4 text-left"
      >
      <span className="flex size-9 shrink-0 items-center justify-center border border-border">
        <AgentIcon agentId={agent.id} className="size-4" />
      </span>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{agent.name}</span>
          <span className="font-mono text-xs text-muted-foreground">{agent.id}</span>
          {isDefault && <Badge variant="muted">default</Badge>}
          {active && <Badge>in use</Badge>}
          {installing && <Badge variant="muted">installing</Badge>}
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span
            className={cn(
              "inline-flex items-center gap-1",
              agent.authConfigured && "text-foreground/80",
            )}
          >
            {agent.authConfigured ? (
              <ShieldCheck className="size-3.5" />
            ) : (
              <ShieldAlert className="size-3.5" />
            )}
            {agent.authConfigured ? agent.authMethod ?? "authed" : "no auth"}
          </span>
          <span>{agent.configured ? "configured" : "not configured"}</span>
          {agent.installedVersion && <span className="font-mono">v{agent.installedVersion}</span>}
          {installing && <span>{setup?.current ?? "preparing install"}</span>}
        </div>
      </div>

      {/* per-agent spend — tracked separately per adapter */}
      <div className="hidden shrink-0 items-center gap-5 sm:flex">
        <SpendMetric value={compact(tokens)} label="tokens" />
        <SpendMetric value={compact(usage?.turns ?? 0)} label={usage?.turns === 1 ? "turn" : "turns"} />
        <SpendMetric value={usd(usage?.costUsd)} label="cost" />
      </div>

        <ArrowUpRight className="size-4 shrink-0 text-muted-foreground/50 transition-colors group-hover:text-foreground" />
      </button>
      {!agent.installedVersion && (
        <Button size="sm" variant="outline" onClick={onInstall} disabled={installing}>
          {installing && <Loader2 className="size-3.5 animate-spin" />}
          {installing ? "Installing" : installFailed ? "Retry install" : "Install"}
        </Button>
      )}
    </div>
  );
}

function SpendMetric({ value, label }: { value: string; label: string }) {
  return (
    <div className="text-right">
      <div className="tabular-nums font-medium text-foreground">{value}</div>
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>
    </div>
  );
}
