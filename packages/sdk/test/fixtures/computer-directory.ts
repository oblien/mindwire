// A disposable instance of the existing Console backend, including real Better
// Auth and database migrations. The proxy injects network failures only.
import { createServer, request as httpRequest, type Server } from "node:http";
import { createHash, randomBytes } from "node:crypto";
import { once } from "node:events";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import type { AddressInfo } from "node:net";
import { DIRECTORY_API_PATH } from "../../src/computer/discovery.js";
import type { SetupPrompt } from "../../src/computer/setup-prompt.js";

const consoleDirectory = fileURLToPath(new URL("../../../../apps/console/", import.meta.url));
const consoleRequire = createRequire(join(consoleDirectory, "package.json"));
export interface TestAccount {
  cookie: string;
  id: string;
  email: string;
}
export interface LegacyDirectoryRow {
  directoryId: string;
  ownerId: string;
  computerId: string;
  publicKey: string;
  revoked: number;
  sequence: number;
  digest: string | null;
  enabled: number;
  expiresAt: number;
  createdAt: number;
  publication: string | null;
}

export function directoryKeyId(raw: Buffer): string {
  return createHash("sha256")
    .update(
      Buffer.concat([
        Buffer.from([0, 0, 0, 11]),
        Buffer.from("ssh-ed25519"),
        Buffer.from([0, 0, 0, 32]),
        raw,
      ]),
    )
    .digest("base64url");
}

async function unusedPort(): Promise<number> {
  const server = createServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = (server.address() as AddressInfo).port;
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return port;
}

export class ComputerDirectoryFixture {
  url = "";
  directory = "";
  private server?: Server;
  private backend?: ChildProcess;
  private backendPort = 0;
  private authSecret = randomBytes(48).toString("base64url");
  private encryptionKey = randomBytes(32).toString("base64url");
  private databaseURL?: string;
  private cleanupDatabase?: () => Promise<void>;
  private rpcId = 0;
  offline = false;
  redirectTo?: string;
  dropPublishReceipts = 0;
  readonly publishDigests: string[] = [];
  readonly requests: { method: string; path: string; authorized: boolean; cookie: boolean }[] = [];
  lookups = 0;
  enrollments = 0;
  now = () => Math.floor(Date.now() / 1000);

  constructor(
    readonly options: {
      databaseURL?: string;
      maxRegistrations?: number;
      enrollmentsPerDay?: number;
      trustedProxies?: string;
      production?: boolean;
      seedLegacy?: (fixture: ComputerDirectoryFixture) => LegacyDirectoryRow[];
    } = {},
  ) {}

  async start(): Promise<this> {
    this.directory = await mkdtemp(join(tmpdir(), "mindwire-directory-console-"));
    this.backendPort = await unusedPort();
    this.server = createServer((request, response) => {
      const path = request.url ?? "/";
      // Record no codes, cookies, grants, payloads or private keys.
      this.requests.push({
        method: request.method ?? "GET",
        path: path.split("?")[0],
        authorized: !!request.headers.authorization,
        cookie: !!request.headers.cookie,
      });
      if (this.offline) {
        response.writeHead(503, { "Content-Type": "application/json" });
        response.end('{"error":{"code":"unavailable"}}');
        return;
      }
      if (this.redirectTo) {
        response.writeHead(307, { Location: this.redirectTo });
        response.end();
        return;
      }
      const publication = request.method === "PUT" && path.startsWith(DIRECTORY_API_PATH + "/records/");
      const digest = createHash("sha256");
      if (publication) request.on("data", (chunk) => digest.update(chunk));
      if (request.method === "POST" && path.endsWith("/lookup")) this.lookups++;
      const upstream = httpRequest(
        {
          hostname: "127.0.0.1",
          port: this.backendPort,
          path,
          method: request.method,
          headers: request.headers,
        },
        (result) => {
          if (publication) this.publishDigests.push(digest.digest("hex"));
          if (
            request.method === "POST" &&
            path === DIRECTORY_API_PATH + "/registrations" &&
            result.statusCode === 201
          )
            this.enrollments++;
          if (publication && result.statusCode === 200 && this.dropPublishReceipts > 0) {
            // The store committed, but an intermediary lost its success receipt.
            // Reply explicitly: Bun's ServerResponse.destroy can otherwise flush
            // a synthetic 200 instead of closing the socket like Node does.
            this.dropPublishReceipts--;
            result.resume();
            response.writeHead(503, { "Content-Type": "application/json" });
            response.end('{"error":{"code":"unavailable"}}');
            return;
          }
          response.writeHead(result.statusCode ?? 503, result.headers);
          result.pipe(response);
        },
      );
      upstream.on("error", () => {
        if (!response.headersSent) response.writeHead(503);
        response.end();
      });
      request.on("error", () => upstream.destroy());
      response.on("close", () => upstream.destroy());
      request.pipe(upstream);
    });
    this.server.listen(0, "127.0.0.1");
    await once(this.server, "listening");
    this.url = `http://127.0.0.1:${(this.server.address() as AddressInfo).port}${DIRECTORY_API_PATH}`;
    try {
      const databaseURL = this.options.databaseURL ?? process.env.MINDWIRE_DIRECTORY_TEST_DATABASE_URL;
      if (databaseURL) {
        // Only a new randomly named test database is ever migrated or dropped.
        const { Pool } = consoleRequire("pg");
        const admin = new Pool({ connectionString: databaseURL });
        const name = "directory_test_" + randomBytes(10).toString("hex");
        try {
          await admin.query(`CREATE DATABASE ${name}`);
        } catch (error) {
          await admin.end();
          throw error;
        }
        const isolated = new URL(databaseURL);
        isolated.pathname = "/" + name;
        this.databaseURL = isolated.href;
        this.cleanupDatabase = async () => {
          try {
            await admin.query(`DROP DATABASE ${name} WITH (FORCE)`);
          } finally {
            await admin.end();
          }
        };
      }
      if (this.options.seedLegacy)
        await writeFile(
          join(this.directory, "legacy-directory.json"),
          JSON.stringify(this.options.seedLegacy(this)),
          { mode: 0o600 },
        );
      await this.startBackend();
      return this;
    } catch (error) {
      await this.close();
      throw error;
    }
  }

  private async startBackend(): Promise<void> {
    const inherited = Object.fromEntries(
      ["PATH", "HOME", "USERPROFILE", "SystemRoot", "TMPDIR", "TEMP", "TMP"].flatMap((key) =>
        process.env[key] ? [[key, process.env[key]!]] : [],
      ),
    );
    this.backend = spawn(
      "node",
      [
        "--import",
        consoleRequire.resolve("tsx"),
        join(consoleDirectory, "test/fixtures/directory-server.ts"),
      ],
      {
        cwd: this.directory,
        stdio: ["ignore", "pipe", "pipe", "ipc"],
        env: {
          ...inherited,
          NODE_ENV: this.options.production ? "production" : "test",
          SECRETS_ENCRYPTION_KEY: this.encryptionKey,
          CONSOLE_MODE: "cloud",
          BASE_URL: new URL(this.url).origin,
          PORT: String(this.backendPort),
          AUTH_SECRET: this.authSecret,
          AUTH_DB_PATH: join(this.directory, "auth.db"),
          ...(this.databaseURL ? { DATABASE_URL: this.databaseURL } : {}),
          COMPUTER_DIRECTORY_MAX_REGISTRATIONS: String(this.options.maxRegistrations ?? 100_000),
          COMPUTER_DIRECTORY_ENROLLMENTS_PER_DAY: String(this.options.enrollmentsPerDay ?? 100),
          COMPUTER_DIRECTORY_TRUSTED_PROXIES: this.options.trustedProxies ?? "",
          ...(this.options.seedLegacy
            ? { MINDWIRE_DIRECTORY_LEGACY_FIXTURE: join(this.directory, "legacy-directory.json") }
            : {}),
          SEED_DEFAULT_DAEMON: "false",
          ALLOW_LOCAL_RUNTIME: "false",
          ALLOW_REMOTE_RUNTIME: "false",
          ALLOW_SSH_RUNTIME: "false",
          ALLOW_DOCKER_RUNTIME: "false",
        },
      },
    );
    let failed = false,
      startupLog = "";
    this.backend.on("error", () => {
      failed = true;
    });
    const record = (chunk: Buffer) => {
      startupLog = (startupLog + chunk.toString()).slice(-2500);
    };
    this.backend.stdout!.on("data", record);
    this.backend.stderr!.on("data", record);
    const deadline = Date.now() + 15_000;
    while (Date.now() < deadline && !failed && this.backend.exitCode === null) {
      const ready = await fetch(`http://127.0.0.1:${this.backendPort}/api/ping`).then(
        async (r) => {
          await r.body?.cancel();
          return r.ok;
        },
        () => false,
      );
      if (ready) return;
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
    throw new Error("The Console test backend did not become ready. " + startupLog);
  }

  async accountRequest(
    path: string,
    method = "GET",
    body?: unknown,
    account?: TestAccount,
  ): Promise<Response> {
    return fetch(new URL(this.url).origin + "/api/account" + path, {
      method,
      redirect: "error",
      headers: {
        "Content-Type": "application/json",
        Origin: new URL(this.url).origin,
        ...(account ? { Cookie: account.cookie } : {}),
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
  }

  async newAccount(): Promise<TestAccount> {
    const email = `fixture-${randomBytes(8).toString("hex")}@example.test`;
    const response = await this.accountRequest("/sign-up/email", "POST", {
      name: "Directory test",
      email,
      password: randomBytes(32).toString("base64url"),
    });
    const data = (await response.json()) as { user?: { id: string } };
    if (!response.ok || !data?.user?.id) throw new Error("Console fixture account creation failed.");
    return {
      email,
      id: data.user.id,
      cookie: response.headers
        .getSetCookie()
        .map((value) => value.split(";", 1)[0])
        .join("; "),
    };
  }

  prompt(): SetupPrompt {
    return {
      print() {},
      question: async () => {
        throw new Error("Key-based enrollment must not require user input.");
      },
      secret: async () => {
        throw new Error("The CLI must not ask for an account password or token.");
      },
      open: () => {
        throw new Error("Key-based enrollment must not open a browser login.");
      },
    };
  }

  /** Test-only IPC, never exposed by the production HTTP server. */
  async control<T>(action: string, value?: unknown): Promise<T> {
    const child = this.backend;
    if (!child?.connected) throw new Error("Test backend IPC is unavailable.");
    const id = ++this.rpcId;
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        child.off("message", listener);
        reject(new Error("Fixture control timed out."));
      }, 5000);
      const listener = (message: any) => {
        if (message.id !== id) return;
        clearTimeout(timer);
        child.off("message", listener);
        if (message.error) reject(new Error("Fixture control failed: " + action));
        else resolve(message.value as T);
      };
      child.on("message", listener);
      child.send({ id, action, value });
    });
  }

  private async stopBackend(): Promise<void> {
    const child = this.backend;
    this.backend = undefined;
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const stopped = once(child, "exit");
    child.kill("SIGTERM");
    const deadline = setTimeout(() => child.kill("SIGKILL"), 5000);
    try {
      await stopped;
    } finally {
      clearTimeout(deadline);
    }
  }

  async restart(): Promise<void> {
    await this.stopBackend();
    await this.startBackend();
  }
  async close(): Promise<void> {
    this.server?.closeAllConnections();
    await new Promise<void>((resolve) => (this.server ? this.server.close(() => resolve()) : resolve()));
    this.server = undefined;
    await this.stopBackend();
    await this.cleanupDatabase?.();
    this.cleanupDatabase = undefined;
    if (this.directory) await rm(this.directory, { recursive: true, force: true });
  }
}
