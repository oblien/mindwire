import { expect, test } from "bun:test";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { WebSocketServer } from "ws";
import { checkRelay, relayNeedsRestart, RelayCheckError } from "../src/computer/relay-health.js";

test("DNS caches never rotate a live address; persistent tunnel failures can recover", () => {
  const dns = new RelayCheckError("dns", "DNS has not caught up");
  expect(relayNeedsRestart(dns, 10, 90_000)).toBe(false);
  expect(relayNeedsRestart(dns, 100, 3_600_000)).toBe(false);
  const proxy = new RelayCheckError("proxy", "The provider lost its origin");
  expect(relayNeedsRestart(proxy, 1, 90_000)).toBe(false);
  expect(relayNeedsRestart(proxy, 3, 89_999)).toBe(false);
  expect(relayNeedsRestart(proxy, 3, 90_000)).toBe(true);
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
          assert.match(error.message, /Waiting for Cloudflare's internet address/);
          assert.doesNotMatch(error.message, /Reconnecting|resolver fixture/);
          return true;
        });
      }
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
