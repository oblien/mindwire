import { expect, test } from "bun:test";
import { Mindwire, remote, type LocalDesktopInfo, type LocalDesktopSettings } from "../src/index.js";
import { manageDesktop, desktopSummary } from "../src/computer/desktop.js";
import type { SetupPrompt } from "../src/computer/setup-prompt.js";

function fixture(choices: (string | undefined)[], initiallyEnabled = false) {
  let info: LocalDesktopInfo = { supported: true, enabled: initiallyEnabled, screenSharing: false, username: "owner" };
  const writes: LocalDesktopSettings[] = [], output: string[] = [];
  let opened = 0, passwords = 0, forbiddenQuestion = false;
  let rejected: { code: string; status: number }[] = [];
  let secrets = [" test Ω password "];
  const client = new Mindwire({ target: remote("http://127.0.0.1:1234", { token: "daemon-token" }), fetch: async (url, init) => {
    expect(new URL(url).pathname).toBe("/surfaces/desktop/local");
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer daemon-token");
    if (init?.method === "PUT") {
      expect(new Headers(init.headers).get("X-Mindwire-Local-Control")).toBe("a".repeat(64));
      const settings = JSON.parse(String(init.body)) as LocalDesktopSettings;
      writes.push(settings);
      const rejection = rejected.shift();
      if (rejection) return Response.json({ error: "Private authentication details must not be printed", code: rejection.code }, { status: rejection.status });
      info = { ...info, enabled: settings.enabled };
      return Response.json({ enabled: info.enabled });
    }
    return Response.json(info);
  } });
  const prompt: SetupPrompt = {
    print: value => output.push(value), open: () => { throw new Error("Use the fixed Mac settings destination"); },
    question: async () => { if (forbiddenQuestion) throw new Error("Cancelled picker reopened as a question"); return ""; },
    secret: async () => { passwords++; return secrets.shift() ?? " test Ω password "; },
    select: async <T extends string>() => choices.shift() as T | undefined,
  };
  return {
    client, prompt, writes, output,
    openSettings: () => { opened++; info = { ...info, screenSharing: true }; },
    readControlToken: async () => "a".repeat(64),
    setSharing: () => { info = { ...info, screenSharing: true }; },
    forbidQuestions: () => { forbiddenQuestion = true; },
    reject: (code = "desktop_authentication", status = 428) => { rejected.push({ code, status }); },
    passwords: (...values: string[]) => { secrets = values; },
    counts: () => ({ opened, passwords }),
  };
}

test("Mac desktop status never enables screen sharing or asks for a password", async () => {
  const f = fixture([]);
  const info = await manageDesktop({ ...f, directory: "/unused", action: "status", interactive: true });
  expect(info.enabled).toBe(false);
  expect(f.writes).toEqual([]);
  expect(f.counts()).toEqual({ opened: 0, passwords: 0 });
  expect(desktopSummary(info)).toContain("mindwire desktop enable");
});

test("rejected Mac password offers a private retry and saves only after acceptance", async () => {
  const f = fixture(["allow", "retry"]); f.setSharing(); f.reject();
  f.passwords("incorrect private password", "correct private password");
  const result = await manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: true });
  expect(result.enabled).toBe(true);
  expect(f.counts().passwords).toBe(2);
  expect(f.writes.map(write => write.password)).toEqual(["incorrect private password", "correct private password"]);
  const output = f.output.join("\n");
  expect(output).toContain("Check the password for owner and try again");
  expect(output).toContain("Desktop settings were not changed");
  expect(output).not.toMatch(/127\.0\.0\.1|PUT |428|private password|Private authentication details/);
});

test("cancelling a rejected password leaves an existing desktop setup enabled", async () => {
  for (const choice of ["cancel", undefined]) {
    const f = fixture(["allow", choice], true); f.setSharing(); f.reject(); f.forbidQuestions();
    const result = await manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: true });
    expect(result.enabled).toBe(true);
    expect(f.writes).toHaveLength(1);
    expect(f.counts().passwords).toBe(1);
    expect(f.output.join("\n")).not.toContain("Desktop access is on.");
  }
});

test("password retry can open Mac settings and rejects invalid passwords locally", async () => {
  const f = fixture(["allow", "retry", "settings"]); f.setSharing(); f.reject();
  f.passwords("", "incorrect private password", " test Ω password ");
  const result = await manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: true });
  expect(result.enabled).toBe(true);
  expect(f.counts()).toEqual({ opened: 1, passwords: 3 });
  expect(f.writes).toHaveLength(2);
});

test("connection and permission failures are not presented as an incorrect password", async () => {
  for (const [code, status] of [["timeout", 504], ["forbidden", 403], ["screen_sharing_off", 428]] as const) {
    const f = fixture(["allow", "retry"]); f.setSharing(); f.reject(code, status);
    await expect(manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: true })).rejects.toThrow();
    expect(f.counts().passwords).toBe(1);
    expect(f.writes).toHaveLength(1);
    expect(f.output.join("\n")).not.toContain("didn’t accept");
  }
});

test("CLI opt-in guides Mac settings then saves credentials locally once", async () => {
  const f = fixture(["allow", "open"]);
  const result = await manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: true });
  expect(result.enabled).toBe(true);
  expect(f.counts()).toEqual({ opened: 1, passwords: 1 });
  expect(f.writes).toEqual([{ enabled: true, username: "owner", password: " test Ω password " }]);
  expect(f.output.join("\n")).not.toContain(" test Ω password ");
  expect(f.output.join("\n")).toContain("Allow only owner");
});

test("cancelling local approval or Mac setup preserves the existing settings", async () => {
  for (const choices of [["cancel"], [undefined], ["allow", "cancel"]]) {
    const f = fixture(choices); f.forbidQuestions();
    const result = await manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: true });
    expect(result.enabled).toBe(false); expect(f.writes).toEqual([]);
    expect(f.counts().passwords).toBe(0);
  }
});

test("desktop disable is an explicit local operation and never asks for the Mac password", async () => {
  const f = fixture([], true);
  const result = await manageDesktop({ ...f, directory: "/unused", action: "disable", interactive: false, json: true });
  expect(result.enabled).toBe(false); expect(f.writes).toEqual([{ enabled: false }]);
  expect(f.counts().passwords).toBe(0); expect(f.output).toEqual([]);
});

test("unattended setup and a missing local owner credential cannot enable desktop", async () => {
  const f = fixture(["allow"]); f.setSharing();
  await expect(manageDesktop({ ...f, directory: "/unused", action: "enable", interactive: false })).rejects.toThrow("interactive terminal");
  expect(() => f.client.surfaces.configureLocal({ enabled: true, password: "secret" }, "")).toThrow("mindwire desktop");
  expect(f.writes).toEqual([]);
});

test("desktop viewer grants preserve the shared surface session without another connection API", async () => {
  const request = { id: "viewer", deviceId: "phone", port: 8794, desktopSessionId: "surface-session" };
  const client = new Mindwire({ target: remote("http://fixture"), fetch: async (url, init) => {
    expect(new URL(url).pathname).toBe("/computer/forwards");
    expect(JSON.parse(String(init?.body))).toEqual(request);
    return Response.json({ ...request, expiresAt: "later" });
  } });
  expect((await client.computer.forward(request)).desktopSessionId).toBe("surface-session");
});
