import { expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtemp, rm, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import * as path from "node:path";
import { computerClient } from "../src/computer/lifecycle.js";

const binary = process.env.MINDWIRE_COMPUTER_TEST_BINARY;
const cli = path.resolve(import.meta.dir, "../dist/cli.js");
function run(args: string[], env: NodeJS.ProcessEnv = process.env): Promise<{ code: number | null; output: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn("node", [cli, ...args], { env, stdio: ["ignore", "pipe", "pipe"] });
    let output = "";
    child.stdout.on("data", data => { output += data; }); child.stderr.on("data", data => { output += data; });
    child.on("error", reject); child.on("exit", code => resolve({ code, output }));
  });
}

(binary ? test : test.skip)("CLI concurrent start, native files, detached PTY and idle-safe shutdown", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "mindwire-computer-cli-"));
  const common = ["--state-dir", directory, "--json"];
  try {
    const start = ["start", ...common, "--relay", "none", "--daemon-bin", binary!, "--directory", directory, "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"];
    const results = await Promise.all([run(start), run(start)]);
    for (const result of results) expect(result.code, result.output).toBe(0);
    const client = await computerClient(directory);
    const initial = await client.computer.info();
    const original = await readFile(path.join(directory, "computer.json"), "utf8");
    const filename = path.join(directory, "file '$.txt");
    await client.execution.write(filename, "Native content ✓\nمرحبا");
    expect((await client.execution.read(filename)).content).toBe("Native content ✓\nمرحبا");
    expect((await client.execution.files(directory)).some(file => file.path === filename)).toBe(true);
    const executed = await client.execution.exec({ argv: ["/usr/bin/printf", "%s", "unexpanded $HOME"], directory });
    expect(executed.stdout).toBe("unexpanded $HOME");
    const terminal = await client.execution.terminals.open({ id: "cli-live-terminal", columns: 80, rows: 24, directory });
    const retried = await client.execution.terminals.open({ id: terminal.id, columns: 80, rows: 24, directory });
    expect(retried.createdAt).toBe(terminal.createdAt);
    expect((await run(["stop", ...common])).code).toBe(1);
    expect((await client.computer.info()).pid).toBe(initial.pid);
    const data = Buffer.from("printf 'CLI_%s\\n' 'READY'\n").toString("base64");
    for (let i = 0; i < 2; i++) await client.execution.terminals.input(terminal.id, { writer: "test-writer", sequence: 1, data });
    let output = "", cursor = 0;
    for await (const event of client.execution.terminals.events(terminal.id, 0, AbortSignal.timeout(8000))) {
      output += Buffer.from(event.data ?? "", "base64").toString(); cursor = event.sequence;
      if (output.includes("CLI_READY")) break;
    }
    expect(output).toContain("CLI_READY");
    const same = await client.execution.terminals.list(directory);
    expect(same.map(value => value.id)).toEqual([terminal.id]);
    expect((await client.computer.info()).pid).toBe(initial.pid);
    await client.execution.terminals.input(terminal.id, { writer: "test-writer", sequence: 2, data: Buffer.from("printf 'CLI_%s\\n' 'RECONNECTED'\n").toString("base64") });
    output = "";
    for await (const event of client.execution.terminals.events(terminal.id, cursor, AbortSignal.timeout(8000))) {
      output += Buffer.from(event.data ?? "", "base64").toString();
      if (output.includes("CLI_RECONNECTED")) break;
    }
    expect(output).toContain("CLI_RECONNECTED");
    expect(output).not.toContain("CLI_READY");
    await client.execution.terminals.close(terminal.id);
    const stopped = await run(["stop", ...common]); expect(stopped.code, stopped.output).toBe(0);
    const restarted = await run(["start", ...common]); expect(restarted.code, restarted.output).toBe(0);
    const recovered = await computerClient(directory);
    const after = await recovered.computer.info();
    expect(after.computerId).toBe(initial.computerId);
    expect(after.fingerprint).toBe(initial.fingerprint);
    expect(after.pid).not.toBe(initial.pid);
    expect(JSON.parse(await readFile(path.join(directory, "computer.json"), "utf8")).privateKey).toBe(JSON.parse(original).privateKey);
  } finally {
    await run(["stop", ...common, "--force"]).catch(() => {});
    await rm(directory, { recursive: true, force: true });
  }
}, 90_000);

(binary ? test : test.skip)("managed update waits for terminals and rolls back an invalid release", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "mindwire-computer-update-"));
  const bytes = await readFile(binary!);
  const sum = createHash("sha256").update(bytes).digest("hex");
  const platform = process.platform === "win32" ? "windows" : process.platform;
  const arch = process.arch === "arm64" ? "arm64" : "amd64";
  const version = "0.0.999-fixture";
  const asset = `mindwired-v${version}-${platform}-${arch}${process.platform === "win32" ? ".exe" : ""}`;
  const mirror = Bun.serve({ hostname: "127.0.0.1", port: 0, fetch(request) {
    const url = new URL(request.url);
    if (url.pathname.endsWith("checksums.txt")) return new Response(`${sum}  ${asset}\n`);
    if (url.pathname.endsWith(asset)) return new Response(bytes);
    return new Response("missing", { status: 404 });
  } });
  const env = { ...process.env, MINDWIRE_RELEASE_BASE_URL: `http://127.0.0.1:${mirror.port}`,
    MINDWIRE_RELEASE_CACHE_DIR: path.join(directory, "release-cache") };
  const common = ["--state-dir", directory, "--json"];
  try {
    const start = await run(["start", ...common, "--relay", "none", "--daemon-bin", binary!, "--directory", directory, "--bind", "127.0.0.1", "--port", "0", "--websocket-port", "0"], env);
    expect(start.code, start.output).toBe(0);
    const client = await computerClient(directory);
    const before = await client.computer.info();
    await client.execution.terminals.open({ id: "blocks-update", columns: 80, rows: 24, directory });
    await client.computer.requestUpdate(version);
    async function status() { return JSON.parse(await readFile(path.join(directory, "computer-update.json"), "utf8")) as { status: string; error?: string }; }
    let waiting = false;
    for (let attempt = 0; attempt < 100; attempt++) {
      if ((await status()).status === "waiting") { waiting = true; break; }
      await Bun.sleep(100);
    }
    expect(waiting).toBe(true);
    expect((await client.computer.info()).pid).toBe(before.pid);
    expect((await client.execution.terminals.get("blocks-update")).running).toBe(true);
    await client.execution.terminals.close("blocks-update");
    let failed = false;
    for (let attempt = 0; attempt < 200; attempt++) {
      if ((await status()).status === "failed") { failed = true; break; }
      await Bun.sleep(100);
    }
    expect(failed).toBe(true);
    expect((await status()).error).toContain("different release version");
    const restored = await computerClient(directory);
    const after = await restored.computer.info();
    expect(after.computerId).toBe(before.computerId);
    expect(after.fingerprint).toBe(before.fingerprint);
    expect(after.sshPort).toBe(before.sshPort);
    expect(after.websocketPort).toBe(before.websocketPort);
  } finally {
    await run(["stop", ...common, "--force"], env).catch(() => {});
    mirror.stop(true);
    await rm(directory, { recursive: true, force: true });
  }
}, 90_000);
