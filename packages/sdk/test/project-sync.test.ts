import { test, expect } from "bun:test";
import { createHash } from "node:crypto";
import { Mindwire, remote, ProjectSyncError, type ProjectSyncCheckpoint, type ProjectSyncOperation } from "../src/index.js";

const sha = (data: Uint8Array) => createHash("sha256").update(data).digest("hex");
const checkpointId = "a".repeat(64);
const stamp = "2026-09-26T00:00:00Z";

class SyncWorkspace {
  objects = new Map<string, string>();
  checkpoints = new Map<string, ProjectSyncCheckpoint>();
  operations = new Map<string, ProjectSyncOperation>();
  calls: string[] = [];
  uploads: string[] = [];
  importCommits = 0;
  dropChunk = false;
  dropImport = false;
  offline = false;
  status: ProjectSyncOperation["status"] = "succeeded";
  protocol = 1;
  readonly client: Mindwire;
  constructor(name: string) {
    this.client = new Mindwire({ target: remote(`http://${name}.invalid`, { token: "sync-fixture" }), fetch: async (url, init) => {
      if (this.offline) throw new TypeError("offline");
      const path = new URL(url).pathname;
      this.calls.push(path);
      expect(new Headers(init?.headers).get("authorization")).toBe("Bearer sync-fixture");
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      const id = path.split("/").at(-1)!;
      const response = (v: unknown) => v === undefined ? Response.json({ error: "missing" }, { status: 404 }) : Response.json(v);
      if (path === "/healthz") return response({ ok: true, projectSyncVersion: this.protocol });
      if (path.endsWith("/exports")) {
        const o: ProjectSyncOperation = { ...body, kind: "export", status: "succeeded", phase: "complete", progress: 1,
          resultCheckpointId: checkpointId, createdAt: stamp, updatedAt: stamp };
        this.operations.set(body.id, o); return response(o);
      }
      if (path.endsWith("/imports")) {
        if (!this.operations.has(body.id)) {
          this.operations.set(body.id, { ...body, kind: "import", status: this.status, phase: "complete", progress: 1,
            resultProjectId: "replica", resultCheckpointId: checkpointId, createdAt: stamp, updatedAt: stamp,
            conflicts: this.status === "conflicts" ? [{ path: "files/app.ts", kind: "file", message: "Independent changes preserved" }] : undefined });
          this.importCommits++;
        }
        if (this.dropImport) { this.dropImport = false; throw new TypeError("lost final acknowledgement"); }
        return response(this.operations.get(body.id));
      }
      if (path.includes("/operations/")) return response(this.operations.get(id));
      if (path.includes("/checkpoints/")) {
        if (path.endsWith("/objects")) return response({ objects: [...this.objects.keys()] });
        if (init?.method === "PUT") { this.checkpoints.set(id, body); return response(body); }
        return response(this.checkpoints.get(id));
      }
      if (path.endsWith("/objects/missing")) return response({ objects: body.objects.filter((id: string) => !this.objects.has(id)) });
      if (path.includes("/objects/")) {
        if (init?.method === "PUT") {
          expect(sha(Buffer.from(body.data, "base64"))).toBe(id);
          this.uploads.push(id); this.objects.set(id, body.data);
          if (this.dropChunk) { this.dropChunk = false; throw new TypeError("lost chunk acknowledgement"); }
          return response({ ok: true });
        }
        return response({ data: this.objects.get(id) });
      }
      throw new Error("Unexpected route: " + path);
    } });
  }
  seed() {
    const chunks = [Buffer.from("native transcript and project memory"), Buffer.from("working files")];
    for (const chunk of chunks) this.objects.set(sha(chunk), chunk.toString("base64"));
    this.checkpoints.set(checkpointId, { id: checkpointId, projectId: "logical", parents: [],
      manifest: { kind: "file", mode: 384, size: chunks[0]!.length, chunks: [sha(chunks[0]!)] }, objectCount: 2,
      bytes: chunks.reduce((n, c) => n + c.length, 0) });
  }
}

test("resuming a partial checkpoint uploads only missing chunks", async () => {
  const x = new SyncWorkspace("x"), y = new SyncWorkspace("y"); x.seed(); y.dropChunk = true;
  const request = { id: "switch", projectId: "source", path: "/work/project" };
  await expect(x.client.workspace.sync.switchTo(y.client.workspace.sync, request)).rejects.toThrow();
  expect(y.objects.size).toBe(1);
  const result = await x.client.workspace.sync.switchTo(y.client.workspace.sync, request);
  expect(result.status).toBe("succeeded");
  expect(y.uploads.length).toBe(2);
  expect(new Set(y.uploads).size).toBe(2);
  expect(y.importCommits).toBe(1);
  expect(x.objects.size).toBe(2);
  expect(x.calls.some(p => p.endsWith("/imports"))).toBe(false);
});

test("destination acknowledgement can be recovered with the source offline", async () => {
  const x = new SyncWorkspace("x"), y = new SyncWorkspace("y"); x.seed(); y.dropImport = true;
  const request = { id: "resume", projectId: "source", path: "/work/project" };
  await expect(x.client.workspace.sync.switchTo(y.client.workspace.sync, request)).rejects.toThrow();
  expect(y.importCommits).toBe(1);
  x.offline = true;
  const result = await x.client.workspace.sync.switchTo(y.client.workspace.sync, request);
  expect(result.status).toBe("succeeded");
  expect(y.importCommits).toBe(1);
  expect(y.uploads.length).toBe(2);
});

test("duplicate starts share a flight, but a different destination cannot reuse it", async () => {
  const x = new SyncWorkspace("x"), y = new SyncWorkspace("y"), z = new SyncWorkspace("z"); x.seed();
  const request = { id: "shared", projectId: "source", path: "/work/project" };
  const first = x.client.workspace.sync.switchTo(y.client.workspace.sync, request);
  const second = x.client.workspace.sync.switchTo(y.client.workspace.sync, request);
  expect(first).toBe(second);
  await expect(x.client.workspace.sync.switchTo(z.client.workspace.sync, request)).rejects.toThrow("different request or destination");
  expect((await first).status).toBe("succeeded");
  expect(x.calls.filter(p => p.endsWith("/exports")).length).toBe(1);
  expect(y.importCommits).toBe(1);
  expect(z.calls).toEqual([]);
});

test("conflicts retain their full operation and old daemons fail before export", async () => {
  const x = new SyncWorkspace("x"), y = new SyncWorkspace("y"); x.seed(); y.status = "conflicts";
  try { await x.client.workspace.sync.switchTo(y.client.workspace.sync, { id: "conflict", projectId: "source" }); throw new Error("accepted conflict"); }
  catch (e) { expect(e).toBeInstanceOf(ProjectSyncError); expect((e as ProjectSyncError).operation.conflicts?.[0]?.path).toBe("files/app.ts"); }
  const old = new SyncWorkspace("old"); old.protocol = 0;
  const count = x.calls.filter(p => p.endsWith("/exports")).length;
  await expect(x.client.workspace.sync.switchTo(old.client.workspace.sync, { id: "old", projectId: "source" })).rejects.toThrow("Update Mindwire");
  expect(x.calls.filter(p => p.endsWith("/exports")).length).toBe(count);
});
