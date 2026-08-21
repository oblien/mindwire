// The Notifications surface — daemon-WIDE notification routing for the active runtime. The daemon owns
// a set of named delivery **channels** (a webhook URL shaped for webhook/Slack/Discord/Telegram, with
// optional headers, a bearer token, and — for the raw webhook type — an HMAC signing secret) and the
// **rules** that route each emitted notification to the union of matching channels (global / per-agent /
// per-session, with optional event selection). Three tabs: Channels · Rules · Activity (a live feed).
//
// SECURITY: channel secrets are write-only. The list view is masked — the server returns only presence
// booleans (`hasToken`/`hasSecret`), the header key names, and the URL host, never a secret value or the
// full URL. On save, a blank url/token/secret leaves the stored value untouched (merge-preserve), so
// round-tripping the masked view can't wipe a secret.
import { useMemo, useState, type ReactNode } from "react";
import { Plus, Trash2, Loader2, Radio, Zap, X, Globe, Check, ChevronDown, Bot } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { useNotifyFeed } from "@/lib/useNotifyFeed";
import { cn } from "@/lib/utils";
import { ChannelIcon } from "@/components/ChannelIcon";
import { AgentIcon } from "@/components/AgentIcon";
import { Panel, Spinner, ErrorNote, EmptyState } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import type {
  Condition,
  NotifyChannel,
  NotifyChannelInput,
  NotifyChannelType,
  NotifyRule,
  NotifyRuleInput,
  NotifyRuleScope,
} from "@shared/api";

// ---- vocab -----------------------------------------------------------------

const CHANNEL_TYPES: {
  value: NotifyChannelType;
  label: string;
  hint: string;
  urlPlaceholder: string;
  urlHelp: string;
}[] = [
  {
    value: "slack",
    label: "Slack",
    hint: "Posts to a Slack channel via an Incoming Webhook.",
    urlPlaceholder: "https://hooks.slack.com/services/…",
    urlHelp: "Your Slack Incoming Webhook URL.",
  },
  {
    value: "discord",
    label: "Discord",
    hint: "Posts a message through a Discord channel webhook.",
    urlPlaceholder: "https://discord.com/api/webhooks/…",
    urlHelp: "A Discord channel → Integrations → Webhook URL.",
  },
  {
    value: "telegram",
    label: "Telegram",
    hint: "Sends a message from your bot to a chat.",
    urlPlaceholder: "https://api.telegram.org/bot<token>/sendMessage?chat_id=<id>",
    urlHelp: "The full bot sendMessage URL (token + chat_id in the URL).",
  },
  {
    value: "webhook",
    label: "Webhook",
    hint: "POSTs the raw Notification JSON to your own receiver.",
    urlPlaceholder: "https://your-service.example.com/hooks/mindwire",
    urlHelp: "Your endpoint — receives the raw Notification JSON.",
  },
];

function typeMeta(t: NotifyChannelType) {
  return CHANNEL_TYPES.find((c) => c.value === t) ?? CHANNEL_TYPES[0];
}

const CONDITIONS: { value: Condition; label: string }[] = [
  { value: "finished", label: "Finished" },
  { value: "error", label: "Error" },
  { value: "waiting_approval", label: "Waiting · approval" },
  { value: "waiting_feedback", label: "Waiting · feedback" },
  { value: "waiting_input", label: "Waiting · input" },
];

function conditionLabel(c: Condition): string {
  return CONDITIONS.find((x) => x.value === c)?.label ?? String(c);
}

const SCOPES: { value: NotifyRuleScope; label: string; hint: string }[] = [
  { value: "global", label: "Global", hint: "Every notification from this runtime." },
  { value: "agent", label: "Per agent", hint: "Only one adapter type's notifications." },
  { value: "session", label: "Per session", hint: "Only one chat/session's notifications." },
];

// ---- panel -----------------------------------------------------------------

export function NotificationsPanel() {
  const { activeDaemon } = useApp();
  const ready = activeDaemon?.state === "ready";
  const daemonKey = `${activeDaemon?.id ?? "none"}:${activeDaemon?.state ?? "none"}`;

  const channelsQ = useAsync<NotifyChannel[]>(
    () => (ready ? api.notify.channels() : Promise.resolve([])),
    [daemonKey],
  );
  const rulesQ = useAsync<NotifyRule[]>(
    () => (ready ? api.notify.rules() : Promise.resolve([])),
    [daemonKey],
  );

  const [tab, setTab] = useState("channels");
  const feed = useNotifyFeed(ready && tab === "activity", activeDaemon?.id);

  if (!ready) {
    return (
      <Panel
        title="Notifications"
        description="Route this runtime's events to Slack, Discord, Telegram, or any webhook."
      >
        <EmptyState>
          {activeDaemon
            ? "This runtime isn't running. Spin it up to configure notifications."
            : "Add a runtime to configure notifications."}
        </EmptyState>
      </Panel>
    );
  }

  const channels = channelsQ.data ?? [];
  const rules = rulesQ.data ?? [];

  return (
    <Panel
      title="Notifications"
      description="Daemon-driven fan-out: named channels + rules that route events per agent or session."
    >
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="channels">Channels</TabsTrigger>
          <TabsTrigger value="rules">Rules</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
        </TabsList>

        {/* ---- channels ---- */}
        <TabsContent value="channels" className="mt-5 space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-xs text-muted-foreground">
              Delivery targets. Secrets are write-only — stored on the runtime, never shown here.
            </p>
            <ChannelDialog
              onSaved={() => {
                channelsQ.reload();
                rulesQ.reload();
              }}
            />
          </div>
          {channelsQ.loading && <Spinner />}
          {channelsQ.error && <ErrorNote message={channelsQ.error} />}
          {channelsQ.data && channels.length === 0 && (
            <EmptyState>No channels yet. Add one to start routing events.</EmptyState>
          )}
          <div className="space-y-2">
            {channels.map((ch) => (
              <ChannelRow
                key={ch.id}
                channel={ch}
                onChanged={() => channelsQ.reload()}
              />
            ))}
          </div>
        </TabsContent>

        {/* ---- rules ---- */}
        <TabsContent value="rules" className="mt-5 space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-xs text-muted-foreground">
              Which events reach which channels — globally, or scoped to one agent or session.
            </p>
            <RuleDialog channels={channels} onSaved={() => rulesQ.reload()} />
          </div>
          {rulesQ.loading && <Spinner />}
          {rulesQ.error && <ErrorNote message={rulesQ.error} />}
          {rulesQ.data && rules.length === 0 && (
            <EmptyState>
              {channels.length === 0
                ? "Add a channel first, then create a rule to route events to it."
                : "No rules yet. Add one to route events to your channels."}
            </EmptyState>
          )}
          <div className="space-y-2">
            {rules.map((r) => (
              <RuleRow
                key={r.id}
                rule={r}
                channels={channels}
                onChanged={() => rulesQ.reload()}
              />
            ))}
          </div>
        </TabsContent>

        {/* ---- activity ---- */}
        <TabsContent value="activity" className="mt-5 space-y-3">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Radio className={cn("size-3.5", feed.status === "open" && "text-emerald-500")} />
            {feed.status === "open"
              ? "Live — new notifications appear as the runtime emits them."
              : feed.status === "connecting"
                ? "Connecting to the live feed…"
                : feed.status === "error"
                  ? "Feed disconnected."
                  : "Idle."}
          </div>
          {feed.error && <ErrorNote message={feed.error} />}
          {feed.items.length === 0 ? (
            <EmptyState>No notifications yet. Run a turn to see events flow.</EmptyState>
          ) : (
            <div className="space-y-2">
              {feed.items.map((n, i) => (
                <div key={i} className="border border-border bg-card p-3">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline">{conditionLabel(n.condition)}</Badge>
                    {n.agent && <span className="text-xs text-muted-foreground">{n.agent}</span>}
                    {n.chatId && (
                      <span className="ml-auto font-mono text-[11px] text-muted-foreground">
                        {n.chatId.slice(0, 8)}
                      </span>
                    )}
                  </div>
                  {n.title && <p className="mt-1.5 text-sm font-medium">{n.title}</p>}
                  {n.body && (
                    <p className="mt-0.5 whitespace-pre-wrap text-xs text-muted-foreground">{n.body}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </Panel>
  );
}

// ---- channel row -----------------------------------------------------------

function ChannelRow({ channel, onChanged }: { channel: NotifyChannel; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const meta = typeMeta(channel.type);

  async function toggle(enabled: boolean) {
    setBusy(true);
    try {
      // Partial PUT — omitting url/token/secret merge-preserves them; only `enabled` changes.
      await api.notify.setChannel(channel.id, { enabled });
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not update");
    } finally {
      setBusy(false);
    }
  }

  async function test() {
    setBusy(true);
    try {
      const res = await api.notify.testChannel(channel.id);
      if (res.ok) toast.success("Test notification delivered");
      else toast.error(res.error || "Delivery failed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not test");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await api.notify.deleteChannel(channel.id);
      onChanged();
      toast.success("Channel deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-3 border border-border bg-card px-3 py-2.5">
      <ChannelIcon type={channel.type} className="size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{channel.label || meta.label}</span>
          <Badge variant="muted">{meta.label}</Badge>
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
          <span className="font-mono">{channel.urlHost || "no url"}</span>
          {channel.hasToken && <span>· token</span>}
          {channel.hasSecret && <span>· HMAC</span>}
          {channel.headerKeys && channel.headerKeys.length > 0 && (
            <span>· {channel.headerKeys.length} header(s)</span>
          )}
        </div>
      </div>
      <Button variant="ghost" size="sm" onClick={test} disabled={busy} title="Send a test notification">
        {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Zap className="size-3.5" />}
        Test
      </Button>
      <ChannelDialog channel={channel} onSaved={onChanged} />
      <Switch checked={channel.enabled} onCheckedChange={toggle} disabled={busy} />
      <Button variant="ghost" size="icon" onClick={remove} disabled={busy} title="Delete channel">
        <Trash2 className="size-4" />
      </Button>
    </div>
  );
}

// ---- channel dialog (add / edit) -------------------------------------------

interface HeaderRow {
  key: string;
  value: string;
}

function ChannelDialog({
  channel,
  onSaved,
}: {
  channel?: NotifyChannel;
  onSaved: () => void;
}) {
  const isEdit = !!channel;
  const { catalog, activeAgentId, activeDaemon } = useApp();
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [advanced, setAdvanced] = useState(false);

  const [type, setType] = useState<NotifyChannelType>(channel?.type ?? "slack");
  const [label, setLabel] = useState(channel?.label ?? "");
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [secret, setSecret] = useState("");
  const [headers, setHeaders] = useState<HeaderRow[]>(
    (channel?.headerKeys ?? []).map((k) => ({ key: k, value: "" })),
  );
  // Create-only routing (the "fully managed" part): which events fire this channel, and who it covers.
  const [conditions, setConditions] = useState<Condition[]>([]);
  const [applyTo, setApplyTo] = useState<"agent" | "all">("agent");

  // Which agent "This agent" means — the pinned agent, else the runtime's default. Empty when the
  // runtime has no default, in which case only "All agents" is offered.
  const agentId = activeAgentId ?? activeDaemon?.agent ?? "";
  const agentName = catalog?.agents.find((a) => a.id === agentId)?.name ?? agentId;
  const runtimeLabel = activeDaemon?.label ?? "this runtime";

  function reset() {
    setType(channel?.type ?? "slack");
    setLabel(channel?.label ?? "");
    setUrl("");
    setToken("");
    setSecret("");
    setHeaders((channel?.headerKeys ?? []).map((k) => ({ key: k, value: "" })));
    setConditions([]);
    setApplyTo(agentId ? "agent" : "all");
    // Reveal Advanced up-front when editing a channel that already carries auth/headers.
    setAdvanced(
      isEdit &&
        (!!channel?.hasToken || !!channel?.hasSecret || (channel?.headerKeys?.length ?? 0) > 0),
    );
  }

  function toggleCondition(c: Condition) {
    setConditions((cs) => (cs.includes(c) ? cs.filter((x) => x !== c) : [...cs, c]));
  }

  async function save() {
    if (!isEdit && !url.trim()) return toast.error("Paste the destination URL.");
    // Build the write body. Blank url/token/secret are OMITTED so a stored value is kept on edit;
    // headers are sent only when the user actually entered a value (a blank value would clear it).
    const filledHeaders = headers.filter((h) => h.key.trim() && h.value.trim());
    const input: NotifyChannelInput = {
      type,
      label: label.trim(),
      ...(url.trim() ? { url: url.trim() } : {}),
      ...(token.trim() ? { token: token.trim() } : {}),
      ...(type === "webhook" && secret.trim() ? { secret: secret.trim() } : {}),
      ...(filledHeaders.length
        ? { headers: Object.fromEntries(filledHeaders.map((h) => [h.key.trim(), h.value.trim()])) }
        : {}),
    };
    setSaving(true);
    try {
      if (isEdit) {
        await api.notify.setChannel(channel.id, input);
      } else {
        // Create the channel, then wire the routing rule in the same step — scoped to this agent
        // (default) or the whole runtime. The user never has to make a separate trip to the Rules tab.
        const created = await api.notify.createChannel(input);
        const useAgent = applyTo === "agent" && !!agentId;
        await api.notify.createRule({
          scope: useAgent ? "agent" : "global",
          ...(useAgent ? { agent: agentId } : {}),
          ...(conditions.length ? { conditions } : {}),
          channelIds: [created.id],
          enabled: true,
        });
      }
      onSaved();
      toast.success(isEdit ? "Channel saved" : "Channel added — routing is live");
      setOpen(false);
      if (!isEdit) reset();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save");
    } finally {
      setSaving(false);
    }
  }

  const meta = typeMeta(type);

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (o) reset();
      }}
    >
      {isEdit ? (
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
          Edit
        </Button>
      ) : (
        <Button size="sm" onClick={() => setOpen(true)}>
          <Plus className="size-4" />
          Add channel
        </Button>
      )}
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit channel" : "Add a channel"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "Update where this channel delivers. Blank secret fields keep what's stored."
              : "Pick where alerts land, which events fire them, and who they cover — wired up in one step."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          {/* 1 · destination — brand tiles */}
          <div className="space-y-2">
            <Label>Destination</Label>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {CHANNEL_TYPES.map((t) => {
                const on = type === t.value;
                const locked = isEdit && t.value !== channel.type;
                return (
                  <button
                    key={t.value}
                    type="button"
                    onClick={() => setType(t.value)}
                    aria-pressed={on}
                    disabled={locked}
                    className={cn(
                      "relative flex flex-col items-center gap-2 border px-2 py-3 text-xs transition-colors",
                      on
                        ? "border-ink/40 bg-accent text-foreground"
                        : "border-border text-muted-foreground hover:border-ink/25",
                      locked && "cursor-not-allowed opacity-40",
                    )}
                  >
                    {on && <Check className="absolute right-1.5 top-1.5 size-3 text-foreground" />}
                    <ChannelIcon type={t.value} className="size-6" />
                    {t.label}
                  </button>
                );
              })}
            </div>
            <p className="text-xs text-muted-foreground">{meta.hint}</p>
          </div>

          {/* 2 · connection */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="ch-url">{isEdit && channel.hasUrl ? "URL (stored)" : "URL"}</Label>
              <Input
                id="ch-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder={
                  isEdit && channel.hasUrl
                    ? `•••• ${channel.urlHost ?? ""} — blank to keep`
                    : meta.urlPlaceholder
                }
                spellCheck={false}
                autoComplete="off"
              />
              <p className="text-xs text-muted-foreground">
                {meta.urlHelp} Write-only — stored on the runtime.
              </p>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="ch-label">
                Label <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="ch-label"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder={`${meta.label} · ${runtimeLabel}`}
                spellCheck={false}
                autoComplete="off"
              />
            </div>

            {/* advanced disclosure — auth token / signing secret / headers */}
            <button
              type="button"
              onClick={() => setAdvanced((v) => !v)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
            >
              <ChevronDown className={cn("size-3.5 transition-transform", !advanced && "-rotate-90")} />
              Advanced — auth token{type === "webhook" ? ", signing secret" : ""}, headers
            </button>
            {advanced && (
              <div className="space-y-3 border-l border-border pl-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="ch-token">
                      Bearer token {isEdit && channel.hasToken && "(stored)"}
                    </Label>
                    <Input
                      id="ch-token"
                      type="password"
                      value={token}
                      onChange={(e) => setToken(e.target.value)}
                      placeholder={isEdit && channel.hasToken ? "•••••••• — blank to keep" : "optional"}
                      spellCheck={false}
                      autoComplete="off"
                    />
                  </div>
                  {type === "webhook" && (
                    <div className="space-y-1.5">
                      <Label htmlFor="ch-secret">
                        HMAC secret {isEdit && channel.hasSecret && "(stored)"}
                      </Label>
                      <Input
                        id="ch-secret"
                        type="password"
                        value={secret}
                        onChange={(e) => setSecret(e.target.value)}
                        placeholder={
                          isEdit && channel.hasSecret ? "•••••••• — blank to keep" : "optional"
                        }
                        spellCheck={false}
                        autoComplete="off"
                      />
                      <p className="text-[11px] text-muted-foreground">
                        Signs each POST as <span className="font-mono">X-Mindwire-Signature</span>.
                      </p>
                    </div>
                  )}
                </div>
                <div className="space-y-2">
                  <Label>Extra headers</Label>
                  {isEdit && (channel.headerKeys?.length ?? 0) > 0 && (
                    <p className="text-[11px] text-muted-foreground">
                      Values are write-only — re-enter a value to keep or rotate a header.
                    </p>
                  )}
                  {headers.map((h, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <Input
                        value={h.key}
                        onChange={(e) =>
                          setHeaders((hs) =>
                            hs.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)),
                          )
                        }
                        placeholder="X-Custom-Header"
                        spellCheck={false}
                        autoComplete="off"
                        className="flex-1"
                      />
                      <Input
                        value={h.value}
                        onChange={(e) =>
                          setHeaders((hs) =>
                            hs.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)),
                          )
                        }
                        placeholder="value"
                        spellCheck={false}
                        autoComplete="off"
                        className="flex-1"
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setHeaders((hs) => hs.filter((_, j) => j !== i))}
                      >
                        <X className="size-4" />
                      </Button>
                    </div>
                  ))}
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setHeaders((hs) => [...hs, { key: "", value: "" }])}
                  >
                    <Plus className="size-3.5" />
                    Add header
                  </Button>
                </div>
              </div>
            )}
          </div>

          {/* 3 · events + 4 · scope — create only (the managed routing) */}
          {!isEdit && (
            <>
              <div className="space-y-2">
                <Label>Notify me when</Label>
                <div className="flex flex-wrap gap-2">
                  <ChipToggle on={conditions.length === 0} onClick={() => setConditions([])}>
                    Anything happens
                  </ChipToggle>
                  {CONDITIONS.map((c) => (
                    <ChipToggle
                      key={c.value}
                      on={conditions.includes(c.value)}
                      onClick={() => toggleCondition(c.value)}
                    >
                      {c.label}
                    </ChipToggle>
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <Label>Apply to</Label>
                <div className="grid gap-2 sm:grid-cols-2">
                  <ApplyTile
                    on={applyTo === "agent"}
                    disabled={!agentId}
                    onClick={() => agentId && setApplyTo("agent")}
                    icon={
                      agentId ? (
                        <AgentIcon agentId={agentId} className="size-4" />
                      ) : (
                        <Bot className="size-4 text-muted-foreground" />
                      )
                    }
                    title={agentId ? agentName : "This agent"}
                    subtitle={
                      agentId ? `only ${agentName} on ${runtimeLabel}` : "no default agent here"
                    }
                  />
                  <ApplyTile
                    on={applyTo === "all"}
                    onClick={() => setApplyTo("all")}
                    icon={<Globe className="size-4 text-muted-foreground" />}
                    title="All agents"
                    subtitle={`everything on ${runtimeLabel}`}
                  />
                </div>
              </div>
            </>
          )}
        </div>

        <DialogFooter>
          <Button onClick={save} disabled={saving}>
            {saving && <Loader2 className="size-4 animate-spin" />}
            {isEdit ? "Save" : "Add channel"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// A small toggle chip — used for event selection in the managed add flow.
function ChipToggle({
  on,
  onClick,
  children,
}: {
  on: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={on}
      className={cn(
        "border px-2.5 py-1 text-xs transition-colors",
        on
          ? "border-ink/30 bg-accent text-foreground"
          : "border-border text-muted-foreground hover:border-ink/25",
      )}
    >
      {children}
    </button>
  );
}

// A scope tile — "this agent" vs "all agents on this runtime".
function ApplyTile({
  on,
  disabled,
  onClick,
  icon,
  title,
  subtitle,
}: {
  on: boolean;
  disabled?: boolean;
  onClick: () => void;
  icon: ReactNode;
  title: string;
  subtitle: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={on}
      className={cn(
        "flex items-start gap-2.5 border p-3 text-left transition-colors",
        on ? "border-ink/40 bg-accent" : "border-border hover:border-ink/25",
        disabled && "cursor-not-allowed opacity-40",
      )}
    >
      <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center border border-border">
        {icon}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium">{title}</span>
        <span className="block truncate text-[11px] text-muted-foreground">{subtitle}</span>
      </span>
    </button>
  );
}

// ---- rule row --------------------------------------------------------------

function RuleRow({
  rule,
  channels,
  onChanged,
}: {
  rule: NotifyRule;
  channels: NotifyChannel[];
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);

  const target =
    rule.scope === "global"
      ? "All events"
      : rule.scope === "agent"
        ? `Agent · ${rule.agent}`
        : `Session · ${(rule.session ?? "").slice(0, 8)}`;

  const targetChannels = rule.channelIds
    .map((id) => channels.find((c) => c.id === id))
    .filter((c): c is NotifyChannel => !!c);

  async function toggle(enabled: boolean) {
    setBusy(true);
    try {
      // Rules are replaced wholesale on PUT (no merge) — resend the whole rule with the flipped flag.
      await api.notify.setRule(rule.id, {
        scope: rule.scope,
        agent: rule.agent,
        session: rule.session,
        conditions: rule.conditions,
        channelIds: rule.channelIds,
        enabled,
      });
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not update");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await api.notify.deleteRule(rule.id);
      onChanged();
      toast.success("Rule deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-3 border border-border bg-card px-3 py-2.5">
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline">{target}</Badge>
          {(rule.conditions && rule.conditions.length > 0 ? rule.conditions : (["*"] as const)).map(
            (c) => (
              <Badge key={c} variant="muted">
                {c === "*" ? "all events" : conditionLabel(c as Condition)}
              </Badge>
            ),
          )}
        </div>
        <p className="text-[11px] text-muted-foreground">
          →{" "}
          {targetChannels.length
            ? targetChannels.map((c) => c.label || typeMeta(c.type).label).join(", ")
            : `${rule.channelIds.length} channel(s)`}
        </p>
      </div>
      <RuleDialog rule={rule} channels={channels} onSaved={onChanged} />
      <Switch checked={rule.enabled} onCheckedChange={toggle} disabled={busy} />
      <Button variant="ghost" size="icon" onClick={remove} disabled={busy} title="Delete rule">
        <Trash2 className="size-4" />
      </Button>
    </div>
  );
}

// ---- rule dialog (add / edit) ----------------------------------------------

function RuleDialog({
  rule,
  channels,
  onSaved,
}: {
  rule?: NotifyRule;
  channels: NotifyChannel[];
  onSaved: () => void;
}) {
  const isEdit = !!rule;
  const { catalog, activeAgentId, chats } = useApp();
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  const [scope, setScope] = useState<NotifyRuleScope>(rule?.scope ?? "global");
  const [agent, setAgent] = useState(rule?.agent ?? activeAgentId ?? "");
  const [session, setSession] = useState(rule?.session ?? "");
  const [conditions, setConditions] = useState<Condition[]>(rule?.conditions ?? []);
  const [channelIds, setChannelIds] = useState<string[]>(rule?.channelIds ?? []);

  function reset() {
    setScope(rule?.scope ?? "global");
    setAgent(rule?.agent ?? activeAgentId ?? "");
    setSession(rule?.session ?? "");
    setConditions(rule?.conditions ?? []);
    setChannelIds(rule?.channelIds ?? []);
  }

  const agentOptions = useMemo(() => catalog?.agents ?? [], [catalog]);

  function toggleCondition(c: Condition) {
    setConditions((cs) => (cs.includes(c) ? cs.filter((x) => x !== c) : [...cs, c]));
  }
  function toggleChannel(id: string) {
    setChannelIds((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]));
  }

  async function save() {
    if (scope === "agent" && !agent.trim()) return toast.error("Pick an agent for an agent-scoped rule.");
    if (scope === "session" && !session.trim())
      return toast.error("Enter a session (chat) id for a session-scoped rule.");
    if (channelIds.length === 0) return toast.error("Select at least one channel.");

    const input: NotifyRuleInput = {
      scope,
      ...(scope === "agent" ? { agent: agent.trim() } : {}),
      ...(scope === "session" ? { session: session.trim() } : {}),
      ...(conditions.length ? { conditions } : {}),
      channelIds,
      enabled: rule?.enabled ?? true,
    };
    setSaving(true);
    try {
      if (isEdit) await api.notify.setRule(rule.id, input);
      else await api.notify.createRule(input);
      onSaved();
      toast.success(isEdit ? "Rule saved" : "Rule added");
      setOpen(false);
      if (!isEdit) reset();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (o) reset();
      }}
    >
      {isEdit ? (
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
          Edit
        </Button>
      ) : (
        <Button size="sm" disabled={channels.length === 0} onClick={() => setOpen(true)}>
          <Plus className="size-4" />
          Add rule
        </Button>
      )}
      <DialogContent className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit rule" : "Add a rule"}</DialogTitle>
          <DialogDescription>
            Route matching notifications to one or more channels.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="rule-scope">Scope</Label>
            <Select value={scope} onValueChange={(v) => setScope(v as NotifyRuleScope)}>
              <SelectTrigger id="rule-scope">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SCOPES.map((s) => (
                  <SelectItem key={s.value} value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {SCOPES.find((s) => s.value === scope)?.hint}
            </p>
          </div>

          {scope === "agent" && (
            <div className="space-y-1.5">
              <Label htmlFor="rule-agent">Agent</Label>
              {agentOptions.length > 0 ? (
                <Select value={agent} onValueChange={setAgent}>
                  <SelectTrigger id="rule-agent">
                    <SelectValue placeholder="Pick an adapter" />
                  </SelectTrigger>
                  <SelectContent>
                    {agentOptions.map((a) => (
                      <SelectItem key={a.id} value={a.id}>
                        {a.name} ({a.id})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id="rule-agent"
                  value={agent}
                  onChange={(e) => setAgent(e.target.value)}
                  placeholder="claude-code"
                  spellCheck={false}
                  autoComplete="off"
                />
              )}
            </div>
          )}

          {scope === "session" && (
            <div className="space-y-1.5">
              <Label htmlFor="rule-session">Session (chat) id</Label>
              <Input
                id="rule-session"
                list="rule-session-list"
                value={session}
                onChange={(e) => setSession(e.target.value)}
                placeholder="chat id"
                spellCheck={false}
                autoComplete="off"
              />
              <datalist id="rule-session-list">
                {chats.map((ch) => (
                  <option key={ch.chatId} value={ch.chatId}>
                    {ch.title}
                  </option>
                ))}
              </datalist>
            </div>
          )}

          <div className="space-y-2">
            <Label>Events</Label>
            <p className="text-xs text-muted-foreground">None selected → all events.</p>
            <div className="flex flex-wrap gap-2">
              {CONDITIONS.map((c) => {
                const on = conditions.includes(c.value);
                return (
                  <button
                    key={c.value}
                    type="button"
                    onClick={() => toggleCondition(c.value)}
                    className={cn(
                      "border px-2.5 py-1 text-xs transition-colors",
                      on ? "border-ink/30 bg-accent" : "border-border text-muted-foreground hover:border-ink/25",
                    )}
                  >
                    {c.label}
                  </button>
                );
              })}
            </div>
          </div>

          <div className="space-y-2">
            <Label>Channels</Label>
            {channels.length === 0 ? (
              <p className="text-xs text-muted-foreground">No channels — add one first.</p>
            ) : (
              <div className="flex flex-wrap gap-2">
                {channels.map((c) => {
                  const on = channelIds.includes(c.id);
                  return (
                    <button
                      key={c.id}
                      type="button"
                      onClick={() => toggleChannel(c.id)}
                      className={cn(
                        "flex items-center gap-1.5 border px-2.5 py-1 text-xs transition-colors",
                        on ? "border-ink/30 bg-accent" : "border-border text-muted-foreground hover:border-ink/25",
                      )}
                    >
                      <ChannelIcon type={c.type} className="size-3" />
                      {c.label || typeMeta(c.type).label}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button onClick={save} disabled={saving}>
            {saving && <Loader2 className="size-4 animate-spin" />}
            {isEdit ? "Save" : "Add rule"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
