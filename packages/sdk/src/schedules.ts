import type { Mindwire } from "./client.js";
import type { AppSchedule, AppScheduleSpec } from "./apps.js";

/** Work to repeat in an existing conversation, using its native context and permissions. */
export type ChatScheduleSpec = Omit<AppScheduleSpec, "command" | "title" | "body"> & {
  prompt: string;
};

export type ChatSchedule = Omit<AppSchedule, "appId" | "command" | "title" | "body"> & {
  chatId: string;
  prompt: string;
  /** Present while the scheduled turn is attached. */
  runId?: string;
  lastRunId?: string;
};

/** Requires chatSchedulesVersion >= 1. Uses the same job engine as Apps. */
export class ChatSchedulesApi {
  constructor(private readonly mw: Mindwire) {}

  list(chatId: string): Promise<ChatSchedule[]> {
    return this.mw.http.request("GET", this.path(chatId));
  }

  save(chatId: string, schedule: ChatScheduleSpec): Promise<ChatSchedule> {
    return this.mw.http.request("POST", this.path(chatId), { body: schedule });
  }

  /** Stops future occurrences; an accepted turn continues through the ordinary run API. */
  delete(chatId: string, scheduleId: string): Promise<{ removed: boolean }> {
    return this.mw.http.request("DELETE", `${this.path(chatId)}/${encodeURIComponent(scheduleId)}`);
  }

  private path(chatId: string): string {
    return `/chats/${encodeURIComponent(chatId)}/schedules`;
  }
}
