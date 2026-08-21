#!/usr/bin/env node
// Cross-compiles the `mindwired` daemon for every supported platform and packages each
// binary as its own npm package (mindwire-daemon-<platform>-<arch>). The main `mindwire`
// package lists these as optionalDependencies with `os`/`cpu` filters, so `npm i mindwire`
// downloads only the one binary matching the host — the esbuild/swc distribution pattern.
//
// Output: packages/sdk/npm/<pkg>/ (gitignored build artifacts) + a host-matching binary at
// packages/sdk/bin/mindwired-<platform>-<arch> (gitignored) so this repo's examples find a daemon
// with no `bin` path — the same discovery a published consumer gets from the optional dependency.
//
// A plain `bun run build:daemon` is a DEV build: it does NOT touch package.json. A publish build
// (MINDWIRE_RELEASE=1) additionally rewrites `optionalDependencies` in packages/sdk/package.json in
// lockstep with the built targets.
//
// Release order: MINDWIRE_RELEASE=1 build → publish every packages/sdk/npm/* package FIRST, then
// publish `mindwire` (its optionalDependencies must already exist on the registry at the same version).

import { execFileSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const sdkDir = resolve(here, "..");
const daemonDir = resolve(sdkDir, "..", "..", "daemon");
const outRoot = join(sdkDir, "npm");

// node process.platform / process.arch  ↔  Go GOOS / GOARCH
const TARGETS = [
  { platform: "darwin", arch: "arm64", goos: "darwin", goarch: "arm64" },
  { platform: "darwin", arch: "x64", goos: "darwin", goarch: "amd64" },
  { platform: "linux", arch: "x64", goos: "linux", goarch: "amd64" },
  { platform: "linux", arch: "arm64", goos: "linux", goarch: "arm64" },
  { platform: "win32", arch: "x64", goos: "windows", goarch: "amd64" },
];

const pkgJsonPath = join(sdkDir, "package.json");
const pkg = JSON.parse(readFileSync(pkgJsonPath, "utf8"));
const version = pkg.version;

console.log(`building mindwired ${version} for ${TARGETS.length} targets\n`);

rmSync(outRoot, { recursive: true, force: true });
mkdirSync(outRoot, { recursive: true });

const optionalDependencies = {};

for (const t of TARGETS) {
  const name = `mindwire-daemon-${t.platform}-${t.arch}`;
  const binName = t.platform === "win32" ? "mindwired.exe" : "mindwired";
  const dir = join(outRoot, name);
  mkdirSync(dir, { recursive: true });
  const outBin = join(dir, binName);

  execFileSync("go", ["build", "-trimpath", "-ldflags=-s -w", "-o", outBin, "./cmd/daemon"], {
    cwd: daemonDir,
    env: { ...process.env, CGO_ENABLED: "0", GOOS: t.goos, GOARCH: t.goarch },
    stdio: ["ignore", "inherit", "inherit"],
  });

  writeFileSync(
    join(dir, "package.json"),
    JSON.stringify(
      {
        name,
        version,
        description: `Prebuilt mindwired daemon (${t.platform}-${t.arch}) for the mindwire SDK's embedded mode.`,
        license: "Apache-2.0",
        repository: { type: "git", url: "https://github.com/oblien/mindwire.git" },
        os: [t.platform],
        cpu: [t.arch],
        files: [binName],
        preferUnplugged: true,
      },
      null,
      2,
    ) + "\n",
  );

  writeFileSync(
    join(dir, "README.md"),
    `# ${name}\n\nThe prebuilt \`mindwired\` daemon for **${t.platform}-${t.arch}**, installed automatically as an optional dependency of [\`mindwire\`](https://www.npmjs.com/package/mindwire) so its embedded mode works with no extra setup. You don't install this directly.\n`,
  );

  const kb = (statSync(outBin).size / 1024).toFixed(0);
  console.log(`  ✓ ${name.padEnd(32)} ${binName} (${kb} KB)`);
  optionalDependencies[name] = version;
}

// Local-dev convenience: drop the host-matching binary next to the SDK at
// bin/mindwired-<platform>-<arch>, which is exactly where embedded.ts's resolveBinary() looks after
// the optional-dependency lookup. This is what makes the examples' `new Mindwire({ agent })` find a
// daemon in this repo — no `bin` path, no published optional dependency. (packages/sdk/bin/ is
// gitignored: a pure build artifact, and not in the SDK's published `files`.)
const host = TARGETS.find((t) => t.platform === process.platform && t.arch === process.arch);
if (host) {
  const hostBinName = host.platform === "win32" ? "mindwired.exe" : "mindwired";
  const src = join(outRoot, `mindwire-daemon-${host.platform}-${host.arch}`, hostBinName);
  const binDir = join(sdkDir, "bin");
  mkdirSync(binDir, { recursive: true });
  const dest = join(binDir, `mindwired-${host.platform}-${host.arch}${host.platform === "win32" ? ".exe" : ""}`);
  copyFileSync(src, dest);
  console.log(`\nlinked host binary for local dev → ${dest}`);
} else {
  console.log(`\n(no target matches this host ${process.platform}-${process.arch}; skipped local bin link)`);
}

// optionalDependencies are a PUBLISH concern: they must be present in the published package.json, but
// must NOT be committed on unpublished (404) versions — otherwise `bun install --frozen-lockfile`
// breaks on fresh clones and in CI (the deps can't resolve, so the lockfile drifts). So only write
// them for an explicit release build; everyday `bun run build:daemon` leaves package.json untouched.
if (process.env["MINDWIRE_RELEASE"]) {
  pkg.optionalDependencies = optionalDependencies;
  writeFileSync(pkgJsonPath, JSON.stringify(pkg, null, 2) + "\n");
  console.log(`\nwrote optionalDependencies to package.json (release build):`);
  for (const [k, v] of Object.entries(optionalDependencies)) console.log(`  ${k}@${v}`);
  console.log(`\ndone. Publish npm/* packages before publishing mindwire@${version}.`);
} else {
  console.log(`\n(dev build — optionalDependencies not written; set MINDWIRE_RELEASE=1 for a publish build)`);
}
