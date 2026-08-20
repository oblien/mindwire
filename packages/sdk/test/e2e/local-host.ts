// The real base host the container layer composes over locally: a SandboxHost backed by
// node:child_process + node:fs, running commands on THIS machine. It's what the E2E hands to
// `provisionContainer` — the same `SandboxHost` seam the SSH/Oblien backends implement, but with
// no remote hop, so the container path is exercised against the runner's own Docker.
//
// Deliberately shell-free: `exec(argv)` runs argv[0] with argv[1..] via execFile and never wraps
// them in a shell. `ContainerHost` already hands us fully-formed argv like
// `["docker","exec",cid,"bash","-lc",script]` — a second shell here would re-split the script and
// corrupt it (container.ts:123 "never pre-join"). Only `bash -lc` when it's literally in the argv.
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import * as fs from "node:fs/promises";
import type { SandboxHost, ExecResult } from "../../src/index.js";

const pExecFile = promisify(execFile);

export class LocalHost implements SandboxHost {
  async exec(argv: string[], opts?: { timeoutSeconds?: number }): Promise<ExecResult> {
    const [cmd, ...args] = argv;
    if (!cmd) return { exitCode: 1, stdout: "", stderr: "", error: "empty argv" };
    try {
      const { stdout, stderr } = await pExecFile(cmd, args, {
        timeout: opts?.timeoutSeconds ? opts.timeoutSeconds * 1000 : undefined,
        maxBuffer: 32 * 1024 * 1024,
        encoding: "utf8",
      });
      return { exitCode: 0, stdout, stderr };
    } catch (e: any) {
      // execFile rejects with the exit code on `.code` (a number) for a non-zero exit, or a string
      // like "ENOENT" when the binary isn't found. Surface both without pretending success.
      return {
        exitCode: typeof e?.code === "number" ? e.code : 1,
        stdout: typeof e?.stdout === "string" ? e.stdout : "",
        stderr: typeof e?.stderr === "string" ? e.stderr : "",
        error: e?.message ?? String(e),
      };
    }
  }

  async putFile(path: string, data: Uint8Array, opts?: { mode?: string }): Promise<void> {
    await fs.writeFile(path, data);
    if (opts?.mode) await fs.chmod(path, Number.parseInt(opts.mode, 8));
  }
}
