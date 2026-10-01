import { expect, test } from "bun:test";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:http";
import { once } from "node:events";
import type { AddressInfo } from "node:net";
import type { Mindwire } from "../src/client.js";
import type { ComputerDiscoveryStatus } from "../src/computer.js";
import { DIRECTORY_URL, directoryURL, enableAddressDiscovery } from "../src/computer/discovery.js";
import { chooseConnectionAction } from "../src/computer/connection-flow.js";
import { ComputerDirectoryFixture } from "./fixtures/computer-directory.js";
import { enroll, envelope, header, keys } from "./fixtures/directory-protocol.js";

function computerStub(fixture: ComputerDirectoryFixture) {
  const host = keys();
  const state = {
    status: { version: 1, enabled: false, directoryId: host.id } as ComputerDiscoveryStatus,
    configured: 0,
  };
  const computer = {
    computer: {
      discovery: async () => state.status,
      directoryEnrollment: async (url: string, challenge: string) =>
        envelope(host, "mindwire-directory-enroll-v1", {
          ...header(fixture, host),
          audience: url,
          expiresAt: fixture.now() + 300,
          challenge,
        }),
      configureDiscovery: async (_: boolean, url: string) => {
        state.configured++;
        state.status = { ...state.status, enabled: true, url, sequence: 1, publishedSequence: 1 };
        return state.status;
      },
    },
  } as unknown as Mindwire;
  return { computer, state };
}

test("CLI enrolls without login, coalesces setup and persists a self-hosted directory", async () => {
  const directory = await mkdtemp(join(tmpdir(), "directory-setup-"));
  const fixture = await new ComputerDirectoryFixture().start();
  const { computer, state } = computerStub(fixture);
  try {
    const options = { directory, computer, prompt: fixture.prompt(), url: fixture.url };
    const results = await Promise.all([enableAddressDiscovery(options), enableAddressDiscovery(options)]);
    expect(results.every((value) => value.enabled)).toBe(true);
    expect(state.configured).toBe(1);
    expect(fixture.enrollments).toBe(1);
    expect(
      fixture.requests.every(
        (request) => !request.authorized && !request.cookie && !request.path.startsWith("/api/account"),
      ),
    ).toBe(true);
    expect((await readdir(directory, { withFileTypes: true })).filter((entry) => entry.isFile())).toEqual([]);
    expect(await fixture.control("accountCount")).toBe(0);
    const previousRequests = fixture.requests.length;
    await enableAddressDiscovery({ directory, computer, prompt: fixture.prompt() });
    expect(fixture.requests.length).toBe(previousRequests);
    expect(state.status.url).toBe(fixture.url);
    await expect(
      enableAddressDiscovery({ ...options, url: "https://another.example/api/computer-directory/v1" }),
    ).rejects.toThrow("Disable the current directory");
    expect(state.configured).toBe(1);
  } finally {
    await fixture.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 30_000);

test("directory capacity and outages leave the working configuration alone", async () => {
  const directory = await mkdtemp(join(tmpdir(), "directory-failed-"));
  const fixture = await new ComputerDirectoryFixture({ maxRegistrations: 1 }).start();
  const { computer, state } = computerStub(fixture);
  try {
    await enroll(fixture, keys());
    await expect(
      enableAddressDiscovery({ directory, computer, url: fixture.url, prompt: fixture.prompt() }),
    ).rejects.toThrow("at capacity");
    expect(state.configured).toBe(0);
    expect((await readdir(directory, { withFileTypes: true })).filter((entry) => entry.isFile())).toEqual([]);
    fixture.offline = true;
    await expect(
      enableAddressDiscovery({ directory, computer, url: fixture.url, prompt: fixture.prompt() }),
    ).rejects.toThrow("temporarily unavailable");
    expect(state.configured).toBe(0);
  } finally {
    await fixture.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 30_000);

test("enrollment refuses redirects without forwarding a proof or credential", async () => {
  const directory = await mkdtemp(join(tmpdir(), "directory-redirect-"));
  const fixture = await new ComputerDirectoryFixture().start();
  const { computer, state } = computerStub(fixture);
  let hits = 0;
  const sink = createServer((_request, response) => {
    hits++;
    response.end("{}");
  });
  sink.listen(0, "127.0.0.1");
  await once(sink, "listening");
  fixture.redirectTo = `http://127.0.0.1:${(sink.address() as AddressInfo).port}/capture`;
  try {
    await expect(
      enableAddressDiscovery({ directory, computer, url: fixture.url, prompt: fixture.prompt() }),
    ).rejects.toThrow("securely");
    expect(hits).toBe(0);
    expect(state.configured).toBe(0);
  } finally {
    sink.closeAllConnections();
    await new Promise<void>((resolve) => sink.close(() => resolve()));
    await fixture.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 15_000);

test("an older account-only directory asks for a backend update, never a login", async () => {
  const directory = await mkdtemp(join(tmpdir(), "directory-old-backend-"));
  const server = createServer((_request, response) => {
    response.writeHead(401, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: { code: "account_required" } }));
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const fixture = new ComputerDirectoryFixture();
  fixture.url = `http://127.0.0.1:${(server.address() as AddressInfo).port}/api/computer-directory/v1`;
  const { computer, state } = computerStub(fixture);
  try {
    await expect(
      enableAddressDiscovery({ directory, computer, url: fixture.url, prompt: fixture.prompt() }),
    ).rejects.toThrow("Update its Mindwire backend");
    expect(state.configured).toBe(0);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
    await rm(directory, { recursive: true, force: true });
  }
});

test("an environment-selected directory is saved and a pending withdrawal blocks a URL change", async () => {
  const directory = await mkdtemp(join(tmpdir(), "directory-environment-"));
  const fixture = await new ComputerDirectoryFixture().start();
  const { computer, state } = computerStub(fixture);
  const old = process.env.MINDWIRE_DIRECTORY_URL;
  try {
    process.env.MINDWIRE_DIRECTORY_URL = fixture.url;
    await enableAddressDiscovery({ directory, computer, prompt: fixture.prompt() });
    expect(state.status.url).toBe(fixture.url);
    state.status = { ...state.status, enabled: false, sequence: 2, publishedSequence: 1 };
    await expect(
      enableAddressDiscovery({
        directory,
        computer,
        prompt: fixture.prompt(),
        url: "https://elsewhere.example",
      }),
    ).rejects.toThrow("wait for its withdrawal");
    expect(state.configured).toBe(1);
  } finally {
    if (old === undefined) delete process.env.MINDWIRE_DIRECTORY_URL;
    else process.env.MINDWIRE_DIRECTORY_URL = old;
    await fixture.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 15_000);

test("directory URLs use the existing Console and require HTTPS beyond loopback", () => {
  expect(DIRECTORY_URL).toBe("https://console.mindwire.sh/api/computer-directory/v1");
  expect(directoryURL("https://own.example")).toBe("https://own.example/api/computer-directory/v1");
  expect(directoryURL("http://127.0.0.1:8080/api/computer-directory/v1")).toContain("127.0.0.1");
  for (const value of [
    "http://public.example/api",
    "https://user:pass@own.example/api",
    "https://own.example/api?token=hidden",
    "https://own.example/api#fragment",
  ]) {
    expect(() => directoryURL(value)).toThrow();
  }
});

test("saved phones with an acknowledged directory can resume a temporary address", async () => {
  const phone = {
    id: "phone",
    publicKey: "key",
    name: "iPhone",
    revoked: false,
    createdAt: "now",
    addressRecovery: true,
  };
  expect(await chooseConnectionAction({ devices: [phone], requested: "reconnect", persistent: false })).toBe(
    "resume",
  );
  expect(
    await chooseConnectionAction({ devices: [{ ...phone, addressRecovery: false }], persistent: false }),
  ).toBe("pair");
});
