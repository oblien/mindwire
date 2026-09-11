// The Agent-auth surface — the harness's own credentials (Anthropic key, `claude login`, gateway
// token, …), distinct from the Oblien runtime auth that gates the whole app. Drives the daemon's
// begin→step auth flow: pick a method, fill any fields (or follow a login URL/code), submit until the
// status settles. Secrets are collected here and posted straight to the daemon — never held in the UI.
import { useState } from "react";
import { KeyRound, ExternalLink, Loader2, CheckCircle2, ArrowRight } from "lucide-react";

import { api } from "@/lib/api";
import { useApp } from "@/lib/app-context";
import { useAsync } from "@/lib/useAsync";
import { cn } from "@/lib/utils";
import { resolveAuthForm } from "@/lib/auth-form";
import { Panel, Section, Spinner, ErrorNote } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { toast } from "@/components/ui/sonner";
import type { AuthMethod, AuthState, AuthStatus, Field } from "@shared/api";

export function AgentAuthPanel() {
  const { reloadAgent } = useApp();
  const methodsQ = useAsync<AuthMethod[]>(() => api.authMethods());
  const statusQ = useAsync<AuthStatus>(() => api.authStatus());

  const [flow, setFlow] = useState<{ method: AuthMethod; state: AuthState } | null>(null);
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  const refreshStatus = () => {
    statusQ.reload();
    reloadAgent();
  };

  async function begin(method: AuthMethod) {
    setBusy(true);
    setInputs(Object.fromEntries((method.fields ?? []).filter((f) => f.default !== undefined).map((f) => [f.key, f.default!])));
    try {
      const state = await api.authBegin(method.id);
      setFlow({ method, state });
      if (state.status === "complete") {
        toast.success(`Authenticated via ${method.label}`);
        setFlow(null);
        refreshStatus();
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start auth");
    } finally {
      setBusy(false);
    }
  }

  async function step(values: Record<string, string>) {
    if (!flow) return;
    setBusy(true);
    try {
      const state = await api.authStep(values);
      setFlow({ method: flow.method, state });
      if (state.status === "complete") {
        toast.success(`Authenticated via ${flow.method.label}`);
        setFlow(null);
        setInputs({});
        refreshStatus();
      } else if (state.status === "error") {
        toast.error(state.message ?? "Authentication failed");
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not complete auth");
    } finally {
      setBusy(false);
    }
  }

  const status = statusQ.data;

  return (
    <Panel
      title="Agent auth"
      description="Credentials the harness uses to reach its model provider."
      actions={
        status ? (
          <Badge variant={status.configured ? "default" : "secondary"}>
            {status.configured ? `Configured${status.method ? ` · ${status.method}` : ""}` : "Not configured"}
          </Badge>
        ) : undefined
      }
    >
      {(methodsQ.loading || statusQ.loading) && <Spinner />}
      {methodsQ.error && <ErrorNote message={methodsQ.error} />}
      {status?.detail && (
        <div className="mb-6 flex items-center gap-2 border border-border px-4 py-3 text-sm">
          <CheckCircle2 className="size-4 text-muted-foreground" />
          {status.detail}
        </div>
      )}

      {flow ? (
        <Section title={flow.method.label}>
          <FlowView
            method={flow.method}
            state={flow.state}
            inputs={inputs}
            setInput={(k, v) => setInputs((p) => ({ ...p, [k]: v }))}
            onSubmit={step}
            onCancel={() => {
              setFlow(null);
              setInputs({});
            }}
            busy={busy}
          />
        </Section>
      ) : (
        <Section title="Methods">
          <div className="space-y-2">
            {(methodsQ.data ?? []).map((m) => (
              <div
                key={m.id}
                className="flex items-start gap-3 border border-border px-4 py-3"
              >
                <KeyRound className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-medium">{m.label}</p>
                    {m.scope === "custom" && <Badge variant="outline">custom</Badge>}
                    {m.interactive && <Badge variant="secondary">interactive</Badge>}
                  </div>
                  {m.help && <p className="mt-0.5 text-xs text-muted-foreground">{m.help}</p>}
                </div>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => begin(m)}>
                  Use
                </Button>
              </div>
            ))}
          </div>
        </Section>
      )}
    </Panel>
  );
}

function FlowView({
  method,
  state,
  inputs,
  setInput,
  onSubmit,
  onCancel,
  busy,
}: {
  method: AuthMethod;
  state: AuthState;
  inputs: Record<string, string>;
  setInput: (key: string, value: string) => void;
  onSubmit: (values: Record<string, string>) => void;
  onCancel: () => void;
  busy: boolean;
}) {
  const fields = state.fields ?? method.fields ?? [];
  const form = resolveAuthForm(fields, state.sections ?? method.sections ?? [], inputs);
  const renderField = (f: Field, title: string, helpId?: string) => {
    const value = inputs[f.key] ?? f.default ?? "";
    const id = `auth-${f.key}`;
    if (f.type === "select" && f.presentation === "segmented") {
      return (
        <fieldset key={f.key} aria-label={f.label} aria-describedby={helpId} disabled={busy} className="flex gap-1 rounded-md bg-muted p-1">
          {(f.options ?? []).map((option) => (
            <label key={option.value} className="min-w-0 flex-1 cursor-pointer">
              <input type="radio" name={id} value={option.value} checked={value === option.value}
                onChange={() => setInput(f.key, option.value)} className="peer sr-only" />
              <span className="block rounded-sm px-2 py-2 text-center text-sm text-muted-foreground peer-checked:bg-background peer-checked:text-foreground peer-checked:shadow-sm peer-focus-visible:outline peer-focus-visible:outline-ring">
                {option.label}
              </span>
            </label>
          ))}
        </fieldset>
      );
    }
    return (
      <div key={f.key} className="space-y-1.5">
        <Label htmlFor={id} className={title === f.label ? "sr-only" : undefined}>{f.label}</Label>
        {f.type === "select" ? (
          <select id={id} value={value} onChange={(event) => setInput(f.key, event.target.value)} disabled={busy}
            aria-describedby={helpId} className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm">
            {!value && <option value="">{f.placeholder ?? f.label}</option>}
            {(f.options ?? []).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        ) : f.type === "toggle" ? (
          <Switch id={id} checked={value === "true"} onCheckedChange={(checked) => setInput(f.key, String(checked))}
            disabled={busy} aria-describedby={helpId} />
        ) : (
          <Input id={id} type={f.type === "secret" ? "password" : f.inputMode === "url" ? "url" : "text"}
            value={value} placeholder={f.placeholder} onChange={(event) => setInput(f.key, event.target.value)}
            disabled={busy} autoComplete="off" autoCapitalize="none" spellCheck={false} aria-describedby={helpId} />
        )}
      </div>
    );
  };
  return (
    <form className="space-y-6" onSubmit={(event) => { event.preventDefault(); if (!busy && form.valid) onSubmit(form.values); }}>
      {state.message && <p className="text-sm text-muted-foreground">{state.message}</p>}

      {state.url && (
        <a
          href={state.url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-2 border border-border px-3 py-2 text-sm hover:bg-accent"
        >
          <ExternalLink className="size-4" />
          Open the login page
        </a>
      )}
      {state.code && (
        <div className="border border-border px-4 py-3">
          <p className="text-xs text-muted-foreground">Enter this code</p>
          <p className="font-mono text-lg tracking-widest">{state.code}</p>
        </div>
      )}

      {form.sections.map((section) => (
        <fieldset key={section.id} className="space-y-3">
          <legend className="mb-2 text-sm font-medium">{section.title}</legend>
          {section.fields.map((field) => renderField(field, section.title, section.help ? `auth-section-${section.id}-help` : undefined))}
          {section.help && <p id={`auth-section-${section.id}-help`} className="whitespace-pre-line text-xs text-muted-foreground">{section.help}</p>}
        </fieldset>
      ))}

      <div className="sticky bottom-0 flex items-center gap-2 border-t border-border bg-background py-3">
        <Button type="submit" disabled={busy || !form.valid}>
          {busy ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
          {fields.length ? "Connect" : "Continue"}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <StatusChip status={state.status} />
      </div>
    </form>
  );
}

function StatusChip({ status }: { status: AuthState["status"] }) {
  return (
    <span
      className={cn(
        "text-xs",
        status === "error" ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {status}
    </span>
  );
}
