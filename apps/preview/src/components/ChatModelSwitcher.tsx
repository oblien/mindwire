// The chat header's model picker — the second axis next to the harness switcher, so you can change
// model without leaving the conversation. It is the SAME setting the Models panel writes: the model
// field of the agent's sticky config, found through the settings schema's `canon: "model"` (see
// `modelFieldKey`), so the two surfaces stay in lockstep and neither hardcodes a per-harness key.
//
// Everything here is scoped to the CHAT's agent (`chatAgentId`), never the sidebar's config agent —
// addressed explicitly via `api.modelsFor` / `api.getConfigFor` / `api.setConfigFor` rather than the
// api layer's module-level `?agent=`. Same reasoning as ChatAgentSwitcher: configuring one adapter
// must never re-point a live conversation on another.
//
// Renders nothing at all unless the chat's agent both declares the `models` capability and exposes a
// model field in its schema — an agent that can't enumerate or can't be pointed at a model has no
// honest control to show. The runnable list is whatever the daemon reports for that agent, verbatim
// (see ModelsPanel's note); this is a picker over it, not a second source of truth.
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Check, ChevronDown, Loader2, Search } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { cn } from "@/lib/utils";
import { hasModelField, modelFieldKey } from "@/lib/model-config";
import { Input } from "@/components/ui/input";
import { ProviderLogo, providerLabel } from "@/components/ProviderLogo";
import { toast } from "@/components/ui/sonner";
import type { ModelInfo } from "@shared/api";

export function ChatModelSwitcher({ running }: { running: boolean }) {
  const { activeDaemon, chatAgent, chatAgentId } = useApp();
  const navigate = useNavigate();

  const [open, setOpen] = useState(false);
  // The list is fetched on first open, not on mount: the chat rail is always mounted, and a harness
  // like opencode enumerates hundreds of models. Once armed it stays armed, so the fetch re-runs only
  // when the (daemon, chat agent) scope actually changes.
  const [armed, setArmed] = useState(false);
  const [filter, setFilter] = useState("");
  const [saving, setSaving] = useState<string | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const ready = activeDaemon?.state === "ready";
  const key = modelFieldKey(chatAgent?.schema);
  const supported = (chatAgent?.capabilities.models ?? false) && hasModelField(chatAgent?.schema);
  const scopeKey = `${activeDaemon?.id ?? "none"}:${chatAgentId ?? ""}:${activeDaemon?.state ?? "none"}`;

  // Config is cheap and drives the trigger label, so it loads with the scope; models wait for `armed`.
  const configQ = useAsync<Record<string, string> | null>(
    () => (ready && supported ? api.getConfigFor(chatAgentId) : Promise.resolve(null)),
    [scopeKey, supported],
  );
  const modelsQ = useAsync<ModelInfo[] | null>(
    () => (ready && supported && armed ? api.modelsFor(chatAgentId) : Promise.resolve(null)),
    [scopeKey, supported, armed],
  );

  const current = configQ.data?.[key];
  const models = modelsQ.data ?? [];

  // Group the filtered list by provider, exactly as the Models panel does, so the same model sits under
  // the same brand in both places.
  const groups = useMemo(() => {
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
  }, [models, filter]);

  const firstMatch = groups[0]?.[1][0];

  // Close on click-away and on Escape — same idiom as the Settings model combobox.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // A runtime or chat-agent switch invalidates the open list.
  useEffect(() => {
    setOpen(false);
    setFilter("");
  }, [scopeKey]);

  if (!supported) return null;

  async function pick(id: string) {
    if (id === current) {
      setOpen(false);
      return;
    }
    setSaving(id);
    try {
      await api.setConfigFor({ [key]: id }, chatAgentId);
      configQ.reload();
      setOpen(false);
      setFilter("");
      // The sticky config binds the NEXT turn. `set-model` mid-flight is Claude-only and only on the
      // persistent transport, so never imply this steers a turn that's already running.
      toast.success(running ? `Model set to ${id} — applies to the next turn` : `Model set to ${id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not set model");
    } finally {
      setSaving(null);
    }
  }

  return (
    // Deliberately NOT a positioning context: the dropdown below is absolute against the chat header
    // (which carries `relative`), so it spans the full rail width and can't be clipped by the rail's
    // `overflow-hidden`. Anchoring it to this narrow trigger instead would push it off-edge.
    <div ref={wrapRef} className="min-w-0">
      <button
        type="button"
        onClick={() => {
          setArmed(true);
          setOpen((v) => !v);
        }}
        disabled={!ready}
        aria-label="Chat model"
        aria-expanded={open}
        title={current ? `Model: ${current}` : "Pick the model this agent runs"}
        className={cn(
          "flex h-9 w-full items-center gap-2 border border-border px-2.5 text-xs transition-colors",
          "hover:bg-accent disabled:pointer-events-none disabled:opacity-50",
          open && "bg-accent",
        )}
      >
        {/* No leading mark, deliberately. This chip shares a 26rem rail with the harness switcher, and
            an icon plus its gap costs ~22px — the difference between reading `claude-fable-5` and
            truncating it. The chevron already says "picker", the mono id next to a harness name already
            says "model", and the full value is in the tooltip. Provider marks live on the group headers
            below, where the list is loaded and there is room for them. */}
        <span
          className={cn(
            "truncate font-mono",
            current ? "text-foreground" : "text-muted-foreground",
          )}
        >
          {current ?? "default"}
        </span>
        <ChevronDown className="ml-auto size-3.5 shrink-0 opacity-45" />
      </button>

      {open && (
        <div className="absolute inset-x-3 top-full z-10 mt-px flex max-h-[min(30rem,60vh)] flex-col border border-border bg-background shadow-md">
          <div className="relative shrink-0 border-b border-border p-2">
            <Search className="absolute left-4 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              autoFocus
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && firstMatch) {
                  e.preventDefault();
                  void pick(firstMatch.id);
                }
              }}
              placeholder={models.length ? `Filter ${models.length} models…` : "Filter models…"}
              className="h-8 pl-7 text-xs"
            />
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto">
            {modelsQ.loading && (
              <p className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" /> Loading models…
              </p>
            )}
            {modelsQ.error && (
              <p className="px-3 py-4 text-xs text-destructive">{modelsQ.error}</p>
            )}
            {/* An empty list has real causes (not signed in, no provider connected) that the Models
                panel already explains properly — point there instead of restating them in a dropdown. */}
            {!modelsQ.loading && !modelsQ.error && models.length === 0 && (
              <p className="px-3 py-4 text-xs text-muted-foreground">
                This agent isn’t listing any models yet.
              </p>
            )}
            {!modelsQ.loading && models.length > 0 && groups.length === 0 && (
              <p className="px-3 py-4 text-xs text-muted-foreground">No models match.</p>
            )}

            {groups.map(([provider, list]) => (
              <section key={provider}>
                <header className="flex items-center gap-2 border-b border-border bg-ink/[0.03] px-2.5 py-1.5">
                  <ProviderLogo provider={provider} className="size-3.5 shrink-0 text-ink" />
                  <span className="truncate text-[11px] font-medium">
                    {providerLabel(provider)}
                  </span>
                  <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">
                    {list.length}
                  </span>
                </header>
                <div className="divide-y divide-border/60">
                  {list.map((m) => {
                    const active = m.id === current;
                    return (
                      <button
                        key={m.id}
                        type="button"
                        disabled={saving !== null}
                        onClick={() => void pick(m.id)}
                        className={cn(
                          "flex w-full items-center gap-2 px-2.5 py-2 text-left transition-colors disabled:cursor-wait hover:bg-accent",
                          active && "bg-accent",
                        )}
                      >
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-xs">{m.label}</span>
                          <span className="block truncate font-mono text-[10px] text-muted-foreground">
                            {m.id}
                          </span>
                        </span>
                        {saving === m.id ? (
                          <Loader2 className="size-3.5 shrink-0 animate-spin" />
                        ) : (
                          <Check
                            className={cn(
                              "size-3.5 shrink-0",
                              active ? "opacity-100" : "opacity-0",
                            )}
                          />
                        )}
                      </button>
                    );
                  })}
                </div>
              </section>
            ))}
          </div>

          {/* Say plainly what picking does: it writes the agent's sticky config — the same setting the
              Models panel owns — not a per-thread override. */}
          <div className="flex shrink-0 items-center gap-2 border-t border-border px-2.5 py-1.5">
            <span className="min-w-0 flex-1 truncate text-[10px] text-muted-foreground">
              Saved to this agent’s sticky config
            </span>
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                navigate("/models");
              }}
              className="shrink-0 text-[10px] font-medium text-foreground underline-offset-2 hover:underline"
            >
              All models →
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
