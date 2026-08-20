// The SDK's own version, replaced at build time by tsup's `define` (see tsup.config.ts) with the
// literal from package.json. This is also the version of the bundled Linux daemon binary
// (build-daemon.mjs stamps the per-arch daemon packages to the same version), so a sandbox adapter
// can compare it against a running daemon's `/healthz` version to decide whether to (re)deploy.
//
// The `typeof` guard keeps this importable in dev/test where the define isn't applied (raw `tsc`,
// `bun test`) — it falls back to a sentinel instead of throwing on an undefined identifier.
declare const __MINDWIRE_VERSION__: string;

export const SDK_VERSION: string =
  typeof __MINDWIRE_VERSION__ !== "undefined" ? __MINDWIRE_VERSION__ : "0.0.0-dev";
