#!/usr/bin/env bun
/**
 * Cut a new MindWire release.
 *
 * Usage:
 *   bun run release patch          # 0.4.0      → 0.4.1
 *   bun run release minor          # 0.4.0      → 0.5.0
 *   bun run release major          # 0.4.0      → 1.0.0
 *   bun run release rc             # 0.4.0      → 0.4.1-rc.1
 *                                  # 0.4.1-rc.1 → 0.4.1-rc.2
 *                                  # 0.4.1-rc.2 → 0.4.1     (promote: rc → stable)
 *   bun run release <explicit>     # set to literal "0.5.0-beta.3"
 *   bun run release --dry-run patch
 *
 *   bun run release                # NO args in a terminal → interactive wizard over every option.
 *                                  # It prints the equivalent command and re-runs itself with it, so
 *                                  # there is one code path. Non-TTY (CI/pipes) releases the current
 *                                  # version as-is.
 *
 *   bun run release docker [tag]   # publish the console image ONLY (GHCR) via the docker-images
 *                                  # workflow. A test/prerelease image build: NO version bump, NO git
 *                                  # tag, NO GitHub release, NO npm, and it never moves :latest. Omit
 *                                  # [tag] → the image is tagged with the short SHA. `--ref=<branch>`
 *                                  # builds that branch.
 *
 * What it does:
 *   1. Refuse if the working tree is dirty
 *   2. Refuse if not on main (override with --force-branch)
 *   3. Refuse if HEAD is behind origin/main (you'd push an old commit)
 *   4. Compute the next version from packages/sdk/package.json (the ONE published version)
 *   5. Write it into packages/sdk/package.json and daemon/internal/agent/agent.go
 *   6. Commit "Bump to vX.Y.Z"
 *   7. Push the bump commit
 *   8. Tag vX.Y.Z and push the tag
 *   9. Stream the live build status here
 *
 * The tag push triggers .github/workflows/release.yml, which runs the gate, builds the daemon binaries
 * + GitHub release, and publishes `mindwire` to npm. Tags
 * containing `-` (rc.N, beta.N) become prereleases automatically (published to the npm `next` tag).
 *
 * Version model: one version drives the SDK, daemon health response, GitHub Release tag, and npm package.
 */

import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { buildWizardArgs, type WizardAnswers } from "./release-args";

const ROOT = dirname(dirname(fileURLToPath(import.meta.url)));
// The ONE version that ships. Root package.json and the apps are private 0.1.0 and never published, so
// they are deliberately NOT synced — only this file drives npm and the git tag.
const SDK_PKG = join(ROOT, "packages/sdk/package.json");
// The daemon health version is a Go const and is always synchronized to the SDK release version.
const AGENT_GO = join(ROOT, "daemon/internal/agent/agent.go");

/* ─── CLI parsing ──────────────────────────────────────────────────── */

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const forceBranch = args.includes("--force-branch");
// `--force` re-releases a tag that ALREADY exists: instead of refusing, delete it (local + origin) and
// re-push, firing a fresh release run. Off by default and explicit on purpose — re-pointing a tag people
// may already have pulled silently diverges their bytes. The safe use is salvaging a tag whose release
// failed and published nothing (see the refusal block below).
const force = args.includes("--force");
if (args.includes("--help") || args.includes("-h")) usageAndExit(0);

// The bump is the first positional argument.
const cmd = args.find((a) => !a.startsWith("--"));

/* ─── Interactive mode ─────────────────────────────────────────────────
 *
 * `bun run release` with NO arguments, in a terminal, walks the same option surface as the flags. It
 * builds an argv and RE-EXECUTES this script with it — so the release logic below has exactly one entry
 * shape and can't drift from the flag path. It also prints the equivalent command, so the wizard teaches
 * the flags instead of hiding them. Non-TTY (CI, pipes) releases the current version as-is.
 */
if (args.length === 0 && process.stdin.isTTY && process.stdout.isTTY) {
  const chosen = await runWizard();
  if (!chosen) {
    log("Cancelled — nothing was released.");
    process.exit(0);
  }
  log("");
  log(`▶ bun run release ${chosen.join(" ")}`);
  log("");
  const child = spawnSync(process.execPath, [fileURLToPath(import.meta.url), ...chosen], {
    stdio: "inherit",
  });
  process.exit(child.status ?? 1);
}

// `docker` is a SEPARATE path: publish the console image ONLY (via the docker-images workflow) — no
// version bump, no git tag, no GitHub release. Branch out here, before any semver handling treats
// "docker" as a bump.
if (cmd === "docker") {
  releaseDocker();
  process.exit(0);
}

type BumpKind = "patch" | "minor" | "major" | "rc" | "current" | "literal";
// No arg (or "current") → release the version already in package.json as-is, no bump. Otherwise bump.
const bump: { kind: BumpKind; literal?: string } =
  !cmd || cmd === "current"
    ? { kind: "current" }
    : cmd === "patch" || cmd === "minor" || cmd === "major" || cmd === "rc"
      ? { kind: cmd }
      : { kind: "literal", literal: cmd };

if (bump.kind === "literal" && !/^\d+\.\d+\.\d+(-[a-z0-9]+(\.\d+)?)?$/i.test(bump.literal!)) {
  console.error(`Refusing literal version "${bump.literal}" — not a semver string.`);
  process.exit(1);
}

/* ─── Pre-flight checks ────────────────────────────────────────────── */

if (!dryRun) {
  preflight();
}

/* ─── Version compute ──────────────────────────────────────────────── */

const current = readVersion(SDK_PKG);
const next = computeNext(current, bump);
const tag = `v${next}`;
const daemonVersion = readDaemonVersion();

log(`Current version (SDK):  ${current}`);
log(`Next version:           ${next}`);
log(`Tag:                    ${tag}`);
log(`Prerelease:             ${tag.includes("-") ? "yes (npm dist-tag: next)" : "no"}`);
// Informational, not a warning: the daemon binary version is decoupled from the SDK BY DESIGN, so they
// routinely differ. Surfaced only so a daemon change doesn't ship with a stale binary version.
log(`Daemon binary version:  ${daemonVersion}${daemonVersion === next ? "" : dim(` → ${next} (will sync)`)}`);
log(``);

if (dryRun) {
  if (bump.kind === "current") {
    log(`[dry-run] would tag the current version (no bump) and push ${tag}`);
  } else {
    log(`[dry-run] would update packages/sdk/package.json → ${next}`);
    log(`[dry-run] would update daemon/internal/agent/agent.go → ${next}`);
    log(`[dry-run] would commit + push, then tag + push ${tag}`);
  }
  if (force && tagExists(tag)) {
    log(`[dry-run] --force: tag ${tag} exists → would DELETE it (local + origin) and re-push`);
  }
  log(`[dry-run] no files written, no git ops executed.`);
  process.exit(0);
}

if (tagExists(tag)) {
  if (!force) {
    console.error(
      `Refusing: tag ${tag} already exists. ` +
        (bump.kind === "current"
          ? `Bump the version (patch/minor/major), or pass --force to re-release ${tag}.`
          : `Pick a different version, or pass --force to re-release ${tag}.`),
    );
    process.exit(1);
  }
  // --force: salvage an existing tag. Delete it locally AND on origin so the `git tag` + push below
  // create a FRESH ref — a same-SHA re-push reports "up-to-date" and fires no workflow, which is exactly
  // why a plain re-run of an existing tag does nothing.
  log(`⚠ --force: tag ${tag} exists — deleting it (local + origin) to re-release`);
  retireExistingTag(tag);
}

/* ─── Apply ─────────────────────────────────────────────────────────── */

if (bump.kind !== "current" || daemonVersion !== next) {
  writeVersion(SDK_PKG, next);
  writeDaemonVersion(next);
  log(`✓ updated packages/sdk/package.json → ${next}`);
  log(`✓ updated daemon/internal/agent/agent.go → ${next}`);
  git("add", SDK_PKG, AGENT_GO);
  // Only commit if something actually changed. Re-releasing the version you're already on writes no diff,
  // and `git commit` would abort with "nothing to commit" and kill the release — ship as-is instead.
  const nothingStaged =
    spawnSync("git", ["diff", "--cached", "--quiet"], { cwd: ROOT }).status === 0;
  if (nothingStaged) {
    log(`No changes to commit — releasing as-is.`);
  } else {
    git("commit", "-m", `Bump to ${tag}`);
    log(`✓ committed`);
  }
}

git("push", "origin", `refs/heads/${currentBranch()}`);
log(`✓ pushed ${currentBranch()}${bump.kind === "current" ? " (releasing current version, no bump)" : ""}`);

git("tag", tag);
// Fully-qualified refspec: a BRANCH sharing the tag's name would otherwise make `git push origin vX`
// ambiguous ("src refspec … matches more than one").
git("push", "origin", `refs/tags/${tag}`);
log(`✓ pushed tag ${tag} — CI is running the gate, building the daemon binaries, and publishing to npm`);

/* ─── Final report ─────────────────────────────────────────────────── */

const remoteUrl = git("remote", "get-url", "origin", { capture: true }).trim();
const ghMatch = remoteUrl.match(/github\.com[:/]([^/]+)\/([^/.]+)/);
const actionsUrl = ghMatch ? `https://github.com/${ghMatch[1]}/${ghMatch[2]}/actions` : "";
if (ghMatch) {
  const [, owner, repo] = ghMatch;
  log(``);
  log(`Release will appear at:`);
  log(`  https://github.com/${owner}/${repo}/releases/tag/${tag}`);
  log(``);
}

// Stream the live build status right here in the terminal.
watchCi(tag, actionsUrl);

/* ─── Helpers ──────────────────────────────────────────────────────── */

function usageAndExit(code = 1): never {
  const out = code === 0 ? console.log : console.error;
  out(
    [
      "Usage: bun run release [current|patch|minor|major|rc|x.y.z[-rc.N]] [--dry-run] [--force-branch] [--force]",
      "",
      "  (no arg)       in a terminal: interactive wizard over all of the below.",
      "                 non-interactive (CI/pipe): release the current version as-is",
      "  current        same as no arg",
      "  patch          0.4.0      → 0.4.1",
      "  minor          0.4.0      → 0.5.0",
      "  major          0.4.0      → 1.0.0",
      "  rc             0.4.0      → 0.4.1-rc.1",
      "                 0.4.1-rc.1 → 0.4.1-rc.2",
      "                 0.4.1-rc.2 → 0.4.1   (rc → stable promotion)",
      "  <literal>      explicit semver string",
      "",
      "  docker [tag]   publish the console image ONLY (GHCR) via the docker-images workflow — no",
      "                 version bump / git tag / GitHub release / npm, and never moves :latest. Omit",
      "                 [tag] → the image is tagged with the short SHA. `--ref=<branch>` builds that",
      "                 branch (default: current). e.g.  bun run release docker 0.4.1-rc.1",
      "",
      "  --dry-run      print the plan, don't touch anything",
      "  --force-branch run from a non-main branch (default refuses)",
      "  --force        re-release an EXISTING tag: delete it (local + origin) and re-push, firing a",
      "                 fresh build. For salvaging a tag whose release failed. Refuses without it",
      "                 (re-pointing a pulled tag diverges users' bytes).",
      "  --help, -h     show this help",
      "",
      "The tag push runs .github/workflows/release.yml (gate → daemon binaries + GitHub release → npm).",
      "Live build status streams here after the tag is pushed (needs the `gh` CLI, logged in).",
    ].join("\n"),
  );
  process.exit(code);
}

/** True if the tag already exists locally or on origin. */
function tagExists(t: string): boolean {
  const local = git("tag", "--list", t, { capture: true }).trim();
  if (local) return true;
  const remote = git("ls-remote", "--tags", "origin", t, { capture: true }).trim();
  return remote.length > 0;
}

/** Delete a tag wherever it exists (local + origin) so it can be re-pushed as a FRESH ref. Each side is
 *  guarded on presence — `git()` exits on any non-zero, so deleting a side that isn't there would abort
 *  the release. */
function retireExistingTag(t: string): void {
  if (git("tag", "--list", t, { capture: true }).trim()) {
    git("tag", "-d", t);
    log(`  ✓ deleted local tag ${t}`);
  }
  if (git("ls-remote", "--tags", "origin", t, { capture: true }).trim()) {
    git("push", "origin", `:refs/tags/${t}`);
    log(`  ✓ deleted origin tag ${t}`);
  }
}

/**
 * Stream the live status of the release workflow run into this terminal. Uses the `gh` CLI
 * (`gh run watch`), which shows each job spinning → pass/fail and exits when the run finishes. Degrades
 * gracefully (prints the Actions URL) if `gh` is missing, not authed, or the run isn't found yet.
 */
function watchCi(t: string, fallbackUrl: string): void {
  const have = spawnSync("gh", ["--version"], { encoding: "utf8" });
  if (have.status !== 0) {
    if (fallbackUrl) log(`Watch the build:  ${fallbackUrl}`);
    return;
  }

  log(`Waiting for the release run to register on GitHub…`);
  let runId = "";
  for (let i = 0; i < 15 && !runId; i++) {
    Bun.sleepSync(4000);
    const out = spawnSync(
      "gh",
      ["run", "list", "--workflow", "release.yml", "--limit", "15", "--json", "databaseId,headBranch,event,createdAt"],
      { cwd: ROOT, encoding: "utf8" },
    );
    if (out.status !== 0) continue;
    try {
      const runs = JSON.parse(out.stdout ?? "[]") as Array<{
        databaseId: number;
        headBranch: string;
        event: string;
      }>;
      // Tag-triggered runs show headBranch === the tag name.
      const match = runs.find((r) => r.headBranch === t || r.headBranch === `refs/tags/${t}`);
      if (match) runId = String(match.databaseId);
    } catch {
      // keep polling
    }
  }

  if (!runId) {
    log(`Couldn't locate the run automatically.`);
    if (fallbackUrl) log(`Watch the build:  ${fallbackUrl}`);
    return;
  }

  log(``);
  log(`▼ live build status (Ctrl-C to stop watching — the build keeps running):`);
  log(``);
  // Streams job-by-job status and exits when the run completes.
  spawnSync("gh", ["run", "watch", runId, "--interval", "6"], { cwd: ROOT, stdio: "inherit" });
  log(``);
  spawnSync("gh", ["run", "view", runId], { cwd: ROOT, stdio: "inherit" });
}

/* ─── Docker image release (GHCR-only, via workflow_dispatch) ────────── */

/** owner/repo parsed from origin, or null when origin isn't a GitHub remote. */
function ghOwnerRepo(): { owner: string; repo: string } | null {
  const url = git("remote", "get-url", "origin", { capture: true }).trim();
  const m = url.match(/github\.com[:/]([^/]+)\/([^/.]+)/);
  return m ? { owner: m[1], repo: m[2] } : null;
}

/**
 * `bun run release docker [tag]` — trigger the docker-images workflow to publish the console image to
 * GHCR. This is the DOCKER-ONLY path: it dispatches the workflow (no version bump, no git tag, no GitHub
 * release, no npm) and the dispatch run never moves `:latest`. An untracked/dirty tree is fine — it
 * builds whatever is on the pushed `--ref` branch, not your working copy. Requires the `gh` CLI (or use
 * the Actions UI → "Docker images").
 */
function releaseDocker(): void {
  const positionals = args.filter((a) => !a.startsWith("--"));
  const dockerTag = positionals[1]; // `release docker [tag]`
  const ref = args.find((a) => a.startsWith("--ref="))?.slice("--ref=".length) || currentBranch();

  const runArgs = ["workflow", "run", "docker-images.yml", "--ref", ref];
  if (dockerTag) runArgs.push("-f", `tag=${dockerTag}`);

  log(`Console image publish (GHCR-only)`);
  log(`  workflow: docker-images.yml`);
  log(`  ref:      ${ref}`);
  log(`  tag:      ${dockerTag ?? "(short SHA)"}`);
  log(`  scope:    image ONLY — no git tag / GitHub release / npm; :latest untouched`);
  log(``);

  if (dryRun) {
    log(`[dry-run] gh ${runArgs.join(" ")}`);
    log(`[dry-run] nothing dispatched.`);
    return;
  }

  if (spawnSync("gh", ["--version"], { encoding: "utf8" }).status !== 0) {
    console.error(
      `Refusing: the \`gh\` CLI is required for \`release docker\`. Install it + \`gh auth login\`,\n` +
        `or trigger it in the UI: Actions → "Docker images" → Run workflow.`,
    );
    process.exit(1);
  }

  const res = spawnSync("gh", runArgs, { cwd: ROOT, stdio: "inherit" });
  if (res.status !== 0) {
    console.error(
      `\ngh workflow run failed. Most common cause: docker-images.yml isn't on the repo's\n` +
        `DEFAULT branch yet — workflow_dispatch only works once the workflow file is on it.\n` +
        `Push the workflow to the default branch, then retry.`,
    );
    process.exit(res.status ?? 1);
  }
  log(`✓ dispatched docker-images.yml (${ref})`);

  watchDispatch();

  const or = ghOwnerRepo();
  const shown = dockerTag ?? "<short-sha>";
  log(``);
  log(`Verify when green:`);
  if (or) {
    log(`  docker manifest inspect ghcr.io/${or.owner}/mindwire-console:${shown}   # amd64 + arm64`);
    log(`  docker pull ghcr.io/${or.owner}/mindwire-console:${shown}`);
    log(`  Packages: https://github.com/orgs/${or.owner}/packages?repo_name=${or.repo}`);
  }
}

/** Poll for + stream the just-dispatched docker-images run (newest dispatch run). */
function watchDispatch(): void {
  if (spawnSync("gh", ["--version"], { encoding: "utf8" }).status !== 0) return;
  log(`Waiting for the run to register on GitHub…`);
  let runId = "";
  for (let i = 0; i < 15 && !runId; i++) {
    Bun.sleepSync(4000);
    const out = spawnSync(
      "gh",
      ["run", "list", "--workflow", "docker-images.yml", "--event", "workflow_dispatch",
        "--limit", "1", "--json", "databaseId", "--jq", ".[0].databaseId"],
      { cwd: ROOT, encoding: "utf8" },
    );
    if (out.status === 0) {
      const id = (out.stdout ?? "").trim();
      if (id && id !== "null") runId = id;
    }
  }
  if (!runId) {
    log(`Couldn't locate the run automatically — check: gh run list --workflow docker-images.yml`);
    return;
  }
  log(``);
  log(`▼ live build status (Ctrl-C to stop watching — the build keeps running):`);
  log(``);
  spawnSync("gh", ["run", "watch", runId, "--interval", "6"], { cwd: ROOT, stdio: "inherit" });
}

function preflight(): void {
  // 1. Clean working tree
  const status = git("status", "--porcelain", { capture: true }).trim();
  if (status) {
    console.error(`Refusing: working tree is dirty. Commit or stash first.\n${status}`);
    process.exit(1);
  }

  // 2. On main
  const branch = currentBranch();
  if (branch !== "main" && !forceBranch) {
    console.error(
      `Refusing: current branch is "${branch}", not "main". ` +
        `Pass --force-branch to release from a different branch.`,
    );
    process.exit(1);
  }

  // 3. Up-to-date with origin (refuse if behind — would push a stale tag)
  git("fetch", "origin", branch);
  const behind = git("rev-list", "--count", `HEAD..origin/${branch}`, { capture: true }).trim();
  if (behind !== "0") {
    console.error(`Refusing: local "${branch}" is ${behind} commit(s) behind origin. Pull first.`);
    process.exit(1);
  }
}

function currentBranch(): string {
  return git("rev-parse", "--abbrev-ref", "HEAD", { capture: true }).trim();
}

function readVersion(pkgPath: string): string {
  const pkg = JSON.parse(readFileSync(pkgPath, "utf8")) as { version?: string };
  if (!pkg.version) {
    console.error(`Refusing: ${pkgPath} has no "version" field.`);
    process.exit(1);
  }
  return pkg.version;
}

/** The daemon health version (Go const `agent.Version`). */
function readDaemonVersion(): string {
  try {
    const m = readFileSync(AGENT_GO, "utf8").match(/Version\s*=\s*"([^"]+)"/);
    return m ? m[1] : "unknown";
  } catch {
    return "unknown";
  }
}

function writeVersion(pkgPath: string, nextVersion: string): void {
  const raw = readFileSync(pkgPath, "utf8");
  const re = /("version"\s*:\s*")[^"]+(")/;
  // Error only if the field is genuinely absent — NOT when it's already at the target value (a no-op).
  if (!re.test(raw)) {
    console.error(`Refusing: could not locate version field in ${pkgPath}.`);
    process.exit(1);
  }
  // Preserve formatting + trailing newline. Surgical replacement of the version field rather than full
  // re-serialization avoids reformatting the whole file (which would create noisy diffs).
  const replaced = raw.replace(re, (_, a, b) => `${a}${nextVersion}${b}`);
  if (replaced !== raw) writeFileSync(pkgPath, replaced);
}

function writeDaemonVersion(nextVersion: string): void {
  const raw = readFileSync(AGENT_GO, "utf8");
  const re = /(const Version\s*=\s*")[^"]+(")/;
  if (!re.test(raw)) {
    console.error(`Refusing: could not locate daemon Version in ${AGENT_GO}.`);
    process.exit(1);
  }
  const replaced = raw.replace(re, (_, a, b) => `${a}${nextVersion}${b}`);
  if (replaced !== raw) writeFileSync(AGENT_GO, replaced);
}

interface SemverParts {
  major: number;
  minor: number;
  patch: number;
  /** e.g. "rc.1", "beta.3" — undefined when stable. */
  prerelease?: string;
}

function parseSemver(v: string): SemverParts {
  const m = v.match(/^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$/);
  if (!m) {
    console.error(`Refusing: "${v}" is not a parseable semver.`);
    process.exit(1);
  }
  return {
    major: Number(m[1]),
    minor: Number(m[2]),
    patch: Number(m[3]),
    prerelease: m[4],
  };
}

function formatSemver(p: SemverParts): string {
  const base = `${p.major}.${p.minor}.${p.patch}`;
  return p.prerelease ? `${base}-${p.prerelease}` : base;
}

function computeNext(currentVersion: string, b: { kind: BumpKind; literal?: string }): string {
  if (b.kind === "current") return currentVersion;
  if (b.kind === "literal") return b.literal!;
  const parsed = parseSemver(currentVersion);
  switch (b.kind) {
    case "patch":
      return formatSemver({
        major: parsed.major,
        minor: parsed.minor,
        patch: parsed.patch + (parsed.prerelease ? 0 : 1),
      });
    case "minor":
      return formatSemver({ major: parsed.major, minor: parsed.minor + 1, patch: 0 });
    case "major":
      return formatSemver({ major: parsed.major + 1, minor: 0, patch: 0 });
    case "rc": {
      // rc → next rc OR rc → stable promotion
      if (parsed.prerelease) {
        const rcMatch = parsed.prerelease.match(/^rc\.(\d+)$/);
        if (rcMatch) {
          // currently rc.N — bump to rc.(N+1)
          return formatSemver({ ...parsed, prerelease: `rc.${Number(rcMatch[1]) + 1}` });
        }
        // some other prerelease (beta.N etc.) — bump patch + start rc.1
        return formatSemver({
          major: parsed.major,
          minor: parsed.minor,
          patch: parsed.patch + 1,
          prerelease: "rc.1",
        });
      }
      // stable → next patch rc.1
      return formatSemver({
        major: parsed.major,
        minor: parsed.minor,
        patch: parsed.patch + 1,
        prerelease: "rc.1",
      });
    }
    default:
      return currentVersion;
  }
}

function git(...gitArgs: (string | { capture: true })[]): string {
  const last = gitArgs[gitArgs.length - 1];
  const capture =
    typeof last === "object" && last !== null && (last as { capture?: boolean }).capture === true;
  const realArgs = (capture ? gitArgs.slice(0, -1) : gitArgs) as string[];
  const result = spawnSync("git", realArgs, {
    cwd: ROOT,
    stdio: capture ? ["ignore", "pipe", "inherit"] : "inherit",
    encoding: "utf8",
  });
  if (result.status !== 0) {
    console.error(`git ${realArgs.join(" ")} exited ${result.status}`);
    process.exit(result.status ?? 1);
  }
  return capture ? (result.stdout ?? "") : "";
}

function log(msg: string): void {
  console.log(msg);
}

/** Dim text when the terminal supports colour; plain otherwise. */
function dim(s: string): string {
  return process.stdout.isTTY ? `\x1b[2m${s}\x1b[0m` : s;
}

/* ─── Interactive wizard ───────────────────────────────────────────────
 *
 * Prompting only: it collects answers and hands them to `buildWizardArgs` (scripts/release-args.ts) to
 * become argv the flag path already understands. null = cancelled.
 */

async function runWizard(): Promise<string[] | null> {
  const { createInterface } = await import("node:readline/promises");
  const rl = createInterface({ input: process.stdin, output: process.stdout });
  const CANCEL = Symbol("cancel");

  /** Numbered menu. Enter takes the default; `q` cancels. */
  const pick = async <T extends string>(
    title: string,
    opts: { value: T; label: string; hint?: string }[],
    defaultIndex = 0,
  ): Promise<T | typeof CANCEL> => {
    console.log(`\n${title}`);
    opts.forEach((o, i) => {
      const mark = i === defaultIndex ? "›" : " ";
      console.log(`  ${mark} ${i + 1}. ${o.label}${o.hint ? `  ${dim(o.hint)}` : ""}`);
    });
    for (;;) {
      const raw = (await rl.question(`  choice [${defaultIndex + 1}] `)).trim().toLowerCase();
      if (raw === "q") return CANCEL;
      if (!raw) return opts[defaultIndex]!.value;
      const n = Number(raw);
      if (Number.isInteger(n) && n >= 1 && n <= opts.length) return opts[n - 1]!.value;
      console.log(`  ${dim(`enter 1-${opts.length}, or q to cancel`)}`);
    }
  };

  const ask = async (q: string, def = ""): Promise<string> => {
    const raw = (await rl.question(`  ${q}${def ? ` [${def}]` : ""} `)).trim();
    return raw || def;
  };

  const confirm = async (q: string, def = false): Promise<boolean> => {
    const raw = (await rl.question(`  ${q} [${def ? "Y/n" : "y/N"}] `)).trim().toLowerCase();
    if (!raw) return def;
    return raw === "y" || raw === "yes";
  };

  try {
    const cur = readVersion(SDK_PKG);
    const branch = currentBranch();
    console.log("");
    console.log(`  MindWire release`);
    console.log(`  current version  ${cur}`);
    console.log(`  daemon binary    ${readDaemonVersion()}${dim("  (synced to the SDK release)")}`);
    console.log(`  branch           ${branch}${branch === "main" ? "" : dim("  (not main)")}`);
    console.log(dim(`  Enter accepts the ›default; q cancels.`));

    const mode = await pick("What do you want to release?", [
      { value: "version" as const, label: "A version", hint: "bump + tag + GitHub release + npm" },
      { value: "docker" as const, label: "Console image only", hint: "GHCR; no tag, no release, :latest untouched" },
    ]);
    if (mode === CANCEL) return null;

    const answers: WizardAnswers = { mode, dryRun: false, forceBranch: false };

    if (mode === "docker") {
      answers.dockerTag = await ask("image tag (empty = short SHA):");
      answers.dockerRef = await ask("branch to build:", branch);
    } else {
      // Preview each bump's real target so the choice is concrete.
      const preview = (k: BumpKind) => computeNext(cur, { kind: k });
      const chosenBump = await pick(
        "Which version?",
        [
          { value: "patch" as const, label: "patch", hint: `→ ${preview("patch")}` },
          { value: "minor" as const, label: "minor", hint: `→ ${preview("minor")}` },
          { value: "major" as const, label: "major", hint: `→ ${preview("major")}` },
          { value: "rc" as const, label: "rc", hint: `→ ${preview("rc")}` },
          { value: "current" as const, label: "current, as-is", hint: `→ ${cur} (no bump)` },
          { value: "custom" as const, label: "custom semver…", hint: "e.g. 0.5.0-beta.2" },
        ],
        0,
      );
      if (chosenBump === CANCEL) return null;
      answers.bump = chosenBump;

      if (chosenBump === "custom") {
        for (;;) {
          const v = await ask("version:");
          if (!v) return null;
          if (/^\d+\.\d+\.\d+(-[a-z0-9]+(\.\d+)?)?$/i.test(v)) {
            answers.literal = v;
            break;
          }
          console.log(`  ${dim("not a semver string — e.g. 0.5.0 or 0.5.0-rc.1")}`);
        }
      }

      const target = chosenBump === "custom" ? answers.literal! : computeNext(cur, { kind: chosenBump });

      // Re-releasing a tag that already exists needs --force downstream (it deletes + re-pushes). Surface
      // it here so the wizard can salvage a failed release instead of dead-ending on "tag already exists".
      if (tagExists(`v${target}`)) {
        console.log(`\n  ${dim(`tag v${target} already exists — a prior release used it.`)}`);
        const doForce = await confirm(
          `Re-release v${target}? Deletes the old tag + re-pushes to fire a fresh build`,
          true,
        );
        if (!doForce) return null;
        answers.force = true;
      }
    }

    if (branch !== "main") {
      console.log(`\n  ${dim(`You are on "${branch}", not main. A real release from here needs --force-branch.`)}`);
      answers.forceBranch = await confirm(`Release from "${branch}" anyway?`, false);
      if (!answers.forceBranch) return null;
    }

    const built = buildWizardArgs(answers);
    console.log("");
    console.log(`  Plan: bun run release ${built.join(" ")}`);
    return (await confirm("Run it?", true)) ? built : null;
  } finally {
    rl.close();
  }
}
