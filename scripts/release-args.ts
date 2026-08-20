/**
 * The interactive release wizard's answers → argv translation.
 *
 * Split out of release.ts on purpose: release.ts has top-level side effects (it runs a release when
 * imported), so nothing in it can be imported and checked. This file is pure and dependency-free, so the
 * mapping that decides what a wizard run actually releases is inspectable and testable on its own.
 *
 * The wizard is a FRONT-END to the flags — it may only produce argv the non-interactive path already
 * understands, so the two can't drift.
 */

export type WizardBump = "patch" | "minor" | "major" | "rc" | "current" | "custom";

export interface WizardAnswers {
  mode: "version" | "docker";
  bump?: WizardBump;
  /** Literal semver when `bump === "custom"`. */
  literal?: string;
  /** Empty/undefined → the console image is tagged with the short SHA. */
  dockerTag?: string;
  dockerRef?: string;
  dryRun: boolean;
  forceBranch: boolean;
  /** Re-release an existing tag (delete + re-push). Maps to `--force`. */
  force?: boolean;
}

export function buildWizardArgs(a: WizardAnswers): string[] {
  const out: string[] = [];

  if (a.mode === "docker") {
    out.push("docker");
    if (a.dockerTag) out.push(a.dockerTag);
    if (a.dockerRef) out.push(`--ref=${a.dockerRef}`);
  } else {
    // "current" is also the no-arg default, but it MUST be explicit here: the wizard re-executes this
    // script, and an empty argv would land back in the wizard instead of releasing.
    out.push(a.bump === "custom" ? a.literal! : (a.bump ?? "current"));
  }

  if (a.dryRun) out.push("--dry-run");
  if (a.forceBranch) out.push("--force-branch");
  if (a.force) out.push("--force");
  return out;
}
