import { clearLine, cursorTo } from "node:readline";

/** One terminal line for one connection attempt. JSON and redirected output
 * keep their normal events; interactive retries don't accumulate a wall of logs. */
export class ConnectionProgress {
  private readonly startedAt = Date.now();
  private readonly timer: ReturnType<typeof setInterval>;
  private message = "Starting Mindwire…";
  private closed = false;
  private readonly exiting = () => this.close();

  constructor(private readonly stream: NodeJS.WriteStream) {
    this.render();
    this.timer = setInterval(() => this.render(), 1000);
    this.timer.unref();
    process.once("exit", this.exiting);
  }

  update(message: string): void { this.message = message; this.render(); }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    clearInterval(this.timer);
    process.off("exit", this.exiting);
    cursorTo(this.stream, 0);
    clearLine(this.stream, 0);
  }

  private render(): void {
    const seconds = Math.floor((Date.now() - this.startedAt) / 1000);
    const elapsed = seconds > 0 ? ` · ${seconds}s` : "";
    const width = Math.max(1, (this.stream.columns || 80) - elapsed.length - 1);
    const message = this.message.replace(/[\x00-\x1f\x7f-\x9f]/g, "");
    const text = message.length > width ? message.slice(0, width - 1) + "…" : message;
    cursorTo(this.stream, 0);
    clearLine(this.stream, 0);
    this.stream.write(text + elapsed);
  }
}
