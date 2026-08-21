// The MCP surface — the persistent MCP-server config the agent loads on every run. Two transports: a
// stdio server (command + args + env + cwd) or an HTTP server (url + headers). List → edit → save/delete
// against `/api/mcp`.
//
// SECURITY: HTTP bearer auth is expressed only as the NAME of an env var (`bearerTokenEnvVar`) the
// agent resolves at run time — this surface never carries a token value. The UI enforces that framing.
import { useEffect, useState } from "react";
import { Server } from "lucide-react";

import { api } from "@/lib/api";
import { useAsync } from "@/lib/useAsync";
import { Panel } from "@/components/common/Panel";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { ScopeToggle } from "@/components/common/ScopeToggle";
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
  SegmentedControl,
  FormActions,
  EmptyPane,
} from "@/components/common/config-ui";
import { toast } from "@/components/ui/sonner";
import type { MCPServer, MemoryScope } from "@shared/api";

type Transport = "stdio" | "http";

interface Draft {
  name: string;
  transport: Transport;
  command: string;
  args: string;
  env: string;
  cwd: string;
  url: string;
  bearerTokenEnvVar: string;
  httpHeaders: string;
}

const EMPTY: Draft = {
  name: "",
  transport: "stdio",
  command: "",
  args: "",
  env: "",
  cwd: "",
  url: "",
  bearerTokenEnvVar: "",
  httpHeaders: "",
};

const linesToRecord = (text: string): Record<string, string> => {
  const rec: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const eq = t.indexOf("=");
    if (eq === -1) continue;
    rec[t.slice(0, eq).trim()] = t.slice(eq + 1).trim();
  }
  return rec;
};
const recordToLines = (rec?: Record<string, string>): string =>
  rec ? Object.entries(rec).map(([k, v]) => `${k}=${v}`).join("\n") : "";

function toDraft(name: string, s: MCPServer): Draft {
  const transport: Transport = s.url ? "http" : "stdio";
  return {
    name,
    transport,
    command: s.command ?? "",
    args: (s.args ?? []).join("\n"),
    env: recordToLines(s.env),
    cwd: s.cwd ?? "",
    url: s.url ?? "",
    bearerTokenEnvVar: s.bearerTokenEnvVar ?? "",
    httpHeaders: recordToLines(s.httpHeaders),
  };
}

function toServer(d: Draft): MCPServer {
  if (d.transport === "http") {
    return {
      url: d.url.trim(),
      ...(d.bearerTokenEnvVar.trim() ? { bearerTokenEnvVar: d.bearerTokenEnvVar.trim() } : {}),
      ...(d.httpHeaders.trim() ? { httpHeaders: linesToRecord(d.httpHeaders) } : {}),
    };
  }
  const args = d.args.split("\n").map((s) => s.trim()).filter(Boolean);
  return {
    command: d.command.trim(),
    ...(args.length ? { args } : {}),
    ...(d.env.trim() ? { env: linesToRecord(d.env) } : {}),
    ...(d.cwd.trim() ? { cwd: d.cwd.trim() } : {}),
  };
}

export function McpPanel() {
  const [scope, setScope] = useState<MemoryScope>("project");
  const listQ = useAsync<Record<string, MCPServer>>(() => api.mcp({ scope }), [scope]);

  const [draft, setDraft] = useState<Draft | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setDraft(null);
    setIsNew(false);
  }, [scope]);

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) =>
    setDraft((d) => (d ? { ...d, [k]: v } : d));

  function openServer(name: string, s: MCPServer) {
    setIsNew(false);
    setDraft(toDraft(name, s));
  }
  function startNew() {
    setIsNew(true);
    setDraft({ ...EMPTY });
  }

  async function save() {
    if (!draft) return;
    const name = draft.name.trim();
    if (!name) return toast.error("A server name is required.");
    if (draft.transport === "stdio" && !draft.command.trim())
      return toast.error("A command is required for a stdio server.");
    if (draft.transport === "http" && !draft.url.trim())
      return toast.error("A URL is required for an HTTP server.");
    setBusy(true);
    try {
      await api.setMcp(name, toServer(draft), { scope });
      listQ.reload();
      setIsNew(false);
      toast.success("MCP server saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!draft || isNew) return;
    setBusy(true);
    try {
      await api.deleteMcp(draft.name, { scope });
      listQ.reload();
      setDraft(null);
      toast.success("MCP server deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete");
    } finally {
      setBusy(false);
    }
  }

  const entries = Object.entries(listQ.data ?? {});

  return (
    <Panel
      title="MCP"
      description="MCP servers the agent loads on every run."
      actions={<ScopeToggle scope={scope} onChange={setScope} />}
    >
      <TwoPane
        rail={
          <>
            <RailNew label="New server" onClick={startNew} />
            <RailStatus
              loading={listQ.loading}
              error={listQ.error}
              empty={!!listQ.data && entries.length === 0}
              emptyText="No servers configured."
            />
            {entries.length > 0 && (
              <RailList>
                {entries.map(([name, s]) => (
                  <RailItem
                    key={name}
                    active={!isNew && draft?.name === name}
                    onClick={() => openServer(name, s)}
                    media={<Server className="size-4 text-ink/70" />}
                    title={name}
                    subtitle={s.url ?? s.command}
                    trailing={<Badge variant="outline">{s.url ? "http" : "stdio"}</Badge>}
                  />
                ))}
              </RailList>
            )}
          </>
        }
      >
        {!draft ? (
          <EmptyPane
            icon={<Server className="size-5" />}
            title="No server selected"
            hint="Pick a server from the list, or add a new stdio or HTTP MCP server."
          />
        ) : (
          <FormCard
            media={<Server className="size-5 text-ink/70" />}
            title={draft.name || "New server"}
            subtitle={`${draft.transport.toUpperCase()} transport`}
            footer={
              <FormActions saving={busy} onSave={save} onDelete={remove} deletable={!isNew} />
            }
          >
            <FieldGroup title="Identity">
              <TwoCol>
                <Field label="Name" htmlFor="mcp-name">
                  <Input
                    id="mcp-name"
                    value={draft.name}
                    disabled={!isNew}
                    onChange={(e) => set("name", e.target.value)}
                    placeholder="my-server"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
                <Field label="Transport">
                  <SegmentedControl
                    value={draft.transport}
                    onChange={(t) => set("transport", t)}
                    options={[
                      { value: "stdio", label: "stdio" },
                      { value: "http", label: "http" },
                    ]}
                  />
                </Field>
              </TwoCol>
            </FieldGroup>

            {draft.transport === "stdio" ? (
              <FieldGroup title="Process">
                <Field label="Command" htmlFor="mcp-cmd">
                  <Input
                    id="mcp-cmd"
                    value={draft.command}
                    onChange={(e) => set("command", e.target.value)}
                    placeholder="npx"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
                <Field label="Arguments (one per line)" htmlFor="mcp-args">
                  <Textarea
                    id="mcp-args"
                    value={draft.args}
                    onChange={(e) => set("args", e.target.value)}
                    rows={3}
                    className="font-mono text-xs"
                    placeholder={"-y\n@modelcontextprotocol/server-filesystem"}
                  />
                </Field>
                <Field label="Environment (KEY=value per line)" htmlFor="mcp-env">
                  <Textarea
                    id="mcp-env"
                    value={draft.env}
                    onChange={(e) => set("env", e.target.value)}
                    rows={3}
                    className="font-mono text-xs"
                    placeholder="LOG_LEVEL=info"
                  />
                </Field>
                <Field label="Working directory (optional)" htmlFor="mcp-cwd">
                  <Input
                    id="mcp-cwd"
                    value={draft.cwd}
                    onChange={(e) => set("cwd", e.target.value)}
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
              </FieldGroup>
            ) : (
              <FieldGroup title="Endpoint">
                <Field label="URL" htmlFor="mcp-url">
                  <Input
                    id="mcp-url"
                    value={draft.url}
                    onChange={(e) => set("url", e.target.value)}
                    placeholder="https://mcp.example.com"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
                <Field
                  label="Bearer token env var (name only)"
                  htmlFor="mcp-bearer"
                  hint="The NAME of an environment variable — never the token value. The agent resolves it at run time."
                >
                  <Input
                    id="mcp-bearer"
                    value={draft.bearerTokenEnvVar}
                    onChange={(e) => set("bearerTokenEnvVar", e.target.value)}
                    placeholder="MY_SERVER_TOKEN"
                    spellCheck={false}
                    autoComplete="off"
                  />
                </Field>
                <Field label="Headers (Header=value per line)" htmlFor="mcp-headers">
                  <Textarea
                    id="mcp-headers"
                    value={draft.httpHeaders}
                    onChange={(e) => set("httpHeaders", e.target.value)}
                    rows={3}
                    className="font-mono text-xs"
                    placeholder="X-Api-Version=2024-01"
                  />
                </Field>
              </FieldGroup>
            )}
          </FormCard>
        )}
      </TwoPane>
    </Panel>
  );
}
