import type { Mindwire } from "./client.js";
import type { WorkspaceSnapshot } from "./workspace.js";

/** Metadata from the harness's native resume list, across original folders. */
export interface Conversation {
  id: string;
  /** Present after association with the ordinary Mindwire chat API. */
  chatId?: string;
  agentId?: string;
  agentType: string;
  agentName: string;
  projectId?: string;
  projectName?: string;
  cwd: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  lastStatus?: string;
  lastRunId?: string;
}

export interface ConversationQuery {
  /** A workspace agent profile, not the harness type. Omit for all agents. */
  agentId?: string;
  search?: string;
  cursor?: string;
  /** 1–100; defaults to 50. */
  limit?: number;
  refresh?: boolean;
}

export interface ConversationPage {
  workspaceId: string;
  items: Conversation[];
  total: number;
  nextCursor?: string;
  issues?: { projectId: string; agent: string; message: string }[];
}

export interface ConversationOpenRequest { id: string; agentId?: string }
export interface ConversationOpenResult { chatId: string; snapshot: WorkspaceSnapshot }

/** Requires conversationBrowserVersion >= 1. No transcript copies or background polling. */
export class ConversationsApi {
  constructor(private readonly mw: Mindwire) {}

  /** Read-only; includes folders not registered as Mindwire projects. */
  list(query: ConversationQuery = {}): Promise<ConversationPage> {
    return this.mw.http.request("GET", "/workspace/conversations", { query: { ...query } });
  }

  /** Idempotently associates the selected native session with its original folder.
   * Use chatId with messages()/turn() and the ordinary chat APIs afterward. */
  open(request: ConversationOpenRequest): Promise<ConversationOpenResult> {
    return this.mw.http.request("POST", "/workspace/conversations/open", { body: request });
  }
}
