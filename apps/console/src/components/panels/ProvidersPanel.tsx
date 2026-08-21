// The Providers surface — a marketplace over the whole models.dev catalog. The catalog is the SDK's
// live reference list ("which providers and models exist"), fetched daemon-independently, so every
// provider shows here regardless of which agent is selected or whether a runtime is up. A provider has a
// SINGLE connection that unlocks ALL of its models — so this surface is about connecting a provider, not
// picking a model (that happens in Models). Two connect flows, for agents with a provider registry
// (opencode, Codex):
//   • CONNECT a catalog brand (Google, AWS Bedrock, …) by filling in the credential(s) it declares — NO
//     base URL. The catalog names each env var a provider reads (`detail.env[]`); we render one field per
//     credential the provider actually needs — three for Bedrock (access key + secret + region, all
//     required), but ONE for Google, whose several `*_API_KEY` names are interchangeable aliases (any one
//     authenticates), collapsed to a single "API key" input. The harness already knows the provider's
//     built-in models; we relay each value under its env var and the models become usable in Models. Many
//     providers connect independently (each stored under its own namespaced key — no single-slot overwrite).
//   • "Custom endpoint" registers an OpenAI-compatible provider the catalog has never heard of (needs a
//     base URL + model ids + one key; writes the harness's provider block).
//
// SHARED ACROSS AGENTS: a connection is stored once in a cross-agent namespace, so connecting a provider
// makes it available to EVERY agent that supports it — connect Google under opencode and Codex sees it
// Connected too. (Custom endpoints share the key, but each agent still needs its own endpoint block
// written, since the config-file syntaxes differ.)
//
// THIS IS THE ONE PLACE A CONNECTION IS MANAGED, so it has to reach EVERY stored connection — not just
// the ones that happen to be catalog brands. The rail therefore leads with "Your connections": every
// provider the daemon reports as stored whose id isn't a plain catalog entry (a custom endpoint, or a
// brand the catalog has since dropped). Without that section those connections had no row to click, so
// the key could be neither edited nor removed from anywhere in the console. `/providers/:id` opens
// straight onto one provider, which is where the Models page's "Manage auth" lands.
//
// SECURITY: credentials are write-only. The server reports only `hasKey` and the stored env-var NAMES
// (`envVars`), never the values; values are sent solely on connect, stored daemon-side, and referenced by
// their named env vars. A blank field on a var that already has a stored value leaves it untouched.
import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { Boxes, ExternalLink, Loader2, Plug, Search, Trash2 } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { compact } from "@/lib/format";
import { Panel } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { ScopeToggle } from "@/components/common/ScopeToggle";
import { ProviderLogo } from "@/components/ProviderLogo";
import {
  TwoPane,
  RailNew,
  RailStatus,
  RailList,
  RailItem,
  FormCard,
  FieldGroup,
  TwoCol,
  Field,
  FormActions,
  EmptyPane,
} from "@/components/common/config-ui";
import { toast } from "@/components/ui/sonner";
import type {
  CatalogProvider,
  CatalogProviderSummary,
  CustomProvider,
  MemoryScope,
} from "@shared/api";

/** A blank draft for the "custom OpenAI-compatible endpoint" form (providers not in the catalog). */
interface CustomDraft {
  /** True when editing a provider that already exists: the id is fixed (changing it would create a
   *  second provider and orphan this one) and Disconnect becomes available. */
  editing?: boolean;
  id: string;
  name: string;
  baseUrl: string;
  models: string;
  envVar: string;
  apiKey: string;
}
const EMPTY_CUSTOM: CustomDraft = {
  id: "",
  name: "",
  baseUrl: "",
  models: "",
  envVar: "",
  apiKey: "",
};

/**
 * Whether a catalog env var should be masked as a secret. models.dev mixes true secrets (API keys, access
 * keys, tokens) with plain routing values (regions, project ids, resource names) in the same `env[]`, and
 * masking a region as a password is confusing. Anything on the plain allow-list renders as text; everything
 * else defaults to masked — safer to hide an unknown value than to leak a real key in the clear.
 */
const PLAIN_VAR_HINTS = ["REGION", "PROJECT", "LOCATION", "RESOURCE", "ENDPOINT", "HOST", "BASE", "ZONE", "ACCOUNT"];
function isSecretVar(name: string): boolean {
  const n = name.toUpperCase();
  return !PLAIN_VAR_HINTS.some((p) => n.includes(p));
}

/**
 * The subset of a provider's declared env vars that are interchangeable API-key aliases — names ending in
 * `_API_KEY` (e.g. Google's GOOGLE_GENERATIVE_AI_API_KEY / GOOGLE_API_KEY / GEMINI_API_KEY). models.dev
 * lists every accepted var with no "any-of vs all-of" flag, but a provider offering MULTIPLE `*_API_KEY`
 * names accepts ANY ONE — supplying one authenticates. Heterogeneous credential sets (AWS Bedrock's
 * ACCESS_KEY_ID + SECRET_ACCESS_KEY + REGION) don't all end in `_API_KEY`, so they're never collapsed and
 * still render as the separate fields they genuinely require.
 */
function apiKeyAliases(env: string[]): string[] {
  return env.filter((n) => /_API_KEY$/i.test(n));
}

/** Seed the endpoint form from a provider the daemon already stores. The API key is deliberately absent:
 *  it is write-only and never returned, so a blank field means "keep the stored value". */
function draftFromStored(p: CustomProvider): CustomDraft {
  return {
    editing: true,
    id: p.id,
    name: p.name ?? "",
    baseUrl: p.baseUrl ?? "",
    models: (p.models ?? []).join("\n"),
    envVar: p.envVar ?? "",
    apiKey: "",
  };
}

/** The env-var NAMES a stored provider currently holds a value for (multi-var `envVars`, else the single `envVar`). */
function storedVarNames(p?: CustomProvider): string[] {
  if (!p) return [];
  if (p.envVars && p.envVars.length > 0) return p.envVars;
  return p.envVar ? [p.envVar] : [];
}

export function ProvidersPanel() {
  const { agent } = useApp();
  // `/providers/:providerId` opens straight onto one provider (see the route in AppShell).
  const { providerId } = useParams<{ providerId?: string }>();
  // Only agents with a provider registry (opencode's opencode.json, Codex's config.toml) can hold a
  // relayed key; Claude authenticates through its own login lane (see Agent auth), so for it the catalog
  // is browse-only.
  const canStoreKeys = agent?.capabilities.customProviders ?? false;

  // The scope a connection is stored under. Provider registries are user-only today (opencode, Codex),
  // and connecting at a scope the agent doesn't support 400s — so we don't guess. `null` until the
  // agent's supported scopes load, then defaulted to a valid one (below).
  const [scope, setScope] = useState<MemoryScope | null>(null);
  const scopesQ = useAsync<MemoryScope[]>(
    () => (canStoreKeys ? api.providerScopes() : Promise.resolve([])),
    [canStoreKeys],
  );
  const supportedScopes = scopesQ.data ?? [];

  // Land on a supported scope once the agent reports them: prefer `project` when offered (parity with the
  // memory/MCP surfaces), else the first supported (user, for opencode/Codex). Re-defaults if the current
  // pick isn't valid for the newly selected agent.
  useEffect(() => {
    if (supportedScopes.length === 0) return;
    if (scope && supportedScopes.includes(scope)) return;
    setScope(supportedScopes.includes("project") ? "project" : supportedScopes[0]);
  }, [supportedScopes, scope]);

  // The catalog is daemon-independent reference data — no scope/agent deps, fetched once.
  const catalogQ = useAsync<CatalogProviderSummary[]>(() => api.catalogProviders(), []);
  // Which providers are connected. Because connections are shared across agents, this is the same set for
  // any provider-capable agent — it just needs one to service the call (empty when the agent has no
  // registry or before a scope is resolved).
  const storedQ = useAsync<CustomProvider[]>(
    () => (canStoreKeys && scope ? api.providers({ scope }) : Promise.resolve([])),
    [scope, canStoreKeys],
  );

  const agentName = agent?.name ?? agent?.agentType ?? "the agent";

  const [filter, setFilter] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [customDraft, setCustomDraft] = useState<CustomDraft | null>(null);

  // Selected catalog provider's full model set + declared env vars, fetched on demand (the list carries
  // only counts).
  const detailQ = useAsync<CatalogProvider | null>(
    () => (selectedId ? api.catalogProvider(selectedId) : Promise.resolve(null)),
    [selectedId],
  );

  // Connect values keyed by the catalog's env-var NAME — one entry per var the selected provider declares.
  // Blank/absent means "not supplied" (leave any stored value intact). Never carried across providers.
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setFields({});
  }, [selectedId]);

  const setField = (name: string, value: string) => setFields((f) => ({ ...f, [name]: value }));

  const providers = catalogQ.data ?? [];
  const stored = storedQ.data ?? [];
  const storedFor = (id: string) => stored.find((s) => s.id === id);

  const filtered = useMemo(() => {
    const f = filter.trim().toLowerCase();
    if (!f) return providers;
    return providers.filter(
      (p) => p.id.toLowerCase().includes(f) || p.name.toLowerCase().includes(f),
    );
  }, [providers, filter]);

  // Catalog display names, so a stored connection to a known brand reads "Google", not "google". A
  // stored id the catalog doesn't carry falls back to the id.
  const catalogNames = useMemo(
    () => new Map(providers.map((p) => [p.id, p.name] as const)),
    [providers],
  );
  const displayName = (p: CustomProvider) => p.name || catalogNames.get(p.id) || p.id;

  // EVERY stored connection, pinned above the 190-odd-row catalog. Two kinds land here and both need it:
  // a custom endpoint or an off-catalog id has no catalog row at all (these were previously unreachable),
  // and a catalog brand connected by key does have one — a thousand rows down an alphabetical list. This
  // is the answer to "what have I connected?", so it lists the lot rather than only the leftovers.
  const yours = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return stored
      .filter((s) => !f || s.id.toLowerCase().includes(f) || (s.name ?? "").toLowerCase().includes(f))
      .sort((a, b) => (a.name ?? a.id).localeCompare(b.name ?? b.id));
  }, [stored, filter]);

  // Open a provider: a stored ENDPOINT edits its own form (base URL, models, key), anything else gets the
  // catalog connect pane. `openProvider` is the single entry point so the rail, the deep link and the
  // Models hand-off all land on the same pane for the same id.
  function openProvider(id: string) {
    const p = stored.find((s) => s.id === id);
    setCustomDraft(p && (p.baseUrl ?? "").trim() !== "" ? draftFromStored(p) : null);
    setSelectedId(id);
  }
  function startCustom() {
    setSelectedId(null);
    setCustomDraft({ ...EMPTY_CUSTOM });
  }

  // Apply the route param once the stored list has loaded — which pane a deep link opens depends on
  // whether that id is an endpoint, so acting before then would flash the wrong form. The ref makes this
  // a one-shot per id: after it lands, clicking around the rail must not be yanked back by the URL.
  const appliedParam = useRef<string | null>(null);
  useEffect(() => {
    if (!providerId) {
      appliedParam.current = null;
      return;
    }
    if (storedQ.loading || appliedParam.current === providerId) return;
    appliedParam.current = providerId;
    openProvider(providerId);
    // openProvider is re-created each render; the ref guard is what keeps this from re-running.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [providerId, storedQ.loading, stored]);

  // Connect a CATALOG provider by its declared credential(s) — no base URL. The harness already defines the
  // provider's built-in models; we relay each value under the env var the catalog names, so those models
  // become usable. Env-only registration: the daemon stores one namespaced entry per var in the shared
  // cross-agent namespace and writes no config block. Blank fields are omitted (kept as-is when stored).
  async function connectCatalog(detail: CatalogProvider) {
    const existing = storedFor(detail.id);
    const secrets: Record<string, string> = {};
    for (const name of detail.env) {
      const v = (fields[name] ?? "").trim();
      if (v) secrets[name] = v;
    }
    if (Object.keys(secrets).length === 0) {
      return toast.error(
        existing?.hasKey
          ? "Nothing to update — enter a new value to change a credential."
          : "Enter the provider's credentials to connect it.",
      );
    }
    setBusy(true);
    try {
      await api.setProvider(
        detail.id,
        { baseUrl: "", models: [], ...(detail.name ? { name: detail.name } : {}) },
        undefined,
        secrets,
        scope ? { scope } : undefined,
      );
      setFields({});
      storedQ.reload();
      toast.success(
        existing?.hasKey ? `Credentials updated for ${detail.name}` : `Connected ${detail.name}`,
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not connect the provider");
    } finally {
      setBusy(false);
    }
  }

  async function disconnect(id: string, name: string) {
    setBusy(true);
    try {
      await api.deleteProvider(id, scope ? { scope } : undefined);
      setFields({});
      storedQ.reload();
      toast.success(`Disconnected ${name}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not disconnect the provider");
    } finally {
      setBusy(false);
    }
  }

  async function saveCustom() {
    if (!customDraft) return;
    const id = customDraft.id.trim();
    if (!id) return toast.error("A provider id is required.");
    if (!customDraft.baseUrl.trim()) return toast.error("A base URL is required.");
    const models = customDraft.models.split("\n").map((s) => s.trim()).filter(Boolean);
    setBusy(true);
    try {
      await api.setProvider(
        id,
        {
          baseUrl: customDraft.baseUrl.trim(),
          models,
          ...(customDraft.name.trim() ? { name: customDraft.name.trim() } : {}),
          ...(customDraft.envVar.trim() ? { envVar: customDraft.envVar.trim() } : {}),
        },
        customDraft.apiKey.trim() || undefined,
        undefined,
        scope ? { scope } : undefined,
      );
      storedQ.reload();
      // An edit stays on the provider it just saved (with the key field cleared again — it is write-only);
      // a brand-new one closes back to the rail. Clearing the draft on an edit would drop the pane through
      // to the catalog form, which is not what this provider is.
      setCustomDraft(customDraft.editing ? { ...customDraft, apiKey: "" } : null);
      setSelectedId(customDraft.editing ? id : null);
      toast.success(customDraft.editing ? "Provider updated" : "Provider saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  const setCustom = <K extends keyof CustomDraft>(k: K, v: CustomDraft[K]) =>
    setCustomDraft((d) => (d ? { ...d, [k]: v } : d));

  const detail = detailQ.data ?? null;

  return (
    <Panel
      title="Providers"
      description="Connect a provider once — its credentials are shared across every agent that supports it. Pick a model to run over in Models."
      actions={
        canStoreKeys && scope && supportedScopes.length > 1 ? (
          <ScopeToggle scope={scope} scopes={supportedScopes} onChange={setScope} />
        ) : undefined
      }
      fill
      contentClassName="mx-auto flex w-full max-w-4xl flex-col px-6 py-6"
    >
      <TwoPane
        fill
        rail={
          <>
            {canStoreKeys && <RailNew label="Custom endpoint" onClick={startCustom} />}
            <div className="relative">
              <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={
                  providers.length ? `Filter ${providers.length} providers…` : "Filter providers…"
                }
                className="h-8 pl-8 text-xs"
              />
            </div>
            {/* Every connection this account has made, first: it is the shortest answer to "what am I
                connected to?", and for an off-catalog id or a custom endpoint it is the ONLY row that
                can reach the pane. Catalog rows below repeat the brand ones, marked the same way. */}
            {yours.length > 0 && (
              <div className="space-y-1.5">
                <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Your connections
                </p>
                <RailList>
                  {yours.map((p) => (
                    <RailItem
                      key={p.id}
                      active={selectedId === p.id}
                      onClick={() => openProvider(p.id)}
                      media={<ProviderLogo provider={p.id} className="size-4 text-ink" />}
                      title={displayName(p)}
                      subtitle={
                        (p.baseUrl ?? "").trim() !== ""
                          ? `${p.models?.length ?? 0} model${(p.models?.length ?? 0) === 1 ? "" : "s"} · custom endpoint`
                          : storedVarNames(p).join(", ") || "stored credential"
                      }
                      trailing={
                        p.hasKey ? (
                          <Badge variant="outline" className="gap-1 px-1.5">
                            <span className="size-1.5 rounded-full bg-ink" />
                            Connected
                          </Badge>
                        ) : undefined
                      }
                    />
                  ))}
                </RailList>
              </div>
            )}

            <RailStatus
              loading={catalogQ.loading}
              error={catalogQ.error}
              empty={!!catalogQ.data && filtered.length === 0}
              emptyText="No providers match."
            />
            {filtered.length > 0 && (
              <RailList className="min-h-0 flex-1 overflow-y-auto">
                {filtered.map((p) => (
                  <RailItem
                    key={p.id}
                    active={!customDraft && selectedId === p.id}
                    onClick={() => openProvider(p.id)}
                    media={<ProviderLogo provider={p.id} className="size-4 text-ink" />}
                    title={p.name}
                    subtitle={`${p.modelCount} model${p.modelCount === 1 ? "" : "s"}`}
                    trailing={
                      storedFor(p.id)?.hasKey ? (
                        <Badge variant="outline" className="gap-1 px-1.5">
                          <span className="size-1.5 rounded-full bg-ink" />
                          Connected
                        </Badge>
                      ) : undefined
                    }
                  />
                ))}
              </RailList>
            )}
          </>
        }
      >
        {customDraft ? (
          <FormCard
            media={<ProviderLogo provider={customDraft.id || "custom"} className="size-5 text-ink" />}
            title={customDraft.name || customDraft.id || "New provider"}
            subtitle={
              customDraft.editing
                ? `Custom OpenAI-compatible provider · ${customDraft.id}`
                : "Custom OpenAI-compatible provider"
            }
            footer={
              <FormActions
                saving={busy}
                onSave={saveCustom}
                saveLabel={customDraft.editing ? "Save changes" : "Save"}
                deletable={customDraft.editing}
                deleteLabel="Disconnect"
                onDelete={() => disconnect(customDraft.id, customDraft.name || customDraft.id)}
              />
            }
          >
            <FieldGroup title="Identity">
              <TwoCol>
                <Field
                  label="Id"
                  htmlFor="prov-id"
                  hint={customDraft.editing ? "Fixed — a new id would register a second provider." : undefined}
                >
                  <Input
                    id="prov-id"
                    value={customDraft.id}
                    onChange={(e) => setCustom("id", e.target.value)}
                    placeholder="my-llm"
                    spellCheck={false}
                    autoComplete="off"
                    disabled={customDraft.editing}
                  />
                </Field>
                <Field label="Display name (optional)" htmlFor="prov-name">
                  <Input
                    id="prov-name"
                    value={customDraft.name}
                    onChange={(e) => setCustom("name", e.target.value)}
                    placeholder="My LLM"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
              </TwoCol>
              <Field label="Base URL" htmlFor="prov-url">
                <Input
                  id="prov-url"
                  value={customDraft.baseUrl}
                  onChange={(e) => setCustom("baseUrl", e.target.value)}
                  placeholder="https://llm.example/v1"
                  spellCheck={false}
                  autoComplete="off"
                />
              </Field>
            </FieldGroup>

            <FieldGroup title="Models">
              <Field label="Model ids (one per line)" htmlFor="prov-models">
                <Textarea
                  id="prov-models"
                  value={customDraft.models}
                  onChange={(e) => setCustom("models", e.target.value)}
                  rows={4}
                  className="font-mono text-xs"
                  placeholder={"gpt-4o-mini\nllama-3.1-70b"}
                />
              </Field>
            </FieldGroup>

            <FieldGroup title="Authentication">
              <TwoCol>
                <Field label="Env var (optional)" htmlFor="prov-env">
                  <Input
                    id="prov-env"
                    value={customDraft.envVar}
                    onChange={(e) => setCustom("envVar", e.target.value)}
                    placeholder="MY_LLM_API_KEY"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
                <Field
                  label={`API key${customDraft.editing && storedFor(customDraft.id)?.hasKey ? " · stored" : ""}`}
                  htmlFor="prov-key"
                  hint="Write-only — stored on the server, never displayed. Referenced by the env var."
                >
                  <Input
                    id="prov-key"
                    type="password"
                    value={customDraft.apiKey}
                    onChange={(e) => setCustom("apiKey", e.target.value)}
                    placeholder={
                      customDraft.editing && storedFor(customDraft.id)?.hasKey
                        ? "•••••••• — leave blank to keep"
                        : "sk-…"
                    }
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
              </TwoCol>
            </FieldGroup>
          </FormCard>
        ) : !selectedId ? (
          <EmptyPane
            icon={<Boxes className="size-5" />}
            title="No provider selected"
            hint="Pick a provider to connect it, or add a custom OpenAI-compatible endpoint."
          />
        ) : detailQ.loading ? (
          <EmptyPane icon={<Boxes className="size-5" />} title="Loading…" hint="Fetching the provider's models." />
        ) : detailQ.error || !detail ? (
          // The catalog has no entry for this id. If the daemon nonetheless STORES a credential under it,
          // that credential is live in every run, so this pane has to offer the way out — otherwise the
          // only surface that can remove it renders an apology.
          storedFor(selectedId) ? (
            <StoredOnlyDetail
              stored={storedFor(selectedId)!}
              busy={busy}
              onRemove={() => disconnect(selectedId, storedFor(selectedId)?.name || selectedId)}
            />
          ) : (
            <EmptyPane
              icon={<Boxes className="size-5" />}
              title="Couldn't load this provider"
              hint={detailQ.error ?? "The catalog has no entry for it."}
            />
          )
        ) : (
          <ProviderDetail
            detail={detail}
            stored={storedFor(detail.id)}
            canStoreKeys={canStoreKeys}
            fields={fields}
            busy={busy}
            agentName={agentName}
            onField={setField}
            onSave={() => connectCatalog(detail)}
            onRemove={() => disconnect(detail.id, detail.name)}
          />
        )}
      </TwoPane>
    </Panel>
  );
}

/** The pane for a stored connection the catalog has no entry for. There is nothing to connect here — the
 *  credential already exists — so this reports what is held (env-var NAMES only, never values) and offers
 *  the removal that would otherwise be impossible from anywhere in the console. */
function StoredOnlyDetail({
  stored,
  busy,
  onRemove,
}: {
  stored: CustomProvider;
  busy: boolean;
  onRemove: () => void;
}) {
  const names = storedVarNames(stored);
  return (
    <FormCard
      media={<ProviderLogo provider={stored.id} className="size-5 text-ink" />}
      title={
        <span className="flex items-center gap-2">
          <span className="truncate">{stored.name || stored.id}</span>
          {stored.hasKey && (
            <span className="inline-flex shrink-0 items-center gap-1 border border-ink/25 bg-ink/[0.05] px-1.5 py-0.5 text-[10px] font-medium text-ink/70">
              <span className="size-1.5 rounded-full bg-ink" />
              Connected
            </span>
          )}
        </span>
      }
      subtitle={`Stored connection · ${stored.id}`}
    >
      <FieldGroup title="Credential">
        <p className="text-xs leading-relaxed text-muted-foreground">
          This provider isn’t in the catalog, so there’s no connect form for it — but a credential is
          stored under {names.length === 1 ? "this env var" : "these env vars"} and is exported into every
          run of any agent that supports providers.
        </p>
        <Field label={names.length > 1 ? "Env vars" : "Env var"}>
          <div className="truncate font-mono text-xs text-foreground">
            {names.length > 0 ? names.join(", ") : <span className="text-muted-foreground">— none —</span>}
          </div>
        </Field>
        <div className="flex items-center gap-2 pt-0.5">
          <Button
            size="sm"
            variant="ghost"
            onClick={onRemove}
            disabled={busy}
            className="text-muted-foreground hover:text-destructive"
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
            Disconnect
          </Button>
        </div>
      </FieldGroup>
    </FormCard>
  );
}

/** The right pane for a selected catalog provider: identity, its models (read-only — what the provider
 *  offers), and the connect control — one field per env var the catalog declares. Picking which model to
 *  run happens in the Models tab. */
function ProviderDetail({
  detail,
  stored,
  canStoreKeys,
  fields,
  busy,
  agentName,
  onField,
  onSave,
  onRemove,
}: {
  detail: CatalogProvider;
  stored?: CustomProvider;
  canStoreKeys: boolean;
  fields: Record<string, string>;
  busy: boolean;
  agentName: string;
  onField: (name: string, value: string) => void;
  onSave: () => void;
  onRemove: () => void;
}) {
  // Any registry-capable agent can relay credentials. For a catalog brand the harness already knows the
  // endpoint, so connecting is credentials-only — no base URL (that's the "Custom endpoint" flow).
  const relayable = canStoreKeys;
  const storedNames = new Set(storedVarNames(stored));
  const connectedLabel = storedVarNames(stored).join(", ");
  const keyless = detail.env.length === 0;

  // One input per credential the provider actually needs. Interchangeable API-key aliases collapse to a
  // single "API key" row and the value is stored under the FIRST alias; every other declared var keeps its
  // own field. `name` is the var each input writes to.
  //
  // The aliases are interchangeable to the HARNESS, not to the provider's SDK: models.dev's `env[]` is the
  // list opencode uses to DETECT a configured provider, but @ai-sdk/google (say) reads exactly
  // GOOGLE_GENERATIVE_AI_API_KEY — store the key under GOOGLE_API_KEY alone and opencode lists the Gemini
  // models and then fails the turn with "API key is missing". Picking the right name is therefore the
  // daemon's job, not this form's: opencode's EnvForRun exports a stored key under the canonical name too
  // (see canonicalProviderEnv), which is also what repairs keys already stored under the wrong alias.
  const aliases = apiKeyAliases(detail.env);
  const collapseKeys = aliases.length > 1;
  const specs: {
    name: string;
    label: string;
    hint: string;
    secret: boolean;
    stored: boolean;
  }[] = [];
  if (collapseKeys) {
    specs.push({
      name: aliases[0],
      label: "API key",
      hint: `Any one of ${aliases.join(", ")} — supplying one is enough. Write-only, stored on the server.`,
      secret: true,
      stored: aliases.some((a) => storedNames.has(a)),
    });
  }
  for (const name of detail.env) {
    if (collapseKeys && aliases.includes(name)) continue;
    const secret = isSecretVar(name);
    specs.push({
      name,
      label: name,
      hint: secret
        ? "Write-only — stored on the server, never displayed."
        : "Stored on the server and exported to the run.",
      secret,
      stored: storedNames.has(name),
    });
  }
  const single = specs.length === 1;

  return (
    <FormCard
      media={<ProviderLogo provider={detail.id} className="size-5 text-ink" />}
      title={
        <span className="flex items-center gap-2">
          <span className="truncate">{detail.name}</span>
          {stored?.hasKey && (
            <span className="inline-flex shrink-0 items-center gap-1 border border-ink/25 bg-ink/[0.05] px-1.5 py-0.5 text-[10px] font-medium text-ink/70">
              <span className="size-1.5 rounded-full bg-ink" />
              Connected{connectedLabel ? ` · ${connectedLabel}` : ""}
            </span>
          )}
        </span>
      }
      subtitle={`${detail.models.length} model${detail.models.length === 1 ? "" : "s"} · ${detail.id}`}
    >
      {/* The primary action comes FIRST. Selecting a provider must immediately offer the credential fields
          and a Connect button — never bury them under the (often 100+) model list, which is what stopped the
          connect step from being reachable. Reference info (catalog, models offered) follows below. */}
      <FieldGroup title={relayable ? "Connect" : "Authentication"}>
        {!canStoreKeys ? (
          <p className="text-xs leading-relaxed text-muted-foreground">
            {agentName} signs in through its own login lane — set it up under{" "}
            <span className="font-medium text-foreground">Agent auth</span>. Provider connections here
            apply to agents with a provider registry (Codex, opencode) and are shared across all of them.
          </p>
        ) : keyless ? (
          <p className="text-xs leading-relaxed text-muted-foreground">
            No credentials required — this provider runs locally (or needs no key), so it's already usable.
            Pick a model to run in <span className="font-medium text-foreground">Models</span>.
          </p>
        ) : (
          <>
            <p className="text-xs leading-relaxed text-muted-foreground">
              Fill in the credential{single ? "" : "s"} this provider needs — the harness already knows its
              endpoint and models. {single ? "It's" : "They're"} stored write-only and shared across every
              agent that supports this provider; its models then become selectable in{" "}
              <span className="font-medium text-foreground">Models</span>.
            </p>
            <div className="space-y-3">
              {specs.map((spec) => (
                <Field
                  key={spec.name}
                  label={`${spec.label}${spec.stored ? " · stored" : ""}`}
                  htmlFor={`cat-${spec.name}`}
                  hint={spec.hint}
                >
                  <Input
                    id={`cat-${spec.name}`}
                    type={spec.secret ? "password" : "text"}
                    value={fields[spec.name] ?? ""}
                    onChange={(e) => onField(spec.name, e.target.value)}
                    placeholder={spec.stored ? "•••••••• — leave blank to keep" : "Enter value"}
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
              ))}
            </div>
            <div className="flex items-center gap-2 pt-0.5">
              <Button size="sm" onClick={onSave} disabled={busy}>
                {busy ? <Loader2 className="size-4 animate-spin" /> : <Plug className="size-4" />}
                {stored?.hasKey ? "Update credentials" : "Connect provider"}
              </Button>
              {stored?.hasKey && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={onRemove}
                  disabled={busy}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="size-4" />
                  Disconnect
                </Button>
              )}
            </div>
          </>
        )}
      </FieldGroup>

      <FieldGroup title="Catalog">
        <TwoCol>
          <Field label="Published base URL">
            <div className="truncate font-mono text-xs text-foreground">
              {detail.api ?? <span className="text-muted-foreground">— built-in to the agent —</span>}
            </div>
          </Field>
          <Field label={detail.env.length > 1 ? "Env vars" : "Env var"}>
            <div className="truncate font-mono text-xs text-foreground">
              {detail.env.length > 0 ? (
                detail.env.join(", ")
              ) : (
                <span className="text-muted-foreground">— keyless / local —</span>
              )}
            </div>
          </Field>
        </TwoCol>
        {detail.doc && (
          <a
            href={detail.doc}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <ExternalLink className="size-3.5" />
            Provider docs
          </a>
        )}
      </FieldGroup>

      <FieldGroup title="Models it offers">
        <p className="text-xs leading-relaxed text-muted-foreground">
          {stored?.hasKey || keyless
            ? "These are usable now — pick one to run from the Models tab."
            : "Connecting this provider makes all of these usable. Pick which one to run from the Models tab."}
        </p>
        <div className="divide-y divide-border border border-border">
          {detail.models.map((m) => (
            <div key={m.id} className="flex items-center gap-3 px-3 py-2">
              <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-medium">{m.label}</p>
                <p className="truncate font-mono text-[11px] text-muted-foreground">{m.id}</p>
              </div>
              {m.contextWindow ? (
                <span className="hidden shrink-0 tabular-nums text-[11px] text-muted-foreground sm:inline">
                  {compact(m.contextWindow)} ctx
                </span>
              ) : null}
            </div>
          ))}
          {detail.models.length === 0 && (
            <p className="px-3 py-6 text-center text-xs text-muted-foreground">
              No models listed for this provider.
            </p>
          )}
        </div>
      </FieldGroup>
    </FormCard>
  );
}
