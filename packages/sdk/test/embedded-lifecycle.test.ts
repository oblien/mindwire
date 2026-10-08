import { afterEach, expect, test } from "bun:test";
import { mkdtemp, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Mindwire, local, startEmbedded, type TargetHandle } from "../src/index.js";

const directories: string[] = [];
const owned: Array<{ stop(): Promise<void> }> = [];
afterEach(async () => {
  await Promise.all(owned.splice(0).map(handle => handle.stop()));
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })));
});

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "mindwire-local-test-"));
  directories.push(root);
  const bin = join(root, "daemon");
  // A process-level HTTP fixture exercises the SDK's real spawn/readiness/kill path.
  // Native auth decoding and child-environment stripping have Go adapter tests.
  await writeFile(bin, `#!${process.execPath}
import { createServer } from 'node:http';
import { writeFileSync } from 'node:fs';
writeFileSync('pid', String(process.pid));
const server = createServer((req, res) => {
  if (req.headers.authorization !== 'Bearer ' + process.env.DAEMON_TOKEN) { res.writeHead(401).end(); return; }
  res.setHeader('content-type', 'application/json');
  res.end(JSON.stringify({ok:true, nativeAuthSource:process.env.LEGACY !== '1', pid:process.pid,
    home:process.env.HOME, cwd:process.cwd(), source:JSON.parse(process.env.MINDWIRE_AUTH_SOURCE || 'null'),
    hostOnly:process.env.MINDWIRE_TEST_HOST_ONLY}));
});
const [hostname, port] = process.env.ADDR.split(':');
server.listen(Number(port), hostname);
process.on('SIGTERM', () => server.close(() => process.exit(0)));
`, { mode: 0o700 });
  return { root, bin };
}

const alive = (pid: number) => { try { process.kill(pid, 0); return true; } catch { return false; } };

test("owned local target isolates its environment, forwards native auth source, and closes across SDK views", async () => {
  const { root, bin } = await fixture();
  process.env.MINDWIRE_TEST_HOST_ONLY = "not-in-child";
  try {
    const target = local({ bin, cwd: root, shared: false, environment: { HOME: root },
      authSource: { home: "/host-account", reuse: "manual", environment: { NAMED_CREDENTIAL: "fixture" } } });
    let handle: TargetHandle | undefined;
    const mw = new Mindwire({ target: { name: target.name, async connect(spec) {
      handle = await target.connect(spec); owned.push(handle); return handle;
    } } });
    await Promise.all([mw.ensure(), mw.withAgent("codex").ensure()]);
    const status = await mw.health() as any;
    expect(status.home).toBe(root);
    expect(status.cwd).toBe(await realpath(root));
    expect(status.hostOnly).toBeUndefined();
    expect(status.source).toEqual({ home: "/host-account", reuse: "manual", environment: { NAMED_CREDENTIAL: "fixture" } });
    expect(handle?.isAlive?.()).toBe(true);
    await mw.withAgent("codex").close();
    await mw.close();
    expect(handle?.isAlive?.()).toBe(false);
    expect(alive(status.pid)).toBe(false);
  } finally { delete process.env.MINDWIRE_TEST_HOST_ONLY; }
});

test("shared embedded processes are reused while alive and invalidated after stop", async () => {
  const { root, bin } = await fixture();
  const options = { bin, cwd: root, environment: { HOME: root } };
  const [a, b] = await Promise.all([startEmbedded(options), startEmbedded(options)]);
  owned.push(a, b);
  expect(a).toBe(b);
  await a.stop();
  const c = await startEmbedded(options);
  owned.push(c);
  expect(c).not.toBe(a);
  expect(c.isAlive()).toBe(true);
  // Normal shared clients do not terminate another client's process on close.
  const handle = await local(options).connect({});
  await handle.stop();
  expect(c.isAlive()).toBe(true);
});

test("native auth source rejects an older daemon and reaps the failed startup", async () => {
  const { root, bin } = await fixture();
  await expect(startEmbedded({ bin, cwd: root, shared: false, environment: { LEGACY: "1" },
    authSource: { home: "/host-account" } })).rejects.toThrow("does not support native auth sources");
  const pid = Number(await readFile(join(root, "pid"), "utf8"));
  expect(alive(pid)).toBe(false);
});

test("automatic and manual reuse do not share a daemon; automatic is the default", async () => {
  const { root, bin } = await fixture();
  const options = { bin, cwd: root, environment: { HOME: root }, authSource: { home: "/host-account" } };
  const [automatic, explicit, manual] = await Promise.all([
    startEmbedded(options),
    startEmbedded({ ...options, authSource: { ...options.authSource, reuse: "automatic" } }),
    startEmbedded({ ...options, authSource: { ...options.authSource, reuse: "manual" } }),
  ]);
  owned.push(automatic, explicit, manual);
  expect(automatic).toBe(explicit);
  expect(manual).not.toBe(automatic);
});
