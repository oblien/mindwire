import type { AgentScoped, Mindwire } from "./client.js";
import type { Http } from "./http.js";
import { Run } from "./run.js";
import { readSSE } from "./sse.js";
import type { ProjectAuth } from "./workspace.js";
import type { Run as RunData, VoiceAudio, VoiceEvent, VoiceStatus } from "./types.js";

/** Harness-native voice; independent from a coding model's audio-file modalities. */
export class VoiceApi {
  constructor(private readonly client: Mindwire) {}

  status(scoped?: AgentScoped): Promise<VoiceStatus> {
    return this.client.http.request("GET", "/voice", {
      query: { agent: scoped?.agent ?? this.client.defaultAgent },
    });
  }

  /** Start in an idle chat. Keep requestId/clientId when retrying a lost receipt. */
  async start(input: AgentScoped & {
    chatId: string; cwd?: string; voice?: string; requestId?: string; clientId?: string; gitAuth?: ProjectAuth;
  }): Promise<VoiceSession> {
    const clientId = input.clientId ?? crypto.randomUUID();
    const data = await this.client.http.request<RunData>("POST", "/turns", {
      query: { agent: input.agent ?? this.client.defaultAgent },
      body: { chatId: input.chatId, message: "", cwd: input.cwd, gitAuth: input.gitAuth,
        requestId: input.requestId ?? crypto.randomUUID(), options: { voice: { clientId, voice: input.voice } } },
    });
    return new VoiceSession(this.client.http, new Run(this.client.http, data), clientId);
  }
}

/** Ephemeral PCM stream. Ending voice leaves any started coding task running. */
export class VoiceSession {
  private sequence = 0;
  private sending = false;
  private ended = false;
  private streaming = false;
  constructor(private readonly http: Http, readonly run: Run, readonly clientId: string) {}

  private get path(): string { return `/runs/${encodeURIComponent(this.run.id)}/voice`; }

  /** Await each ordered batch. No automatic audio replay after a network failure. */
  async appendAudio(audio: VoiceAudio, signal?: AbortSignal): Promise<void> {
    if (this.ended || this.sending) throw new Error("Voice ended or the previous audio batch is still being delivered.");
    this.sending = true;
    try {
      await this.http.request("POST", this.path, { signal, body: {
        clientId: this.clientId, action: "audio", sequence: ++this.sequence, audio,
      } });
    } catch (error) {
      await this.stop().catch(() => {});
      throw error;
    } finally { this.sending = false; }
  }

  /** Subscribe before sending audio; wait for ready. Breaking the loop ends voice. */
  async *stream(signal?: AbortSignal): AsyncGenerator<VoiceEvent> {
    if (this.streaming || this.ended) throw new Error("This voice stream is already open or ended.");
    this.streaming = true;
    const controller = new AbortController();
    const abort = () => controller.abort(signal?.reason);
    signal?.addEventListener("abort", abort, { once: true });
    if (signal?.aborted) abort();
    let heartbeatPending = false;
    const heartbeat = setInterval(() => {
      if (heartbeatPending || controller.signal.aborted) return;
      heartbeatPending = true;
      void this.http.request("POST", this.path, { signal: controller.signal,
        body: { clientId: this.clientId, action: "heartbeat" } })
        .catch((error: unknown) => controller.abort(error)).finally(() => { heartbeatPending = false; });
    }, 5_000);
    try {
      const response = await this.http.open("GET", `${this.path}/stream`, {
        query: { clientId: this.clientId }, signal: controller.signal,
      });
      for await (const event of readSSE<VoiceEvent>(response.body!, controller.signal)) {
        if (event.type !== "heartbeat") yield event;
        if (event.type === "closed" || event.type === "error") break;
      }
    } finally {
      clearInterval(heartbeat);
      signal?.removeEventListener("abort", abort);
      controller.abort();
      this.streaming = false;
      await this.stop().catch(() => {});
    }
  }

  async stop(): Promise<void> {
    if (this.ended) return;
    this.ended = true;
    await this.http.request("POST", this.path, { body: { clientId: this.clientId, action: "stop" } });
  }
}
