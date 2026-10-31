import type { Mindwire } from "./client.js";
import type { WorkspaceRecord, WorkspaceSnapshot, WorkspaceChat, WorkspaceAgent, WorkspaceProject } from "./workspace.js";
import type { Run } from "./types.js";

export interface BotKnowledge { id: string; title: string; content: string }
export interface Bot extends WorkspaceRecord {
  agentId: string;
  /** Default folder for new chats. The bot belongs to workspaceId and can work in its other projects. */
  projectId: string;
  name: string;
  /** Saved palette-character ID; omitted/empty keeps the automatic avatar. */
  icon?: string;
  purpose: string;
  instructions: string;
  memory: string;
  knowledge: BotKnowledge[];
  /** Portable SKILL.md documents, loaded on demand. Requires botsVersion >= 6. */
  skills?: string[] | null;
  /** Creation-only plain harness: no Mindwire bot context, tools or skills. Requires botsVersion >= 7. */
  raw?: boolean;
  /** Application-managed tools; profile context only. Immutable. Requires botsVersion >= 9. */
  externalTools?: boolean;
  isMaster: boolean;
  archived: boolean;
  /** Applies to this bot's current and future chats, independent of its base engine. */
  notificationsMuted?: boolean;
}
export interface BotInput {
  icon?: string;
  id?: string;
  agentId: string;
  projectId?: string;
  name: string;
  purpose?: string;
  instructions?: string;
  memory?: string;
  knowledge?: BotKnowledge[];
  skills?: string[];
  /** Keep native harness behavior. Cannot be changed after creation or combined with bot context. */
  raw?: boolean;
  /** Application-managed tools; profile context only. Immutable. Requires botsVersion >= 9. */
  externalTools?: boolean;
}
export interface BotEdit {
  /** Base agent for future chats; existing chats retain their original engine. Requires botsVersion >= 5. */
  agentId?: string;
  icon?: string;
  expectedRevision: number;
  name?: string;
  purpose?: string;
  instructions?: string;
  memory?: string;
  knowledge?: BotKnowledge[];
  archived?: boolean;
  /** Replaces assigned SKILL.md documents; omit to preserve existing skills. */
  skills?: string[];
  notificationsMuted?: boolean;
}
export interface BotMutation { bot: Bot; snapshot: WorkspaceSnapshot }

export interface BotTaskInput { requestId: string; message: string }
export interface BotTaskRequest extends BotTaskInput { chatId?: string; title?: string; projectId?: string }
export interface BotTask {
  botId: string;
  chatId: string;
  run: Run;
  messages?: { role: string; text: string; createdAt: string }[];
  output?: string;
  historyError?: string;
  interactions?: { id: string; kind: string; title?: string; detail?: string; needsResponse: boolean }[];
  tools?: { name: string; status?: string }[];
  cancelRequested?: boolean;
}
export type BotPeerRoute = { kind: "ssh"; host: string; port: number } | { kind: "websocket"; url: string };
export interface BotPeerIdentity {
  workspaceId: string;
  publicKey: string;
  hostKeyFingerprint: string;
  port?: number;
  sshPort?: number;
  routes?: BotPeerRoute[];
}
export interface BotPeerInput {
  name: string;
  publicKey: string;
  hostKeyFingerprint: string;
  routes: BotPeerRoute[];
  enabled?: boolean;
}
export interface BotPeer extends BotPeerInput { workspaceId: string }
export interface BotPeerCheck { workspaceId: string; reachable: boolean; error?: string; addressRecovery?: boolean }

/** Saved bot identities, native tasks, and explicitly paired bot-control peers. */
export class BotsApi {
  constructor(private readonly mw: Mindwire) {}
  list(options: { projectId?: string; archived?: boolean } = {}): Promise<Bot[]> {
    return this.mw.http.request("GET", "/bots", { query: { projectId: options.projectId, archived: options.archived ? "true" : undefined } });
  }
  get(id: string): Promise<Bot> { return this.mw.http.request("GET", `/bots/${encodeURIComponent(id)}`); }
  ensure(agentId: string, projectId: string): Promise<BotMutation> {
    return this.mw.http.request("POST", "/bots/ensure", { body: { agentId, projectId } });
  }
  create(input: BotInput): Promise<BotMutation> { return this.mw.http.request("POST", "/bots", { body: input }); }
  update(id: string, edit: BotEdit): Promise<BotMutation> {
    return this.mw.http.request("PATCH", `/bots/${encodeURIComponent(id)}`, { body: edit });
  }
  chats(id: string): Promise<WorkspaceChat[]> { return this.mw.http.request("GET", `/bots/${encodeURIComponent(id)}/chats`); }
  createChat(id: string, input: { chatId?: string; title?: string; projectId?: string } = {}): Promise<{ chatId: string; snapshot: WorkspaceSnapshot }> {
    return this.mw.http.request("POST", `/bots/${encodeURIComponent(id)}/chats`, { body: input });
  }
  agents(): Promise<{ agents: WorkspaceAgent[]; projects: WorkspaceProject[] }> {
    return this.mw.http.request("GET", "/bots/agents");
  }
  tasks(id: string): Promise<BotTask[]> {
    return this.mw.http.request("GET", `/bots/${encodeURIComponent(id)}/tasks`);
  }
  /** Keep requestId stable when retrying a task after a lost acknowledgement. */
  delegate(id: string, input: BotTaskRequest): Promise<BotTask> {
    return this.mw.http.request("POST", `/bots/${encodeURIComponent(id)}/tasks`, { body: input });
  }
  task(id: string, runId: string): Promise<BotTask> {
    return this.mw.http.request("GET", this.taskPath(id, runId));
  }
  input(id: string, runId: string, input: BotTaskInput): Promise<BotTask> {
    return this.mw.http.request("POST", `${this.taskPath(id, runId)}/input`, { body: input });
  }
  cancel(id: string, runId: string): Promise<BotTask> {
    return this.mw.http.request("POST", `${this.taskPath(id, runId)}/cancel`);
  }
  peerIdentity(): Promise<BotPeerIdentity> {
    return this.mw.http.request("GET", "/bots/peers/identity");
  }
  startPeerListener(port?: number): Promise<BotPeerIdentity> {
    return this.mw.http.request("POST", "/bots/peers/listener", { body: { port } });
  }
  peers(): Promise<BotPeer[]> { return this.mw.http.request("GET", "/bots/peers"); }
  pair(workspaceId: string, input: BotPeerInput): Promise<BotPeer> {
    return this.mw.http.request("PUT", `/bots/peers/${encodeURIComponent(workspaceId)}`, { body: input });
  }
  revokePeer(workspaceId: string): Promise<{ ok: boolean }> {
    return this.mw.http.request("DELETE", `/bots/peers/${encodeURIComponent(workspaceId)}`);
  }
  /** Bounded peer handshake/discovery; does not start a bot task. */
  checkPeer(workspaceId: string): Promise<BotPeerCheck> {
    return this.mw.http.request("POST", `/bots/peers/${encodeURIComponent(workspaceId)}/check`, { body: {} });
  }
  private taskPath(id: string, runId: string): string {
    return `/bots/${encodeURIComponent(id)}/tasks/${encodeURIComponent(runId)}`;
  }
}
