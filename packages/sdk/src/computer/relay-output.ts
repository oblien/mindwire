import { randomUUID } from "node:crypto";
import { open, rm, type FileHandle } from "node:fs/promises";
import { tmpdir } from "node:os";
import * as path from "node:path";

/** A provider must not inherit pipes whose reader dies during a controller
 * upgrade. Use a private append-only file descriptor, consume startup output,
 * then discard it. A successor can adopt the same file without touching the
 * provider or its address. Raw provider output is never copied to our logs. */
export class RelayOutput {
  private position = 0;
  private timer?: ReturnType<typeof setInterval>;
  private pending: Promise<void> = Promise.resolve();
  private closing?: Promise<void>;

  private constructor(readonly file: string, private readonly handle: FileHandle) {}

  static async create(directory = tmpdir()): Promise<RelayOutput> {
    const file = path.join(directory, `.relay-output-${randomUUID()}.log`);
    return new RelayOutput(file, await open(file, "ax+", 0o600));
  }

  static async adopt(directory: string, file: string): Promise<RelayOutput | undefined> {
    // Only our private spool files can be truncated or removed during adoption.
    if (path.dirname(file) !== path.resolve(directory)
        || !/^\.relay-output-[a-f0-9-]{36}\.log$/.test(path.basename(file))) return;
    try {
      const output = new RelayOutput(file, await open(file, "r+"));
      output.discard();
      return output;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
  }

  get fd(): number { return this.handle.fd; }

  async read(): Promise<Buffer> {
    const { size } = await this.handle.stat();
    if (size > 2 * 1024 * 1024) throw new Error("The tunnel helper produced too much startup output. Check its configuration.");
    const data = Buffer.alloc(Math.min(65_536, Math.max(0, size - this.position)));
    const { bytesRead } = await this.handle.read(data, 0, data.length, this.position);
    this.position += bytesRead;
    return data.subarray(0, bytesRead);
  }

  discard(): void {
    if (this.timer || this.closing) return;
    const drain = () => {
      // O_APPEND on the provider's descriptor ensures truncation never leaves
      // sparse holes. After readiness no log record is needed for connectivity.
      this.pending = this.pending.then(() => this.handle.truncate(0)).catch(() => {});
    };
    drain();
    this.timer = setInterval(drain, 2000);
    this.timer.unref();
  }

  close(): Promise<void> {
    return this.closing ??= (async () => {
      clearInterval(this.timer);
      await this.pending;
      await this.handle.close();
      await rm(this.file, { force: true });
    })();
  }
}
