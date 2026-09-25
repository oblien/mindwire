import type { Mindwire } from "./client.js";
import { readSSE } from "./sse.js";

/** Native workspace execution; the same contract on macOS, Linux and Windows. */
export interface WorkspaceHost {
  hostname: string; os: "darwin" | "linux" | "windows" | string; arch: string;
  home: string; directory: string; pid: number;
}
export interface HostResources { cpus: number; memoryMb: number; diskMb: number }
export interface WorkspaceFile {
  name: string; path: string; type: "file" | "dir"; size: number; modifiedAt: string; isIgnored: boolean;
}
export interface WorkspaceFileContent { path: string; content: string; size: number }
export interface WorkspaceExecRequest { argv: string[]; directory?: string; input?: string; timeoutMs?: number }
export interface WorkspaceExecResult { stdout: string; stderr: string; exitCode: number; truncated?: boolean }
export type WorkspaceExecEvent =
  | { kind: "output"; channel: "stdout" | "stderr"; data: string }
  | { kind: "exit"; exitCode: number }
  | { kind: "error"; error: string };
export interface TerminalRequest { id: string; directory?: string; columns: number; rows: number }
export interface TerminalState extends TerminalRequest { title: string; running: boolean; exitCode?: number; createdAt: string }
export interface TerminalEvent { sequence: number; kind: "output" | "reset" | "exit"; data?: string; state?: TerminalState }
/** Retry the exact same writer/sequence/data after a lost acknowledgement, never a new sequence. */
export interface TerminalInput { writer: string; sequence: number; data: string }

export class ExecutionApi {
  readonly terminals: TerminalsApi;
  constructor(private readonly client: Mindwire) { this.terminals = new TerminalsApi(client); }
  host(): Promise<WorkspaceHost> { return this.client.http.request("GET", "/workspace/host"); }
  resources(): Promise<HostResources> { return this.client.http.request("GET", "/workspace/resources"); }
  files(path = "", query?: string): Promise<WorkspaceFile[]> {
    return this.client.http.request("GET", "/workspace/files", { query: { path, query } });
  }
  read(path: string): Promise<WorkspaceFileContent> { return this.client.http.request("GET", "/workspace/file", { query: { path } }); }
  write(path: string, content: string): Promise<{ ok: boolean }> {
    return this.client.http.request("PUT", "/workspace/file", { body: { path, content } });
  }
  remove(path: string): Promise<{ ok: boolean }> { return this.client.http.request("DELETE", "/workspace/file", { query: { path } }); }
  exec(command: WorkspaceExecRequest, signal?: AbortSignal): Promise<WorkspaceExecResult> {
    return this.client.http.request("POST", "/workspace/exec", { body: command, signal });
  }
  /** Output data is base64, preserving partial UTF-8 chunks. Cancelling stops this command. */
  async *stream(command: WorkspaceExecRequest, signal?: AbortSignal): AsyncGenerator<WorkspaceExecEvent> {
    const response = await this.client.http.open("POST", "/workspace/exec/stream", { body: command, signal });
    yield* readSSE<WorkspaceExecEvent>(response.body!, signal);
  }
}
export class TerminalsApi {
  constructor(private readonly client: Mindwire) {}
  list(directory?: string): Promise<TerminalState[]> { return this.client.http.request("GET", "/workspace/terminals", { query: { directory } }); }
  open(request: TerminalRequest): Promise<TerminalState> { return this.client.http.request("POST", "/workspace/terminals", { body: request }); }
  get(id: string): Promise<TerminalState> { return this.client.http.request("GET", this.path(id)); }
  close(id: string): Promise<{ ok: boolean }> { return this.client.http.request("DELETE", this.path(id)); }
  input(id: string, input: TerminalInput): Promise<{ ok: boolean; sequence: number }> {
    return this.client.http.request("POST", `${this.path(id)}/input`, { body: input });
  }
  resize(id: string, columns: number, rows: number): Promise<TerminalState> {
    return this.client.http.request("PUT", `${this.path(id)}/size`, { body: { columns, rows } });
  }
  /** Detaching only closes the subscription. Resume from the last sequence; reset replaces the screen. */
  async *events(id: string, after = 0, signal?: AbortSignal): AsyncGenerator<TerminalEvent> {
    const response = await this.client.http.open("GET", `${this.path(id)}/events`, { query: { after }, signal });
    yield* readSSE<TerminalEvent>(response.body!, signal);
  }
  private path(id: string): string { return `/workspace/terminals/${encodeURIComponent(id)}`; }
}
