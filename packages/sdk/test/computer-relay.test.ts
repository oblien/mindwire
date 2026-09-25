import { expect, test } from "bun:test";
import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const exists = async (path: string) => access(path).then(() => true, () => false);

test.skipIf(process.platform === "win32")("a reserved relay hostname is not ready until the provider connects", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mindwire-relay-ready-"));
  let child: ReturnType<typeof Bun.spawn> | undefined;
  try {
    // A local provider fixture separates hostname allocation from connectivity.
    // It also emits enough startup diagnostics to fill the bounded log buffer.
    await writeFile(join(directory, "cloudflared"), `#!/bin/sh
if [ "$1" = "--version" ]; then exit 0; fi
printf 'https://reserved-fixture.trycloudflare.com\\n'
touch "$MINDWIRE_RELAY_FIXTURE/allocated"
while [ ! -f "$MINDWIRE_RELAY_FIXTURE/connect" ]; do sleep 0.05; done
dd if=/dev/zero bs=32768 count=1 2>/dev/null | tr '\\000' 'x'
printf '\\nRegistered tunnel connection\\n'
while [ ! -f "$MINDWIRE_RELAY_FIXTURE/stop" ]; do sleep 0.05; done
`, { mode: 0o700 });
    const source = new URL("../src/computer/relay.ts", import.meta.url).href;
    child = Bun.spawn([process.execPath, "--eval", `
      import { startRelay } from ${JSON.stringify(source)};
      const relay = await startRelay({ kind: "cloudflare" }, 12345);
      await Bun.write(process.env.MINDWIRE_RELAY_FIXTURE + "/ready", relay.route.url);
      relay.close();
    `], { env: { ...process.env, PATH: directory + delimiter + (process.env.PATH ?? ""), MINDWIRE_RELAY_FIXTURE: directory },
      stdout: "pipe", stderr: "pipe" });
    for (let attempt = 0; attempt < 100 && !await exists(join(directory, "allocated")); attempt++) await delay(25);
    expect(await exists(join(directory, "allocated"))).toBe(true);
    await delay(200);
    expect(await exists(join(directory, "ready"))).toBe(false);
    await writeFile(join(directory, "connect"), "");
    expect(await child.exited).toBe(0);
    expect(await readFile(join(directory, "ready"), "utf8")).toBe("wss://reserved-fixture.trycloudflare.com/ssh");
  } finally {
    // Let the fixture exit even if its parent fails before closing the relay.
    await writeFile(join(directory, "connect"), "");
    await writeFile(join(directory, "stop"), "");
    if (child) {
      const exited = await Promise.race([child.exited.then(() => true), delay(1500, false)]);
      if (!exited) child.kill();
    }
    await rm(directory, { recursive: true, force: true });
  }
}, 10_000);
