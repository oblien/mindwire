import { expect, test } from "bun:test";
import { once } from "node:events";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { WebSocketServer } from "ws";
import { checkRelay } from "../src/computer/relay-health.js";

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
