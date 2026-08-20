// The Settings surface — a form generated from the agent's declared settings schema (`GET /agent` →
// `schema`) bound to its current config (`GET /config`). Every field renders by its declared type with
// no native controls. Secrets are write-only: never pre-filled, sent only when the user types one.
import { useEffect, useMemo, useRef, useState } from "react";
import { Save, RotateCcw, Check, ChevronsUpDown } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { cn } from "@/lib/utils";
import { hasModelField } from "@/lib/model-config";
import { Panel, Spinner, ErrorNote, EmptyState } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import type { Field, ModelInfo } from "@shared/api";

export function SettingsPanel() {
  const { agent, agentLoading, agentError } = useApp();
  const configQ = useAsync<Record<string, string>>(() => api.getConfig());

  // The model field becomes a combobox backed by the SAME list the Models page shows — one source of
  // truth, both surfaces write the same config key, so picking here or there stays in lockstep. We only
  // fetch when this agent actually exposes a model field; agents that don't get no extra request.
  const wantsModels = hasModelField(agent?.schema);
  const modelsQ = useAsync<ModelInfo[]>(
    () => (wantsModels ? api.models() : Promise.resolve([])),
    [wantsModels],
  );

  const [values, setValues] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  // Seed editable values from config once it loads (secrets stay blank — write-only).
  const secretKeys = useMemo(() => {
    const set = new Set<string>();
    for (const s of agent?.schema.sections ?? []) {
      for (const f of s.fields) if (f.type === "secret") set.add(f.key);
    }
    return set;
  }, [agent]);

  useEffect(() => {
    if (!configQ.data) return;
    const seed: Record<string, string> = {};
    for (const [k, v] of Object.entries(configQ.data)) {
      if (!secretKeys.has(k)) seed[k] = v;
    }
    setValues(seed);
  }, [configQ.data, secretKeys]);

  const set = (key: string, value: string) => setValues((p) => ({ ...p, [key]: value }));

  async function save() {
    setSaving(true);
    try {
      // Drop empty secret fields so we never clear a stored secret with a blank.
      const payload: Record<string, string> = {};
      for (const [k, v] of Object.entries(values)) {
        if (secretKeys.has(k) && !v) continue;
        payload[k] = v;
      }
      await api.setConfig(payload);
      configQ.reload();
      toast.success("Settings saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save settings");
    } finally {
      setSaving(false);
    }
  }

  if (agentLoading) return <Panel title="Settings"><Spinner /></Panel>;
  if (agentError || !agent)
    return (
      <Panel title="Settings">
        <ErrorNote message={agentError ?? "No agent info available."} />
      </Panel>
    );

  const sections = agent.schema.sections ?? [];

  return (
    <Panel
      title="Settings"
      description="Sticky per-agent configuration. Applies to every turn until changed."
      actions={
        <>
          <Button size="sm" variant="ghost" onClick={() => configQ.reload()} disabled={saving}>
            <RotateCcw className="size-3.5" />
            Reset
          </Button>
          <Button size="sm" onClick={save} disabled={saving}>
            <Save className="size-3.5" />
            Save
          </Button>
        </>
      }
    >
      {configQ.error && <ErrorNote message={configQ.error} />}
      {sections.length === 0 ? (
        <EmptyState>This agent declares no editable settings.</EmptyState>
      ) : (
        sections.map((section) => (
          <div key={section.title} className="mb-8 last:mb-0">
            <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide">{section.title}</h2>
            <div className="space-y-5">
              {section.fields.map((f) => (
                <FieldControl
                  key={f.key}
                  field={f}
                  value={values[f.key] ?? f.default ?? ""}
                  hasStored={secretKeys.has(f.key) && Boolean(configQ.data?.[f.key])}
                  models={modelsQ.data ?? []}
                  modelsLoading={modelsQ.loading}
                  onChange={(v) => set(f.key, v)}
                />
              ))}
            </div>
          </div>
        ))
      )}
    </Panel>
  );
}

function FieldControl({
  field,
  value,
  hasStored,
  models,
  modelsLoading,
  onChange,
}: {
  field: Field;
  value: string;
  hasStored: boolean;
  models: ModelInfo[];
  modelsLoading: boolean;
  onChange: (value: string) => void;
}) {
  const header = (
    <div className="flex items-center gap-2">
      <Label htmlFor={field.key}>{field.label}</Label>
      {field.scope === "unified" && <Badge variant="secondary">unified</Badge>}
      {field.required && <span className="text-xs text-destructive">required</span>}
    </div>
  );
  const help = field.help && <p className="text-xs text-muted-foreground">{field.help}</p>;

  // The model field is free-text (an alias like "opus" is valid and NOT an enumerated id) but we surface
  // the account's models as pickable suggestions — same list, same config key as the Models page. When
  // the agent declares model as a fixed `select`, the native select below wins instead.
  if ((field.canon === "model" || field.key === "model") && field.type === "text") {
    return (
      <ModelCombobox
        field={field}
        value={value}
        models={models}
        loading={modelsLoading}
        onChange={onChange}
      />
    );
  }

  if (field.type === "toggle") {
    return (
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-1">
          {header}
          {help}
        </div>
        <Switch checked={value === "true"} onCheckedChange={(c) => onChange(c ? "true" : "false")} />
      </div>
    );
  }

  if (field.type === "select") {
    return (
      <div className="space-y-1.5">
        {header}
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger id={field.key}>
            <SelectValue placeholder={field.placeholder ?? "Select…"} />
          </SelectTrigger>
          <SelectContent>
            {field.options?.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {help}
      </div>
    );
  }

  if (field.type === "multiselect") {
    const selected = new Set(value ? value.split(",").map((s) => s.trim()).filter(Boolean) : []);
    const toggle = (v: string) => {
      const next = new Set(selected);
      if (next.has(v)) next.delete(v);
      else next.add(v);
      onChange([...next].join(","));
    };
    return (
      <div className="space-y-1.5">
        {header}
        <div className="flex flex-wrap gap-2">
          {field.options?.map((o) => (
            <button
              key={o.value}
              type="button"
              onClick={() => toggle(o.value)}
              className={cn(
                "border px-3 py-1.5 text-xs transition-colors",
                selected.has(o.value)
                  ? "border-ink/30 bg-accent"
                  : "border-border text-muted-foreground hover:border-ink/25",
              )}
            >
              {o.label}
            </button>
          ))}
        </div>
        {help}
      </div>
    );
  }

  // text | secret
  return (
    <div className="space-y-1.5">
      {header}
      <Input
        id={field.key}
        type={field.type === "secret" ? "password" : "text"}
        value={value}
        placeholder={
          field.type === "secret" && hasStored ? "•••••• (stored — leave blank to keep)" : field.placeholder
        }
        onChange={(e) => onChange(e.target.value)}
        autoComplete="off"
        spellCheck={false}
      />
      {help}
    </div>
  );
}

// A type-or-pick control for the model field: a free-text input (so an alias like "opus" or any id is
// always valid) with a suggestion dropdown drawn from the account's enumerated models. Selecting a row
// writes its id; the current value's friendly label shows as a badge when it matches a known model. With
// no enumerable models the dropdown never opens, so it behaves exactly like the plain text field it
// replaces. It writes the same config key as the Models page, so the two surfaces stay in sync.
function ModelCombobox({
  field,
  value,
  models,
  loading,
  onChange,
}: {
  field: Field;
  value: string;
  models: ModelInfo[];
  loading: boolean;
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);

  const matched = models.find((m) => m.id === value);

  const suggestions = useMemo(() => {
    const q = value.trim().toLowerCase();
    // While the value exactly names a model, show the full list (so you can switch), not a list of one.
    const list = q && !matched
      ? models.filter((m) => m.id.toLowerCase().includes(q) || m.label.toLowerCase().includes(q))
      : models;
    return list.slice(0, 50);
  }, [models, value, matched]);

  // Dismiss the dropdown when focus/click leaves the control.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const hasList = models.length > 0;

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-2">
        <Label htmlFor={field.key}>{field.label}</Label>
        {field.scope === "unified" && <Badge variant="secondary">unified</Badge>}
        {matched && (
          <Badge variant="outline" className="font-normal">
            {matched.label}
          </Badge>
        )}
      </div>

      <div ref={wrapRef} className="relative">
        <Input
          id={field.key}
          value={value}
          placeholder={field.placeholder}
          autoComplete="off"
          spellCheck={false}
          className={cn(hasList && "pr-9")}
          onChange={(e) => {
            onChange(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === "Escape") setOpen(false);
          }}
        />
        {hasList && (
          <button
            type="button"
            tabIndex={-1}
            aria-label="Toggle model list"
            onClick={() => setOpen((o) => !o)}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <ChevronsUpDown className="size-4" />
          </button>
        )}

        {open && suggestions.length > 0 && (
          <div className="absolute z-20 mt-1 max-h-64 w-full overflow-auto border border-border bg-background shadow-md">
            {suggestions.map((m) => {
              const active = m.id === value;
              return (
                <button
                  key={m.id}
                  type="button"
                  // Keep focus on the input so onChange/commit fire before the outside-click closes us.
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => {
                    onChange(m.id);
                    setOpen(false);
                  }}
                  className={cn(
                    "flex w-full items-center justify-between gap-3 px-3 py-2 text-left hover:bg-accent",
                    active && "bg-accent",
                  )}
                >
                  <span className="min-w-0">
                    <span className="block truncate text-sm">{m.label}</span>
                    <span className="block truncate font-mono text-[11px] text-muted-foreground">{m.id}</span>
                  </span>
                  {active && <Check className="size-3.5 shrink-0" />}
                </button>
              );
            })}
          </div>
        )}
      </div>

      {field.help && <p className="text-xs text-muted-foreground">{field.help}</p>}
      {loading && !hasList && <p className="text-xs text-muted-foreground">Loading your account’s models…</p>}
    </div>
  );
}
