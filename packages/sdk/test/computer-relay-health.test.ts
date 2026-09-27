import { expect, test } from "bun:test";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { WebSocketServer } from "ws";
import { checkRelay, relayNeedsRestart, RelayCheckError, resolveCloudflareAddress } from "../src/computer/relay-health.js";

test("DNS caches never rotate a live address; persistent tunnel failures can recover", () => {
  const dns = new RelayCheckError("dns", "DNS has not caught up");
  expect(relayNeedsRestart(dns, 10, 90_000)).toBe(false);
  expect(relayNeedsRestart(dns, 100, 3_600_000)).toBe(false);
  const expired = new RelayCheckError("dns", "Provider confirms this address is gone", undefined, true);
  expect(relayNeedsRestart(expired, 3, 89_999)).toBe(false);
  expect(relayNeedsRestart(expired, 3, 90_000)).toBe(true);
  const proxy = new RelayCheckError("proxy", "The provider lost its origin");
  expect(relayNeedsRestart(proxy, 1, 90_000)).toBe(false);
  expect(relayNeedsRestart(proxy, 3, 89_999)).toBe(false);
  expect(relayNeedsRestart(proxy, 3, 90_000)).toBe(true);
});

test("provider DNS bypasses stale caches and distinguishes retired addresses from DNS outages", async () => {
  const hostname = "dns-fixture.trycloudflare.com";
  let status = 0, calls = 0;
  const request: typeof fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input));
    expect(url.origin).toBe("https://cloudflare-dns.com");
    expect(url.searchParams.get("name")).toBe(hostname);
    calls++;
    const type = Number(url.searchParams.get("type"));
    return new Response(JSON.stringify({ Status: status, Question: [{ name: hostname + ".", type }],
      Answer: status === 0 ? [{ type, data: type === 1 ? "104.16.230.132" : "2606:4700::6810:e684" }] : undefined }));
  }) as typeof fetch;
  expect(await resolveCloudflareAddress(hostname, 0, { fetch: request })).toEqual({
    addresses: [{ address: "104.16.230.132", family: 4 }, { address: "2606:4700::6810:e684", family: 6 }], missing: false });
  expect(calls).toBe(2);
  status = 3;
  expect(await resolveCloudflareAddress(hostname, 4, { fetch: request })).toEqual({ addresses: [], missing: true });
  status = 2; // SERVFAIL is not proof that an address expired.
  expect(await resolveCloudflareAddress(hostname, 4, { fetch: request })).toEqual({ addresses: [], missing: false });
  const before = calls;
  expect(await resolveCloudflareAddress("private.example", 4, { fetch: request })).toEqual({ addresses: [], missing: false });
  expect(calls).toBe(before);
});

test("provider DNS ignores malformed, unrelated and oversized answers and bounds network waits", async () => {
  const hostname = "dns-fixture.trycloudflare.com";
  const unknown = { addresses: [], missing: false };
  for (const body of [
    "not JSON", "x".repeat(16_385),
    JSON.stringify({ Status: 3, Question: [{ name: "another.trycloudflare.com", type: 1 }] }),
    JSON.stringify({ Status: 0, Question: [{ name: hostname, type: 1 }], Answer: [{ type: 1, data: "not an IP" }] }),
  ]) {
    expect(await resolveCloudflareAddress(hostname, 4, { fetch: (async () => new Response(body)) as typeof fetch })).toEqual(unknown);
  }
  let aborted = false;
  const stalled = (async (_input: unknown, options: RequestInit) => new Promise<Response>((_resolve, reject) => {
    options.signal!.addEventListener("abort", () => { aborted = true; reject(options.signal!.reason); }, { once: true });
  })) as typeof fetch;
  expect(await resolveCloudflareAddress(hostname, 4, { fetch: stalled, timeoutMs: 10 })).toEqual(unknown);
  expect(aborted).toBe(true);
});

test("DNS propagation is identified separately from a disconnected tunnel", async () => {
  // The published CLI runs in Node. Bun substitutes its native WebSocket for
  // `ws` and ignores its custom lookup, so exercise the actual shipped runtime.
  const directory = await mkdtemp(join(tmpdir(), "mindwire-relay-dns-"));
  try {
    const build = await Bun.build({ entrypoints: [new URL("../src/computer/relay-health.ts", import.meta.url).pathname],
      target: "node", format: "esm", outdir: directory });
    expect(build.success).toBe(true);
    const child = Bun.spawn(["node", "--input-type=module", "--eval", `
      import assert from 'node:assert/strict';
      import { checkRelay, RelayCheckError } from ${JSON.stringify(join(directory, "relay-health.js"))};
      for (const code of ['ENOTFOUND', 'EAI_AGAIN']) {
        const lookup = (_host, _options, callback) => callback(Object.assign(new Error('resolver fixture'), { code }), '', 0);
        await assert.rejects(checkRelay({ kind: 'websocket', url: 'wss://pending-fixture.trycloudflare.com/ssh' }, { lookup }), error => {
          assert(error instanceof RelayCheckError);
          assert.equal(error.code, 'dns');
          assert.equal(error.addressMissing, false);
          assert.match(error.message, /Waiting for Cloudflare's internet address/);
          assert.doesNotMatch(error.message, /Reconnecting|resolver fixture/);
          return true;
        });
      }
      const expired = (_host, _options, callback) => callback(Object.assign(new Error('retired address'),
        { code: 'ENOTFOUND', relayAddressMissing: true }), '', 0);
      await assert.rejects(checkRelay({ kind: 'websocket', url: 'wss://retired.trycloudflare.com/ssh' }, { lookup: expired }),
        error => error instanceof RelayCheckError && error.code === 'dns' && error.addressMissing);
    `], { stdout: "pipe", stderr: "pipe" });
    const output = await new Response(child.stderr).text();
    expect(await child.exited, output).toBe(0);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("relay readiness requires a binary SSH banner through the WebSocket upgrade", async () => {
  const server = new WebSocketServer({ port: 0, host: "127.0.0.1" });
  await once(server, "listening");
  let mode: "ssh" | "other" | "text" | "silent" = "ssh";
  server.on("connection", socket => {
    if (mode === "ssh") { socket.send(Buffer.from("SSH-2.0-")); socket.send(Buffer.from("Mindwire\r\n")); }
    if (mode === "other") socket.send(Buffer.from("SSH-2.0-OtherServer\r\n"));
    if (mode === "text") socket.send("SSH-2.0-Mindwire\r\n");
  });
  const route = { kind: "websocket" as const, url: `ws://127.0.0.1:${(server.address() as AddressInfo).port}/ssh` };
  try {
    await checkRelay(route);
    mode = "other";
    await expect(checkRelay(route)).rejects.toThrow("isn't connected to the Mindwire SSH service");
    mode = "text";
    await expect(checkRelay(route)).rejects.toThrow("invalid SSH response");
    mode = "silent";
    await expect(checkRelay(route, { timeoutMs: 50 })).rejects.toThrow("isn't responding");
    const cancellation = new AbortController();
    const pending = checkRelay(route, { signal: cancellation.signal });
    cancellation.abort(new Error("Owner stopped the check"));
    await expect(pending).rejects.toThrow("Owner stopped the check");
  } finally {
    for (const client of server.clients) client.terminate();
    server.close();
  }
});

test("an HTTP response from a live proxy cannot count as a working SSH relay", async () => {
  const server = createServer((_request, response) => { response.writeHead(503); response.end("Tunnel unavailable"); });
  server.on("upgrade", (_request, socket) => socket.end("HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"));
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  try {
    await expect(checkRelay({ kind: "websocket", url: `ws://127.0.0.1:${(server.address() as AddressInfo).port}/ssh` }))
      .rejects.toThrow("internet tunnel");
  } finally {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
