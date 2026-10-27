import type { Mindwire } from "./client.js";
import type { Condition } from "./types.js";

export type IntegrationProvider = "slack" | "teams" | "discord" | "telegram";
export type IntegrationMode = "notifications" | "commands" | "chat" | "trigger";

/** Write-only credentials, stored on the daemon. Omitted fields preserve saved values. */
export interface IntegrationCredentials {
  botToken?: string;
  signingSecret?: string;
  webhookSecret?: string;
  publicKey?: string;
  clientSecret?: string;
}

export interface IntegrationSpec {
  name: string;
  provider: IntegrationProvider;
  botId: string;
  /** Host dispatch delivers verified events to claim()/complete(), without starting an agent. */
  dispatch?: "bot" | "host";
  mode: IntegrationMode;
  enabled: boolean;
  /** Explicit allowlist. Empty lists are rejected. */
  channelIds: string[];
  userIds?: string[];
  /** Slack team, Discord guild, or Microsoft Entra tenant ID. */
  workspaceId?: string;
  applicationId?: string;
  command?: string;
  /** Forward this bot's lifecycle notifications to destination. */
  notify: boolean;
  conditions?: Condition[];
  destination?: string;
  /** Owner-configured Microsoft Teams connector URL. */
  serviceUrl?: string;
  expectedRevision?: number;
  credentials?: IntegrationCredentials;
}

export interface Integration extends Omit<IntegrationSpec, "credentials" | "expectedRevision"> {
  id: string;
  revision: number;
  hasCredentials: boolean;
  webhookPath: string;
}

export interface IntegrationReceipt {
  id: string;
  integrationId: string;
  channelId: string;
  userId: string;
  state: "queued" | "running" | "delivered" | "failed";
  chatId?: string;
  runId?: string;
  error?: string;
  createdAt: string;
}

export interface IntegrationHostEvent {
  id: string;
  integrationId: string;
  workspaceId: string;
  channelId: string;
  userId: string;
  threadId?: string;
  text: string;
  command: boolean;
  lease: string;
  revision: number;
}

/** Persistent messaging connections shared with the app and daemon. */
export class IntegrationsApi {
  constructor(private readonly mw: Mindwire) {}
  list(): Promise<Integration[]> { return this.mw.http.request("GET", "/integrations"); }
  get(id: string): Promise<Integration> { return this.mw.http.request("GET", this.path(id)); }
  create(spec: IntegrationSpec): Promise<Integration> {
    return this.mw.http.request("POST", "/integrations", { body: spec });
  }
  /** Complete replacement with optimistic concurrency; omit credentials to preserve them. */
  update(id: string, spec: IntegrationSpec & { expectedRevision: number }): Promise<Integration> {
    return this.mw.http.request("PUT", this.path(id), { body: spec });
  }
  async delete(id: string): Promise<void> { await this.mw.http.request("DELETE", this.path(id)); }
  /** Sends to the configured destination, also useful as a connection test. */
  send(id: string, text: string): Promise<{ sent: boolean }> {
    return this.mw.http.request("POST", `${this.path(id)}/send`, { body: { text } });
  }
  /** Latest 100 redacted delivery receipts. */
  events(id: string): Promise<IntegrationReceipt[]> {
    return this.mw.http.request("GET", `${this.path(id)}/events`);
  }
  retry(id: string, eventId: string): Promise<IntegrationReceipt> {
    return this.mw.http.request("POST", `${this.path(id)}/events/${encodeURIComponent(eventId)}/retry`);
  }
  /** Requires integrationsVersion >= 2. Events have a 90-second lease; use id for host idempotency. */
  claim(id: string): Promise<IntegrationHostEvent | null> {
    return this.mw.http.request("POST", `${this.path(id)}/claim`, { body: {} });
  }
  complete(id: string, eventId: string, lease: string, text: string): Promise<IntegrationReceipt> {
    return this.mw.http.request("POST", `${this.path(id)}/events/${encodeURIComponent(eventId)}/complete`, { body: { lease, text } });
  }
  /** Forward raw provider bytes through an authenticated private transport. Signatures are still verified. */
  forwardWebhook(id: string, body: string, headers: Record<string, string>): Promise<{status: number;body: string;contentType: string}> {
    return this.mw.http.request("POST", `${this.path(id)}/forward`, { body: {body,headers} });
  }
  private path(id: string): string { return `/integrations/${encodeURIComponent(id)}`; }
}
