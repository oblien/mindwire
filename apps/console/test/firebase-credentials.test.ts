import { afterEach, beforeEach, expect, test } from "bun:test";
import { generateKeyPairSync } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { deleteApp, getApps } from "firebase-admin/app";
import { FirebasePush } from "../server/notifications/firebase";

const { privateKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
const account = JSON.stringify({
  type: "service_account", project_id: "mindwire-push-fixture",
  client_email: "push@mindwire-push-fixture.iam.gserviceaccount.com",
  private_key: privateKey.export({ type: "pkcs8", format: "pem" }),
});
const originalJSON = process.env.FCM_SERVICE_ACCOUNT_JSON;
const originalPath = process.env.FCM_SERVICE_ACCOUNT_PATH;
let directory: string;
beforeEach(async () => {
  delete process.env.FCM_SERVICE_ACCOUNT_JSON;
  delete process.env.FCM_SERVICE_ACCOUNT_PATH;
  directory = await mkdtemp(join(tmpdir(), "mindwire-firebase-"));
});
afterEach(async () => {
  await Promise.all(getApps().filter(app => app.name === "mindwire-push").map(deleteApp));
  await rm(directory, { recursive: true, force: true });
  if (originalJSON === undefined) delete process.env.FCM_SERVICE_ACCOUNT_JSON;
  else process.env.FCM_SERVICE_ACCOUNT_JSON = originalJSON;
  if (originalPath === undefined) delete process.env.FCM_SERVICE_ACCOUNT_PATH;
  else process.env.FCM_SERVICE_ACCOUNT_PATH = originalPath;
});

// An empty recipient list initializes the real SDK without contacting Google.
const initialize = () => new FirebasePush().send([], { data: {} });
test("initializes Firebase from a JSON secret without a credential file", async () => {
  process.env.FCM_SERVICE_ACCOUNT_JSON = account;
  process.env.FCM_SERVICE_ACCOUNT_PATH = join(directory, "does-not-exist.json");
  expect(await initialize()).toEqual({ delivered: 0, failed: 0, invalidTokens: [], errors: [] });
  expect(getApps().find(app => app.name === "mindwire-push")).toBeDefined();
});

test("retains support for a mounted service-account file", async () => {
  const path = join(directory, "firebase.json");
  await writeFile(path, account, { mode: 0o600 });
  process.env.FCM_SERVICE_ACCOUNT_PATH = path;
  await initialize();
  expect(getApps().find(app => app.name === "mindwire-push")).toBeDefined();
});

test("invalid JSON cannot leak its content or silently fall back to a valid file", async () => {
  const path = join(directory, "firebase.json");
  await writeFile(path, account, { mode: 0o600 });
  process.env.FCM_SERVICE_ACCOUNT_PATH = path;
  process.env.FCM_SERVICE_ACCOUNT_JSON = "private-credential-marker-invalid-json";
  await expect(initialize()).rejects.toMatchObject({ status: 503, code: "push_credentials_invalid", message: "push_credentials_invalid" });
  expect(getApps().find(app => app.name === "mindwire-push")).toBeUndefined();
});

test("missing credentials leave push delivery unavailable", async () => {
  await expect(initialize()).rejects.toMatchObject({ status: 503, code: "push_not_configured" });
});
