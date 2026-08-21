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
import { Panel, Section, Spinner, ErrorNote } from "@/components/common/Panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
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
    setInputs({});
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

  async function step() {
    if (!flow) return;
    setBusy(true);
    try {
      const state = await api.authStep(inputs);
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
  state,
  inputs,
  setInput,
  onSubmit,
  onCancel,
  busy,
}: {
  state: AuthState;
  inputs: Record<string, string>;
  setInput: (key: string, value: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
  busy: boolean;
}) {
  return (
    <div className="space-y-4">
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

      {state.fields?.map((f: Field) => (
        <div key={f.key} className="space-y-1.5">
          <Label htmlFor={`auth-${f.key}`}>{f.label}</Label>
          <Input
            id={`auth-${f.key}`}
            type={f.type === "secret" ? "password" : "text"}
            value={inputs[f.key] ?? ""}
            placeholder={f.placeholder}
            onChange={(e) => setInput(f.key, e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
          {f.help && <p className="text-xs text-muted-foreground">{f.help}</p>}
        </div>
      ))}

      <div className="flex items-center gap-2">
        <Button onClick={onSubmit} disabled={busy}>
          {busy ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
          {state.fields?.length ? "Submit" : "Continue"}
        </Button>
        <Button variant="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <StatusChip status={state.status} />
      </div>
    </div>
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
