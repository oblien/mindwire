// One agent's own page — the drill-down the Console/Agents surfaces open into: pick a runtime, see its
// agents, open one, and read everything about *that* adapter on *that* runtime in a single surface. It
// separates the two honest per-agent facts the system actually has — the agent's spend (tokens · turns ·
// cost, broken down) and its identity/auth/capabilities/config — from the runtime-level resources, which
// the daemon reports for the whole process (memory/CPU aren't isolated per agent). A back link returns to
// the runtime's page; a missing daemon or agent falls back to a not-found state.
//
// Data is path-scoped: `api.daemonAgent(id, agentId)` reads this specific (daemon, agent) pair without
// touching the active context, so browsing agents never retargets the chat. Making it the active context
// is an explicit action ("Use this agent"), mirroring the fleet-wide Agents roster.
import { useCallback, useEffect, useState, type ComponentType, type ReactNode } from "react";
import { useNavigate, useParams, Link } from "react-router-dom";
import {
  Activity,
  ArrowLeft,
  Bell,
  Bot,
  Check,
  Cpu,
  Hash,
  KeyRound,
  Minus,
  RefreshCw,
  Loader2,
  ServerOff,
  ShieldAlert,
  ShieldCheck,
} from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { useProcessStream } from "@/lib/useProcessStream";
import { cn } from "@/lib/utils";
import { compact, formatBytes, totalTokens, usd } from "@/lib/format";
import { AgentIcon } from "@/components/AgentIcon";
import { MiniAreaChart } from "@/components/charts/MiniAreaChart";
import { StackedBar, type BarSegment } from "@/components/charts/StackedBar";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ErrorNote, SURFACE_HEADER } from "@/components/common/Panel";
import { toast } from "@/components/ui/sonner";
import type { AgentInfo, Capabilities, NotifyRule, SetupStatus } from "@shared/api";

// Capability flags grouped for display — the same matrix the Capabilities panel shows, rendered inline
// here so the per-agent view is self-contained (it doesn't depend on this agent being the active one).
const FLAG_GROUPS: { title: string; keys: (keyof Capabilities)[] }[] = [
  { title: "Turn control", keys: ["cancel", "interrupt", "respond", "input", "resume", "persistent"] },
  { title: "Live control", keys: ["setModel", "setPermissionMode", "compactNow", "resolve"] },
  { title: "Models & I/O", keys: ["models", "imageInput", "toolEvents", "customProviders"] },
  { title: "Persistent surfaces", keys: ["memory", "promptTemplates", "subagentDefs", "mcpConfig"] },
];

const CAP_LABELS: Partial<Record<keyof Capabilities, string>> = {
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
  memory: "Memory file",
  promptTemplates: "Prompt templates",
  subagentDefs: "Subagent defs",
  mcpConfig: "MCP config",
};

export function AgentPage() {
  const navigate = useNavigate();
  const { id = "", agentId = "" } = useParams<{ id: string; agentId: string }>();

  const { fleet, usage, activeDaemon, activeAgentId, setActiveContext, reloadUsage } = useApp();

  const daemon = fleet?.daemons.find((d) => d.id === id) ?? null;
  const backToRuntime = useCallback(() => navigate(`/daemons/${id}`), [navigate, id]);

  // The agent's own info (capabilities / auth / config) — path-scoped to this (daemon, agent) pair, so
  // it resolves the right adapter regardless of which context is active. Only meaningful once ready.
  const infoQ = useAsync<AgentInfo | null>(
    () => (daemon?.state === "ready" ? api.daemonAgent(id, agentId) : Promise.resolve(null)),
    [id, agentId, daemon?.state],
  );

  // Notification rules are daemon-WIDE and the notify API always targets the *active* runtime — so we
  // only load them when this page is the active runtime, and filter to rules that route THIS agent's
  // events. When browsing another runtime, we point at the Notifications surface instead of showing
  // rules that belong to a different daemon.
  const notifyLive = activeDaemon?.id === id && daemon?.state === "ready";
  const rulesQ = useAsync<NotifyRule[]>(
    () => (notifyLive ? api.notify.rules() : Promise.resolve([])),
    [notifyLive, id],
  );

  // Live per-turn CPU/memory for THIS agent. On-demand: subscribing opens the daemon's sampler and
  // leaving the page (this hook's cleanup abort) stops it — nothing is measured unless this is open.
  const resources = useProcessStream(daemon?.state === "ready", id, agentId);

  // Freshen spend the moment the page opens (it's loaded once app-wide, then refetched on demand).
  useEffect(() => {
    reloadUsage();
  }, [reloadUsage]);

  const usageRow = usage?.agents.find((u) => u.daemonId === id && u.agent === agentId);
  const isDefault = daemon?.agent === agentId;
  const isActiveContext =
    activeDaemon?.id === id && (activeAgentId ? activeAgentId === agentId : isDefault);
  const [setup, setSetup] = useState<SetupStatus | null>(null);

  // The daemon owns installation and exposes an atomic, pollable job. Keep the browser a passive
  // observer: it gets status/step names only, never shell commands or installer credentials.
  useEffect(() => {
    if (daemon?.state !== "ready") return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const next = await api.daemonAgentSetupStatus(id, agentId);
        if (cancelled) return;
        setSetup(next);
        if (next.running) {
          timer = setTimeout(() => void poll(), 900);
        } else if (next.started && next.ok) {
          infoQ.reload();
        }
      } catch {
        // The normal agent inspection reports a more useful runtime error; do not replace it here.
      }
    };
    void poll();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [daemon?.state, id, agentId, infoQ.reload, setup?.running]);

  // Hooks all above this guard so the not-found fallback never changes hook order.
  if (!daemon) {
    return (
      <div className="flex h-full flex-col">
        <AgentHeader onBack={() => navigate("/")} backLabel="Console" />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
          <ServerOff className="size-6" />
          <p>This runtime is no longer in the fleet.</p>
          <Button variant="outline" size="sm" onClick={() => navigate("/")}>
            Back to Console
          </Button>
        </div>
      </div>
    );
  }

  const info = infoQ.data;
  const displayName = info?.name || agentId;

  // Live resource series for the chart: aggregated CPU% and memory over the rolling window, with the
  // current (latest) reading and the window peak for the headline figures. `running` is whether a turn
  // is live right now (drives the per-run rows); the chart shows history either way, so it doesn't blank
  // the instant a turn ends.
  const cpuSeries = resources.history.map((p) => p.cpu);
  const rssSeries = resources.history.map((p) => p.rss);
  const last = resources.history[resources.history.length - 1];
  const cpuPeak = cpuSeries.reduce((m, v) => (v > m ? v : m), 0);
  const rssPeak = rssSeries.reduce((m, v) => (v > m ? v : m), 0);
  const running = resources.samples.length > 0;

  // Token mix for the Spend bar — a fixed, learnable order (input → output → cache → reasoning) so the
  // legend reads the same across agents; the bar hides itself when nothing has been spent yet.
  const u = usageRow?.usage;
  const mixSegments: BarSegment[] = [
    { label: "Input", value: u?.inputTokens ?? 0 },
    { label: "Output", value: u?.outputTokens ?? 0 },
    { label: "Cache read", value: u?.cacheReadTokens ?? 0 },
    { label: "Cache write", value: u?.cacheWriteTokens ?? 0 },
    { label: "Reasoning", value: u?.reasoningTokens ?? 0 },
  ].map((s) => ({ ...s, display: compact(s.value) }));
  const mixTotal = mixSegments.reduce((a, s) => a + s.value, 0);

  async function useThisAgent() {
    try {
      await setActiveContext(id, agentId);
      toast.success(`Chat and config now target ${displayName}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not switch to this agent");
    }
  }

  async function installAgent() {
    try {
      setSetup(await api.daemonAgentSetup(id, agentId));
      toast.success(`Installing ${displayName}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start installation");
    }
  }

  return (
    <div className="flex h-full flex-col">
      <AgentHeader onBack={backToRuntime} backLabel={daemon.label}>
        <div className="flex size-9 shrink-0 items-center justify-center border border-border">
          <AgentIcon agentId={agentId} className="size-4" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h1 className="truncate text-sm font-semibold tracking-tight">{displayName}</h1>
            <span className="font-mono text-xs text-muted-foreground">{agentId}</span>
            {isDefault && <Badge variant="muted">default</Badge>}
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            on {daemon.label}
            {info?.version ? ` · adapter v${info.version}` : ""}
          </p>
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          {!info?.installedVersion && (
            <Button size="sm" onClick={() => void installAgent()} disabled={setup?.running}>
              {setup?.running && <Loader2 className="size-3.5 animate-spin" />}
              {setup?.running ? "Installing" : "Install"}
            </Button>
          )}
          {isActiveContext ? (
            <Badge>in use</Badge>
          ) : (
            <Button size="sm" onClick={() => void useThisAgent()}>
              Use this agent
            </Button>
          )}
        </div>
      </AgentHeader>

      <ScrollArea className="flex-1">
        <div className="space-y-4 px-6 py-6">
          {/* spend — the per-agent numbers, separated. Always shown (0s until a turn settles). */}
          <Section title="Spend" icon={Hash}>
            <div className="grid grid-cols-3 gap-4">
              <BigMetric value={compact(totalTokens(usageRow?.usage))} label="tokens" />
              <BigMetric value={compact(usageRow?.turns ?? 0)} label={usageRow?.turns === 1 ? "turn" : "turns"} />
              <BigMetric value={usd(usageRow?.costUsd)} label="cost" />
            </div>
            <div className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 border-t border-border pt-4 sm:grid-cols-3">
              <TokenStat label="Input" n={usageRow?.usage.inputTokens} />
              <TokenStat label="Output" n={usageRow?.usage.outputTokens} />
              <TokenStat label="Cache read" n={usageRow?.usage.cacheReadTokens} />
              <TokenStat label="Cache write" n={usageRow?.usage.cacheWriteTokens} />
              <TokenStat label="Reasoning" n={usageRow?.usage.reasoningTokens} />
              <TokenStat label="Total" n={totalTokens(usageRow?.usage)} />
            </div>
            {mixTotal > 0 && (
              <div className="mt-4 border-t border-border pt-4 text-ink">
                <div className="mb-2 text-[10px] uppercase tracking-wide text-muted-foreground">
                  Token mix
                </div>
                <StackedBar segments={mixSegments} />
              </div>
            )}
            <p className="mt-3 text-[10px] text-muted-foreground">
              {usageRow
                ? "Accumulated across this session's turns on this agent — separate from every other agent."
                : "No turns recorded for this agent yet. Numbers fill in as it runs."}
            </p>
          </Section>

          {!info?.installedVersion && (
            <Section title="Install agent" icon={Bot}>
              <p className="text-xs text-muted-foreground">
                {setup?.running
                  ? `Installing ${setup.current || displayName}…`
                  : "This harness is available on this runtime but its CLI is not installed yet."}
              </p>
              <div className="mt-3 h-1.5 overflow-hidden bg-border" role="progressbar" aria-label="Agent installation progress">
                <div
                  className="h-full bg-foreground transition-[width] duration-300"
                  style={{ width: `${setupProgress(setup)}%` }}
                />
              </div>
              {setup?.steps.length ? (
                <div className="mt-3 space-y-1 text-xs text-muted-foreground">
                  {setup.steps.map((step) => <p key={step.name}>{step.name} · {step.status}</p>)}
                </div>
              ) : null}
              <Button className="mt-4" size="sm" onClick={() => void installAgent()} disabled={setup?.running}>
                {setup?.running && <Loader2 className="size-3.5 animate-spin" />}
                {setup?.running ? "Installing…" : "Install"}
              </Button>
            </Section>
          )}

          <div className="grid gap-4 lg:grid-cols-2">
            {/* identity */}
            <Section title="Identity" icon={Bot}>
              {daemon.state !== "ready" ? (
                <NeedsRuntime />
              ) : infoQ.loading && !info ? (
                <Reading />
              ) : infoQ.error ? (
                <ErrorNote message={infoQ.error} />
              ) : info ? (
                <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-xs">
                  <Detail label="Adapter type" value={info.agentType} mono />
                  <Detail label="Installed" value={info.installedVersion || "unknown"} mono />
                  <Detail label="Configured" value={info.configured ? "yes" : "no"} />
                  <Detail label="Protocol" value={info.capabilities.protocol} mono />
                  <Detail label="Output" value={info.capabilities.output} mono />
                  <Detail label="Sessions" value={info.capabilities.sessions} mono />
                  {info.configPath && (
                    <div className="col-span-2 flex flex-col gap-0.5">
                      <dt className="text-muted-foreground">Config path</dt>
                      <dd className="break-all font-mono">{info.configPath}</dd>
                    </div>
                  )}
                </dl>
              ) : (
                <p className="text-xs text-muted-foreground">No agent info.</p>
              )}
            </Section>

            {/* auth */}
            <Section title="Authentication" icon={KeyRound}>
              {daemon.state !== "ready" ? (
                <NeedsRuntime />
              ) : infoQ.loading && !info ? (
                <Reading />
              ) : info ? (
                <div className="space-y-3">
                  <div
                    className={cn(
                      "inline-flex items-center gap-1.5 text-sm",
                      info.authStatus.configured ? "text-foreground" : "text-muted-foreground",
                    )}
                  >
                    {info.authStatus.configured ? (
                      <ShieldCheck className="size-4" />
                    ) : (
                      <ShieldAlert className="size-4" />
                    )}
                    {info.authStatus.configured
                      ? `Authenticated${info.authStatus.method ? ` · ${info.authStatus.method}` : ""}`
                      : "Not authenticated"}
                  </div>
                  {info.authStatus.detail && (
                    <p className="text-xs text-muted-foreground">{info.authStatus.detail}</p>
                  )}
                  {info.authMethods.length > 0 && (
                    <div className="flex flex-wrap gap-1.5">
                      {info.authMethods.map((m) => (
                        <Badge key={m.id} variant="outline">
                          {m.label || m.id}
                        </Badge>
                      ))}
                    </div>
                  )}
                  {isActiveContext && (
                    <Link
                      to="/auth"
                      className="inline-block text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                    >
                      Manage authentication →
                    </Link>
                  )}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">No auth info.</p>
              )}
            </Section>

            {/* capabilities — the full matrix, inline (self-contained per agent) */}
            <Section title="Capabilities" icon={Cpu} className="lg:col-span-2">
              {daemon.state !== "ready" ? (
                <NeedsRuntime />
              ) : infoQ.loading && !info ? (
                <Reading />
              ) : info ? (
                <div className="space-y-4">
                  {FLAG_GROUPS.map((g) => (
                    <div key={g.title}>
                      <div className="mb-2 text-[10px] uppercase tracking-wide text-muted-foreground">
                        {g.title}
                      </div>
                      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
                        {g.keys.map((k) => (
                          <Flag
                            key={k}
                            on={Boolean(info.capabilities[k])}
                            label={CAP_LABELS[k] ?? k}
                          />
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">No capability info.</p>
              )}
            </Section>
          </div>

          {/* live per-turn resources — the ONE per-agent resource fact the system can honestly report:
              a running turn's own process group, sampled on demand while this page is open */}
          <Section title="Live resources" icon={Activity}>
            {daemon.state !== "ready" ? (
              <p className="text-xs text-muted-foreground">
                Start the runtime to see live CPU and memory for this agent's turns.
              </p>
            ) : !resources.live ? (
              <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" />
                Connecting to the live sampler…
              </div>
            ) : (
              <div className="space-y-4">
                {/* headline figures + the rolling chart — retained after a turn ends, so the panel
                    shows the last couple of minutes of activity rather than blanking to nothing */}
                <div className="grid grid-cols-2 gap-3">
                  <ResourceChartCard
                    label="CPU"
                    current={`${(last?.cpu ?? 0).toFixed(0)}%`}
                    peak={`${cpuPeak.toFixed(0)}%`}
                    series={cpuSeries}
                    active={running}
                  />
                  <ResourceChartCard
                    label="Memory"
                    current={formatBytes(last?.rss)}
                    peak={formatBytes(rssPeak)}
                    series={rssSeries}
                    active={running}
                  />
                </div>

                {/* per-running-turn breakdown (there can be more than one live chat) — or an idle note */}
                {running ? (
                  <div className="flex flex-col divide-y divide-border border-t border-border pt-3">
                    {resources.samples.map((s) => (
                      <div
                        key={s.runId}
                        className="flex items-center justify-between gap-4 py-2 first:pt-0 last:pb-0"
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <span className="size-1.5 shrink-0 animate-pulse bg-foreground" />
                          <span className="truncate font-mono text-xs text-muted-foreground">
                            {s.chatId}
                          </span>
                        </div>
                        <div className="flex shrink-0 items-center gap-5">
                          <ResReading label="CPU" value={`${s.cpuPercent.toFixed(0)}%`} />
                          <ResReading label="Memory" value={formatBytes(s.rssBytes)} />
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-[10px] text-muted-foreground">
                    Idle — no turn is running right now. The chart holds the last few minutes; live
                    figures resume the moment this agent works.
                  </p>
                )}
              </div>
            )}
            {resources.error && (
              <p className="mt-3 text-[10px] text-muted-foreground">{resources.error}</p>
            )}
          </Section>

          {/* runtime context — honest framing: whole-process snapshot, complementary to the live view */}
          <Section title="Runtime" icon={Cpu}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="min-w-0 text-xs">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{daemon.label}</span>
                  <Badge variant="outline">{daemon.provider}</Badge>
                </div>
                <p className="mt-0.5 truncate text-muted-foreground">{daemon.location.summary}</p>
              </div>
              <Button variant="outline" size="sm" onClick={backToRuntime}>
                Open runtime
              </Button>
            </div>
            <p className="mt-3 text-[10px] text-muted-foreground">
              Memory and CPU are reported for the whole runtime process, not broken out per agent — see
              the runtime page for its live resource snapshot.
            </p>
          </Section>

          {/* notifications — which routing rules deliver THIS agent's events (daemon-wide config) */}
          <Section title="Notifications" icon={Bell}>
            {!notifyLive ? (
              <p className="text-xs text-muted-foreground">
                Notification routing is configured on the active runtime.{" "}
                {isActiveContext ? null : "Use this agent to view and manage its rules, or open "}
                <Link
                  to="/notifications"
                  className="underline-offset-2 hover:text-foreground hover:underline"
                >
                  Notifications
                </Link>
                .
              </p>
            ) : (
              <AgentRules agentId={agentId} rulesQ={rulesQ} />
            )}
          </Section>

          {daemon.state === "ready" && (
            <div className="flex justify-end">
              <button
                type="button"
                onClick={infoQ.reload}
                className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
              >
                <RefreshCw className={cn("size-3", infoQ.loading && "animate-spin")} />
                Refresh
              </button>
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

function AgentHeader({
  onBack,
  backLabel,
  children,
}: {
  onBack: () => void;
  backLabel: string;
  children?: ReactNode;
}) {
  return (
    <header className={cn(SURFACE_HEADER, "px-6")}>
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="size-3.5" />
        <span className="max-w-[12rem] truncate">{backLabel}</span>
      </button>
      {children && <span className="text-muted-foreground/40">/</span>}
      {children}
    </header>
  );
}

function Section({
  title,
  icon: Icon,
  className,
  children,
}: {
  title: string;
  icon?: ComponentType<{ className?: string }>;
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
      </div>
      {children}
    </section>
  );
}

function BigMetric({ value, label }: { value: string; label: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-2xl font-semibold tabular-nums tracking-tight">{value}</span>
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
    </div>
  );
}

// ResourceChartCard is one metric's live view: a headline current reading + window peak over a rolling
// monochrome area chart. `active` pulses a dot while a turn is running; the chart itself persists the
// recent history either way (the anti-blank behaviour). Wrapped in `text-ink` so the chart's
// currentColor picks up the legible ink token in both themes.
function ResourceChartCard({
  label,
  current,
  peak,
  series,
  active,
}: {
  label: string;
  current: string;
  peak: string;
  series: number[];
  active: boolean;
}) {
  return (
    <div className="border border-border p-3 text-ink">
      <div className="flex items-center justify-between">
        <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
        {active && <span className="size-1.5 shrink-0 animate-pulse bg-foreground" />}
      </div>
      <div className="mt-1 flex items-baseline gap-2">
        <span className="text-xl font-semibold tabular-nums tracking-tight">{current}</span>
        <span className="text-[10px] text-muted-foreground">peak {peak}</span>
      </div>
      <div className="mt-2">
        <MiniAreaChart values={series} />
      </div>
    </div>
  );
}

// ResReading is one live figure (CPU% or memory) for a running turn — right-aligned, label above.
function ResReading({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col items-end gap-0.5">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
      <span className="text-sm tabular-nums">{value}</span>
    </div>
  );
}

function TokenStat({ label, n }: { label: string; n: number | undefined }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
      <span className="text-sm tabular-nums">{compact(n)}</span>
    </div>
  );
}

function Detail({
  label,
  value,
  mono,
}: {
  label: string;
  value?: string | number;
  mono?: boolean;
}) {
  if (value === undefined || value === "") return null;
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("truncate", mono && "font-mono")}>{value}</dd>
    </div>
  );
}

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

function NeedsRuntime() {
  return (
    <p className="text-xs text-muted-foreground">
      The runtime isn’t running — spin it up to read this agent’s live details.
    </p>
  );
}

function Reading() {
  return (
    <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
      <Loader2 className="size-3.5 animate-spin" />
      Reading…
    </div>
  );
}

function setupProgress(status: SetupStatus | null): number {
  if (!status?.started) return 0;
  if (!status.running) return status.ok ? 100 : 0;
  // Step counts are dynamic per harness. Reserve the final fifth for the in-flight command so the
  // indicator never claims completion while npm/curl is still running.
  return Math.min(90, 15 + status.steps.length * 25);
}

// The agent-scoped view of notification routing: the daemon's rules, filtered to those that target this
// adapter type. Read-only here — editing lives on the Notifications surface, which this links out to.
function AgentRules({
  agentId,
  rulesQ,
}: {
  agentId: string;
  rulesQ: { data: NotifyRule[] | null; loading: boolean; error: string | null };
}) {
  if (rulesQ.loading && !rulesQ.data) return <Reading />;
  if (rulesQ.error) return <ErrorNote message={rulesQ.error} />;
  const rules = (rulesQ.data ?? []).filter((r) => r.scope === "agent" && r.agent === agentId);
  if (rules.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        No rules route this agent’s events yet.{" "}
        <Link
          to="/notifications"
          className="underline-offset-2 hover:text-foreground hover:underline"
        >
          Add one →
        </Link>
      </p>
    );
  }
  return (
    <div className="space-y-2">
      {rules.map((r) => (
        <div
          key={r.id}
          className="flex items-center gap-2 border border-border px-3 py-2 text-xs"
        >
          <span className={cn("size-1.5 rounded-full", r.enabled ? "bg-foreground" : "bg-foreground/30")} />
          <span className="text-muted-foreground">
            {r.conditions && r.conditions.length > 0 ? r.conditions.join(", ") : "all events"}
          </span>
          <span className="ml-auto text-muted-foreground">
            {r.channelIds.length} channel{r.channelIds.length === 1 ? "" : "s"}
          </span>
        </div>
      ))}
      <Link
        to="/notifications"
        className="inline-block text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
      >
        Manage notifications →
      </Link>
    </div>
  );
}
