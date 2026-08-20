// The Models surface — where you pick the model the agent runs. Presented models.dev-style: grouped by
// provider with the provider's brand mark, each row showing the model's label, id, context window,
// capability marks (reasoning / tools / vision) and per-Mtok pricing. Picking a model writes it to the
// sticky config under the model field's native key (found via the settings schema's `canon: "model"`),
// so it survives across turns.
//
// THE DAEMON'S LIST IS THE RUNNABLE SET. Whatever the harness reports on /models is exactly what it can
// run right now — its built-ins, anything authed by an ambient key, plus every provider you've connected.
// opencode only enumerates a provider once it's authenticated, so there is nothing to re-filter here: we
// show the list verbatim. Providers is how you ADD to this list (connect a provider → its models start
// showing up), not a gate on it — so for a provider-registry agent we point there to get more, but we
// never hide the models the daemon already lists.
//
// Each provider group header carries the way BACK to its connection — the credential behind those models
// is managed on Providers, and noticing a provider here is exactly when you want to re-key or drop it.
// The link is unconditional for a real provider id (a group is always SOME provider's page, connected or
// not); the "Connected" pill is decoration from GET /providers and nothing depends on it, because that
// call answers per-agent and a shared credential can be live in a run the agent doesn't report.
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Search, Check, Loader2, Brain, Wrench, Eye, Plug, KeyRound } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { cn } from "@/lib/utils";
import { compact } from "@/lib/format";
import { Panel, Spinner, ErrorNote, EmptyState } from "@/components/common/Panel";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { ProviderLogo, providerLabel } from "@/components/ProviderLogo";
import { modelFieldKey } from "@/lib/model-config";
import { toast } from "@/components/ui/sonner";
import { providerPath } from "@/lib/nav";
import type { ModelInfo, AuthStatus, CustomProvider } from "@shared/api";

/** Terse per-1M-token price: `3 → "$3"`, `0.3 → "$0.30"`, `undefined → null`. */
function perM(n: number | undefined): string | null {
  if (n === undefined) return null;
  if (n === 0) return "$0";
  return n < 1 ? `$${n.toFixed(2)}` : `$${(+n.toFixed(2)).toString()}`;
}

/** One capability mark; renders nothing when the flag is off so the row only shows what's true. */
function Cap({ on, icon: Icon, label }: { on?: boolean; icon: typeof Brain; label: string }) {
  if (!on) return null;
  return (
    <span
      title={label}
      className="flex size-6 items-center justify-center border border-ink/12 bg-ink/[0.03] text-muted-foreground"
    >
      <Icon className="size-3.5" />
    </span>
  );
}

export function ModelsPanel() {
  const { agent } = useApp();
  const navigate = useNavigate();
  const key = modelFieldKey(agent?.schema);
  // Provider-registry agents (opencode, Codex) can ADD models by connecting providers, so we surface a
  // pointer to Providers. Self-enumerating agents (Claude, no registry) can't — an empty list there means
  // "not signed in," not "connect a provider." This only changes the copy/empty-state, never what's shown.
  const gated = agent?.capabilities.customProviders ?? false;

  const modelsQ = useAsync<ModelInfo[]>(() => api.models());
  const configQ = useAsync<Record<string, string>>(() => api.getConfig());
  // The harness's OWN auth check is the source of truth for "signed in" — the same signal the Agent auth
  // panel shows. We only consult it to pick honest empty-state copy: an agent the harness reports as
  // signed in must never be told to "sign in first" just because its list came back empty (some accounts
  // and gateways don't expose a scriptable model list at all).
  const authQ = useAsync<AuthStatus>(() => api.authStatus());
  // Which providers hold a stored credential — used ONLY to decorate a group header. Skipped entirely for
  // an agent with no provider registry, which has nothing to report.
  const storedQ = useAsync<CustomProvider[]>(
    () => (gated ? api.providers() : Promise.resolve([])),
    [gated],
  );
  const connected = useMemo(
    () => new Set((storedQ.data ?? []).filter((p) => p.hasKey).map((p) => p.id)),
    [storedQ.data],
  );
  const [filter, setFilter] = useState("");
  const [saving, setSaving] = useState<string | null>(null);

  const current = configQ.data?.[key];

  // The daemon's list is authoritative — no connection filter. We only text-filter and group by provider.
  const total = modelsQ.data?.length ?? 0;

  const groups = useMemo(() => {
    const models = modelsQ.data ?? [];
    const f = filter.trim().toLowerCase();
    const filtered = f
      ? models.filter(
          (m) =>
            m.id.toLowerCase().includes(f) ||
            m.label.toLowerCase().includes(f) ||
            (m.provider ?? "").toLowerCase().includes(f) ||
            providerLabel(m.provider ?? "").toLowerCase().includes(f),
        )
      : models;
    const by = new Map<string, ModelInfo[]>();
    for (const m of filtered) {
      const p = m.provider ?? (m.custom ? "custom" : "other");
      const arr = by.get(p) ?? [];
      arr.push(m);
      by.set(p, arr);
    }
    return [...by.entries()].sort((a, b) => providerLabel(a[0]).localeCompare(providerLabel(b[0])));
  }, [modelsQ.data, filter]);

  async function pick(id: string) {
    setSaving(id);
    try {
      await api.setConfig({ [key]: id });
      configQ.reload();
      toast.success(`Model set to ${id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not set model");
    } finally {
      setSaving(null);
    }
  }

  const loading = modelsQ.loading || configQ.loading;

  return (
    <Panel
      title="Models"
      description="Pick the model the agent runs. Saved to the sticky config."
      actions={
        current ? (
          <Badge variant="outline" className="font-mono">
            {current}
          </Badge>
        ) : undefined
      }
    >
      {gated && total > 0 && (
        <p className="mb-4 flex items-center gap-1.5 text-xs text-muted-foreground">
          <Plug className="size-3.5" />
          These are the models your agent can run.
          <button
            type="button"
            onClick={() => navigate("/providers")}
            className="font-medium text-foreground underline-offset-2 hover:underline"
          >
            Connect a provider to add more →
          </button>
        </p>
      )}

      <div className="relative mb-5">
        <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={total ? `Filter ${total} models…` : "Filter models…"}
          className="pl-9"
        />
      </div>

      {loading && <Spinner />}
      {modelsQ.error && <ErrorNote message={modelsQ.error} />}

      {/* The daemon reported no models at all. For a registry agent that usually means the daemon is down
          or the harness isn't installed — but connecting a provider is still the way to grow the list, so
          point there. For a self-enumerating agent (Claude) it almost always means "not signed in." */}
      {!loading &&
        !modelsQ.error &&
        modelsQ.data &&
        total === 0 &&
        (gated ? (
          <EmptyState>
            <span className="block">This agent isn’t listing any models yet.</span>
            <span className="mt-1 block text-xs">
              Its built-in models load once the daemon is up; connect a provider to add more.
            </span>
            <button
              type="button"
              onClick={() => navigate("/providers")}
              className="mt-2 inline-flex items-center gap-1.5 border border-ink/25 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-accent"
            >
              <Plug className="size-3.5" />
              Connect a provider
            </button>
          </EmptyState>
        ) : authQ.loading ? (
          // Don't guess the reason until the harness's own auth check answers — otherwise the copy would
          // flash "sign in first" and then correct itself for an already-signed-in agent.
          <EmptyState>
            <span className="block">Checking sign-in…</span>
          </EmptyState>
        ) : authQ.data?.configured ? (
          // The harness reports it IS signed in — the list is just empty. That's legitimate: this account
          // or gateway may not expose a scriptable model list. Never tell a signed-in agent to sign in.
          <EmptyState>
            <span className="block">
              Signed in{authQ.data.method ? ` · ${authQ.data.method}` : ""} — but no models came back.
            </span>
            <span className="mt-1 block text-xs">
              Some accounts and gateways don’t expose a model list. You can still set the model by name in
              Settings — it’s validated by the harness.
            </span>
            <button
              type="button"
              onClick={() => {
                modelsQ.reload();
                authQ.reload();
              }}
              className="mt-2 inline-flex items-center gap-1.5 border border-ink/25 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-accent"
            >
              <Loader2 className={cn("size-3.5", modelsQ.loading && "animate-spin")} />
              Retry
            </button>
          </EmptyState>
        ) : (
          <EmptyState>
            <span className="block">This agent didn’t list any models.</span>
            <span className="mt-1 block text-xs">
              It enumerates models from its own account, so sign in first.
            </span>
            <button
              type="button"
              onClick={() => navigate("/auth")}
              className="mt-2 inline-flex items-center gap-1.5 border border-ink/25 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-accent"
            >
              <KeyRound className="size-3.5" />
              Go to Agent auth
            </button>
          </EmptyState>
        ))}
      {!loading && !modelsQ.error && modelsQ.data && total > 0 && groups.length === 0 && (
        <EmptyState>No models match.</EmptyState>
      )}

      <div className="space-y-7">
        {groups.map(([provider, models]) => (
          <section key={provider}>
            <header className="mb-2.5 flex items-center gap-2">
              <span className="flex size-6 items-center justify-center border border-ink/15 bg-ink/[0.03]">
                <ProviderLogo provider={provider} className="size-4 text-ink" />
              </span>
              <h2 className="text-sm font-semibold tracking-tight">{providerLabel(provider)}</h2>
              <span className="text-xs tabular-nums text-muted-foreground">{models.length}</span>
              {connected.has(provider) && (
                <span className="inline-flex shrink-0 items-center gap-1 border border-ink/25 bg-ink/[0.05] px-1.5 py-0.5 text-[10px] font-medium text-ink/70">
                  <span className="size-1.5 rounded-full bg-ink" />
                  Connected
                </span>
              )}
              {/* "custom"/"other" are this page's own buckets for models that name no provider — there is
                  no provider page to send anyone to, so they get no link. */}
              {gated && provider !== "custom" && provider !== "other" && (
                <button
                  type="button"
                  onClick={() => navigate(providerPath(provider))}
                  className="ml-auto shrink-0 text-[11px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                >
                  {connected.has(provider) ? "Manage auth →" : "Connect →"}
                </button>
              )}
            </header>

            <div className="divide-y divide-border border border-border">
              {models.map((m) => {
                const active = m.id === current;
                const vision = m.attachment || m.inputModalities?.includes("image");
                const inCost = perM(m.cost?.input);
                const outCost = perM(m.cost?.output);
                return (
                  <button
                    key={m.id}
                    type="button"
                    disabled={saving !== null}
                    onClick={() => pick(m.id)}
                    className={cn(
                      "flex w-full items-center gap-3 px-4 py-3 text-left transition-colors disabled:cursor-wait hover:bg-accent",
                      active && "bg-accent",
                    )}
                  >
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">{m.label}</p>
                      <p className="truncate font-mono text-[11px] text-muted-foreground">{m.id}</p>
                    </div>

                    <div className="flex shrink-0 items-center gap-3">
                      {m.contextWindow ? (
                        <span
                          title="Context window"
                          className="hidden tabular-nums text-[11px] text-muted-foreground sm:inline"
                        >
                          {compact(m.contextWindow)} ctx
                        </span>
                      ) : null}

                      <div className="hidden items-center gap-1 sm:flex">
                        <Cap on={m.reasoning} icon={Brain} label="Reasoning" />
                        <Cap on={m.toolCall} icon={Wrench} label="Tool calling" />
                        <Cap on={vision} icon={Eye} label="Vision / image input" />
                      </div>

                      {inCost && outCost ? (
                        <span
                          title="Price per 1M tokens (input / output)"
                          className="hidden w-20 text-right font-mono text-[11px] tabular-nums text-muted-foreground md:inline"
                        >
                          {inCost} / {outCost}
                        </span>
                      ) : m.custom ? (
                        <Badge variant="outline" className="hidden md:inline-flex">
                          custom
                        </Badge>
                      ) : null}

                      <span
                        className={cn(
                          "flex size-6 items-center justify-center border",
                          active
                            ? "border-ink/30 bg-ink text-background"
                            : "border-transparent text-transparent",
                        )}
                      >
                        {saving === m.id ? (
                          <Loader2 className="size-3.5 animate-spin text-foreground" />
                        ) : (
                          <Check className="size-3.5" />
                        )}
                      </span>
                    </div>
                  </button>
                );
              })}
            </div>
          </section>
        ))}
      </div>
    </Panel>
  );
}
