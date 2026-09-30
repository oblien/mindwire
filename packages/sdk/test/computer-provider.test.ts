import { expect, test } from "bun:test";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { once } from "node:events";
import { createServer } from "node:https";
import type { AddressInfo } from "node:net";
import type { Oblien } from "oblien";
import { OblienTunnelLease, relayFailureMessage } from "../src/computer/managed-relay.js";
import { authenticatedOblien, boundedOblienClient, tokenExpiry } from "../src/computer/oblien-auth.js";
import { prepareOblienTunnel, providerHostname, chooseProvider, setupProvider } from "../src/computer/provider-setup.js";
import { readJSON, writeJSON } from "../src/computer/lifecycle.js";
import { connectionAddress, connectionInfo } from "../src/computer/provider-info.js";
import { validateRelayOptions } from "../src/computer/relay.js";
import { chooseConnectionAction } from "../src/computer/connection-flow.js";
import { providerFailure, providerNeedsAction } from "../src/computer/provider-errors.js";
import { installRelayFixture } from "./fixtures/relay-fixture.js";

(process.platform !== "win32" ? test : test.skip)("Cloudflare setup reuses account/tunnel identity and ngrok setup keeps its token private", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-provider-setup-"));
  const previousPath = process.env.PATH, previousCertificate = process.env.TUNNEL_ORIGIN_CERT;
  const callsFile = join(directory, "calls.jsonl");
  try {
    const certificate = join(directory, "existing-cert.pem"); await writeFile(certificate, "fixture");
    await writeFile(join(directory, "cloudflared"), `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.appendFileSync(${JSON.stringify(callsFile)}, JSON.stringify(args)+'\\n');
if (args[0] === '--version') process.exit(0);
if (args[1] === 'login') throw new Error('Existing login must be reused');
if (args[1] === 'create') {
  if (args[2] !== '--credentials-file') throw new Error('Wrong CLI argument order');
  fs.writeFileSync(args[3], JSON.stringify({TunnelID:'12345678-abcd-abcd-abcd-123456789abc',TunnelSecret:'fixture'}));
} else if (!(args[1] === 'route' && args[2] === 'dns') || args.includes('--overwrite-dns')) throw new Error('Unexpected or destructive provider operation');
`, { mode: 0o700 });
    process.env.PATH = directory + delimiter + (previousPath ?? "");
    process.env.TUNNEL_ORIGIN_CERT = certificate;
    const messages: string[] = [];
    const prompt = { question: async () => "laptop.example.com", secret: async () => "private-ngrok-test-token",
      print: (text: string) => { messages.push(text); }, open: (url: string) => { messages.push(url); } };
    const first = await setupProvider({ directory, provider: "cloudflare", prompt });
    const again = await setupProvider({ directory, provider: "cloudflare", prompt });
    expect(again).toEqual(first);
    const calls = (await readFile(callsFile, "utf8")).trim().split("\n").map(line => JSON.parse(line) as string[]);
    expect(calls.filter(args => args[1] === "create").length).toBe(1);
    expect(calls.filter(args => args[1] === "login").length).toBe(0);
    expect(first.relay?.url).toBe("wss://laptop.example.com/ssh");
    expect((await stat(first.relay!.cloudflareCredentialsFile!)).mode & 0o777).toBe(0o600);
    const ngrok = await setupProvider({ directory, provider: "ngrok", prompt });
    expect(ngrok.relay?.url).toBe("wss://laptop.example.com/ssh");
    expect((await stat(ngrok.relay!.ngrokTokenFile!)).mode & 0o777).toBe(0o600);
    expect(await readJSON(ngrok.relay!.ngrokTokenFile!)).toEqual({ token: "private-ngrok-test-token" });
    expect(JSON.stringify(ngrok) + messages.join("\n")).not.toContain("private-ngrok-test-token");
  } finally {
    if (previousPath === undefined) delete process.env.PATH; else process.env.PATH = previousPath;
    if (previousCertificate === undefined) delete process.env.TUNNEL_ORIGIN_CERT; else process.env.TUNNEL_ORIGIN_CERT = previousCertificate;
    await rm(directory, { recursive: true, force: true });
  }
});

test("persistent saved connections resume without claiming the phone is connected; explicit pair repairs lost keys", async () => {
  const phone = { id: "phone", publicKey: "fixture", name: "iPhone", createdAt: "now", revoked: false, connected: false };
  for (const requested of [undefined, "resume", "reconnect"] as const) {
    expect(await chooseConnectionAction({ devices: [phone], persistent: true, requested })).toBe("resume");
    expect(await chooseConnectionAction({ devices: [], persistent: true, requested })).toBe("pair");
    expect(await chooseConnectionAction({ devices: [{ ...phone, revoked: true }], persistent: true, requested })).toBe("pair");
  }
  expect(await chooseConnectionAction({ devices: [phone], persistent: true, requested: "pair" })).toBe("pair");
  expect(await chooseConnectionAction({ devices: [phone], persistent: false })).toBe("pair");
  expect(connectionInfo({ kind: "cloudflare" }).address).toBe("temporary");
  expect(connectionInfo({ kind: "ngrok" }).address).toBe("temporary");
  expect(connectionInfo({ kind: "none" }).address).toBe("network");
  expect(connectionInfo({ kind: "custom", url: "wss://laptop.example/ssh" }).address).toBe("persistent");
  expect(connectionAddress({ kind: "ngrok", url: "https://laptop.example", ngrokTokenFile: "new-token.json" }))
    .toBe(connectionAddress({ kind: "ngrok", url: "wss://laptop.example/ssh", ngrokTokenFile: "old-token.json" }));
});

test("explicit Oblien setup replaces a removed tunnel without adopting another computer", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-oblien-deleted-"));
  const file = join(directory, "provider.json");
  let created = 0;
  const client = { edgeTunnel: {
    get: async () => { throw Object.assign(new Error("Not found"), { status: 404 }); },
    list: async () => ({ tunnels: [{ id: 9, slug: "other-computer", port: 8792 }] }),
    create: async (data: { slug: string; port: number }) => {
      created++;
      return { tunnel: { ...data, id: 10, status: "active", url: `https://${data.slug}.preview.oblien.com` } };
    },
  } } as unknown as Oblien;
  try {
    await writeJSON(file, { installationId: "stable-installation", oblien: { id: 8, url: "wss://old.example/ssh" } });
    const restored = await prepareOblienTunnel(client, file, 8792);
    expect(restored.id).toBe(10); expect(created).toBe(1);
    expect(restored.url).toContain("mw-stableinstallation.");
    client.edgeTunnel.get = async () => { throw Object.assign(new Error("Forbidden"), { status: 403 }); };
    await expect(prepareOblienTunnel(client, file, 8792)).rejects.toThrow("Forbidden");
    expect(created).toBe(1);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("provider API deadlines cancel stalled SDK operations and preserve caller cancellation", async () => {
  let requests = 0;
  const client = { _http: { request: async ({ signal }: { signal: AbortSignal }) => {
    requests++;
    signal.throwIfAborted();
    return await new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(signal.reason), { once: true }));
  } } } as unknown as Oblien;
  boundedOblienClient(client, 20);
  await expect(client._http.request({ method: "POST", path: "/edge/tunnels/1/token" })).rejects.toThrow("did not respond in time");
  const cancelled = new AbortController(); cancelled.abort(new Error("Setup cancelled"));
  await expect(client._http.request({ method: "POST", path: "/edge/tunnels", signal: cancelled.signal })).rejects.toThrow("Setup cancelled");
  expect(requests).toBe(2); // A timed-out mutation is not silently replayed.
});

test("provider certificate/auth failures are actionable and never echo transport credentials", () => {
  const secret = "never-log-this-provider-secret";
  for (const code of ["CERT_HAS_EXPIRED", "ERR_TLS_CERT_ALTNAME_INVALID", "DEPTH_ZERO_SELF_SIGNED_CERT"]) {
    const failure = providerFailure(new Error(secret, { cause: Object.assign(new Error(secret), { code }) }), "Oblien");
    expect(failure.code).toBe("provider_certificate"); expect(providerNeedsAction(failure.code)).toBe(true);
    expect(failure.message).not.toContain(secret);
  }
  expect(providerFailure(new Error("Unexpected server response: 401"), "Oblien").code).toBe("provider_sign_in");
  expect(providerNeedsAction(providerFailure(new Error(secret), "Oblien").code)).toBe(false);
});

test("the shipped Oblien relay exits promptly on an invalid broker certificate without weakening TLS", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-broker-tls-"));
  const untrusted = await mkdtemp(join(tmpdir(), "mindwire-untrusted-broker-"));
  const certificate = await installRelayFixture(directory);
  await installRelayFixture(untrusted);
  const credentials = async (folder: string) => ({ key: await readFile(join(folder, "relay-key.pem")), cert: await readFile(join(folder, "relay-certificate.pem")) });
  const broker = createServer(await credentials(untrusted));
  broker.listen(0, "127.0.0.1"); await once(broker, "listening");
  const brokerURL = `wss://127.0.0.1:${(broker.address() as AddressInfo).port}/connect`;
  const expires = Date.now() + 3 * 24 * 3600_000;
  const token = `header.${Buffer.from(JSON.stringify({ exp: Math.floor(expires / 1000) })).toString("base64url")}.never-log-provider-secret`;
  const api = createServer(await credentials(directory), (_request, response) => {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ token, connect_url: brokerURL, expires_at: new Date(expires).toISOString() }));
  });
  api.listen(0, "127.0.0.1"); await once(api, "listening");
  try {
    const file = join(directory, "credentials.json"), config = join(directory, "relay.json");
    await writeJSON(file, { token, baseUrl: `https://127.0.0.1:${(api.address() as AddressInfo).port}` });
    await writeJSON(config, { port: 8792, options: { kind: "oblien", oblienTunnelId: 17, oblienCredentialsFile: file, url: "wss://fixture.example/ssh" } });
    const child = Bun.spawn(["node", resolve(import.meta.dir, "../dist/cli.js"), "_relay", "--relay-config", config], {
      env: { ...process.env, NODE_EXTRA_CA_CERTS: certificate }, stdout: "pipe", stderr: "pipe",
    });
    const watchdog = setTimeout(() => child.kill(), 10_000);
    try {
      const [stdout, stderr, code] = await Promise.all([new Response(child.stdout).text(), new Response(child.stderr).text(), child.exited]);
      expect(code, stderr).toBe(1);
      const event = JSON.parse(stdout.trim());
      expect(event.event).toBe("relay_error"); expect(event.code).toBe("provider_certificate");
      expect(stdout + stderr).not.toContain("never-log-provider-secret");
    } finally { clearTimeout(watchdog); child.kill(); }
  } finally {
    api.closeAllConnections(); broker.closeAllConnections();
    await Promise.all([new Promise<void>(resolve => api.close(() => resolve())), new Promise<void>(resolve => broker.close(() => resolve()))]);
    await rm(directory, { recursive: true, force: true }); await rm(untrusted, { recursive: true, force: true });
  }
}, 15_000);

test("provider setup exposes account choices and rejects ambiguous/unsafe hostnames", async () => {
  const answers = ["bad", "0", "99", "3"];
  expect(await chooseProvider({ question: async () => answers.shift()!, secret: async () => "", print() {}, open() {} })).toBe("ngrok");
  expect(providerHostname("laptop.example.com")).toBe("laptop.example.com");
  for (const value of ["https://name:secret@example.com", "https://example.com/#secret", "http://example.com", "example.com/path", "example.com?token=x"]) {
    expect(() => providerHostname(value)).toThrow();
  }
  expect(() => validateRelayOptions({ kind: "oblien", url: "wss://valid.example/ssh" })).toThrow("setup is incomplete");
  expect(() => validateRelayOptions({ kind: "cloudflare", url: "wss://valid.example/ssh" })).toThrow("credentials");
});

test("Oblien tunnels are bound to the installation, not another computer's matching port", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-oblien-identity-"));
  const file = join(directory, "provider.json");
  let created = 0, requested = 0;
  const tunnels = [{ id: 99, slug: "unrelated-computer", port: 8792, url: "https://other.preview.oblien.com", status: "active" }];
  const client = { edgeTunnel: {
    list: async () => ({ tunnels }),
    create: async (value: { slug: string; port: number }) => {
      created++;
      const tunnel = { ...value, id: 123, url: `https://${value.slug}.preview.oblien.com`, status: "active" };
      tunnels.push(tunnel);
      return { tunnel };
    },
    get: async (id: number) => { requested = id; return { tunnel: tunnels.find(t => t.id === id)! }; },
  } } as unknown as Oblien;
  try {
    const first = await prepareOblienTunnel(client, file, 8792);
    const resumed = await prepareOblienTunnel(client, file, 8792);
    expect(resumed).toEqual(first);
    expect(created).toBe(1); expect(requested).toBe(123);
    expect(first.id).not.toBe(99);
    const state = (await readJSON<{ installationId: string }>(file))!;
    await writeJSON(file, { installationId: state.installationId }); // Lost create response / crash before saving ID.
    expect(await prepareOblienTunnel(client, file, 8792)).toEqual(first);
    expect(created).toBe(1);
    expect((await stat(file)).mode & 0o777).toBe(0o600);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("broker credentials renew once before expiry and every renewal targets the same tunnel", async () => {
  let now = 1_000_000, calls = 0;
  const ids: number[] = [];
  const client = { edgeTunnel: { issueToken: async (id: number) => {
    ids.push(id); calls++;
    await Promise.resolve();
    return { token: `secret-${calls}`, connect_url: "wss://edge.oblien.com/connect", expires_at: new Date(now + 3600_000).toISOString() };
  } } } as unknown as Oblien;
  const lease = new OblienTunnelLease(57, async () => client, () => now);
  const first = await Promise.all(Array.from({ length: 8 }, () => lease.get()));
  expect(calls).toBe(1); expect(new Set(first.map(t => t.token)).size).toBe(1);
  now += 60_000; await lease.get(); expect(calls).toBe(1);
  now += 3300_000;
  const renewed = await Promise.all([lease.get(), lease.get(), lease.get()]);
  expect(calls).toBe(2); expect(renewed[0]!.token).not.toBe(first[0]!.token);
  expect(ids).toEqual([57, 57]);
});

test("an expired browser session is never exchanged for an anonymous one; errors never expose credentials", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-oblien-auth-"));
  const file = join(directory, "credentials.json");
  const jwt = (expires: number) => `header.${Buffer.from(JSON.stringify({ exp: expires })).toString("base64url")}.private-signature`;
  let requests = 0;
  try {
    await writeJSON(file, { token: jwt(10) });
    await expect(authenticatedOblien(file, { now: () => 20_000, client: () => ({ _http: { request: async () => { requests++; } } }) as unknown as Oblien }))
      .rejects.toThrow("Sign in to Oblien again");
    expect(requests).toBe(0);
    expect(tokenExpiry("broken")).toBeUndefined();
    expect(relayFailureMessage(new Error("Authorization: Bearer secret-credential"), "Oblien")).not.toContain("secret-credential");
  } finally { await rm(directory, { recursive: true, force: true }); }
});
