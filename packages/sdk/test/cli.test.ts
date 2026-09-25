import { expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const cli = resolve(import.meta.dir, "../dist/cli.js");

test.skipIf(process.platform === "win32")("npm's command symlink opens the CLI help instead of exiting silently", () => {
  const directory = mkdtempSync(join(tmpdir(), "mindwire npm bin "));
  try {
    const command = join(directory, "mindwire");
    symlinkSync(cli, command);
    for (const args of [[], ["--help"]]) {
      const result = spawnSync("node", [command, ...args], { encoding: "utf8", timeout: 5000 });
      expect(result.error).toBeUndefined();
      expect(result.status, result.stderr).toBe(0);
      expect(result.stderr).toBe("");
      expect(result.stdout).toContain("connect this computer to your phone");
      expect(result.stdout).toContain("mindwire connect");
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("the direct CLI entry still prints help", () => {
  const result = spawnSync("node", [cli, "--help"], { encoding: "utf8", timeout: 5000 });
  expect(result.error).toBeUndefined();
  expect(result.status).toBe(0);
  expect(result.stderr).toBe("");
  expect(result.stdout).toContain("connect this computer to your phone");
  expect(result.stdout).toContain("--qr auto|terminal|browser");
});

test("invalid QR display mode fails before starting a service", () => {
  const result = spawnSync("node", [cli, "connect", "--qr", "invalid"], { encoding: "utf8", timeout: 5000 });
  expect(result.status).toBe(1);
  expect(result.stderr).toContain("Choose --qr auto, terminal or browser.");
  expect(result.stdout).toBe("");
});

test("importing the CLI from eval with a non-file argument has no command side effects", () => {
  const source = `const { main } = await import(${JSON.stringify(pathToFileURL(cli).href)}); console.log(typeof main);`;
  const result = spawnSync("node", ["--input-type=module", "-e", source, "--", "not-a-script"], {
    encoding: "utf8", timeout: 5000,
  });
  expect(result.error).toBeUndefined();
  expect(result.status).toBe(0);
  expect(result.stderr).toBe("");
  expect(result.stdout).toBe("function\n");
});
