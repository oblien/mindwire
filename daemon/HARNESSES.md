# Harness versions and compatibility

The daemon owns CLI selection. The mobile app and SDKs request setup or an update; they never choose
an npm tag, infer compatibility by comparing version numbers, or run an installer themselves.

## Catalog and publication

`internal/toolchain/catalog.json` is both the bundled fallback and the independently published
compatibility channel. Its HTTPS URL is
`https://raw.githubusercontent.com/oblien/mindwire/main/daemon/internal/toolchain/catalog.json`.
Merging a reviewed catalog change to `main` publishes new recommendations without changing a daemon
binary or an app release. Normal daemon releases also include this document as `harnessCompatibility`
in their immutable catalog sidecar. Do not replace an old release's sidecar to publish a new policy.

The schema has a separate `schemaVersion` and monotonically increasing `revision`. Each harness
declares exact release versions with one or more supported daemon ranges (`min` inclusive,
`maxExclusive` exclusive). The newest approved release matching the current daemon is recommended.
The catalog can therefore approve a new CLI for an older daemon, while another CLI release requires
new adapter code. A catalog entry cannot add protocol support to an existing binary.

An `excluded` entry names an explicit CLI range, daemon ranges and a human-readable reason. Use it
for a verified incompatibility, including a known minimum-version requirement. Unlisted versions are
**untested**, not presumed broken. Existing untested CLIs remain usable; the managed installer only
selects explicitly approved releases. Automatic downgrade is never permitted.

Before publishing a new pairing:

1. Exercise that exact CLI with the daemon versions covered by the new range. Validate native
   streaming, native history, resume, file/tool events, questions, approvals and cancellation.
2. Add captured protocol regressions when the native format changed. Record the tested versions and
   results in the change description; schema validation alone is not a native compatibility test.
3. Add the exact release and daemon ranges, retain entries needed by older supported daemons, and
   increment `revision`. Do not widen a range just because both releases use SemVer.
4. Run the catalog workflow and normal release checks. Merge the reviewed metadata to publish it.

Version 1 starts with Codex 0.155.0, Claude Code 2.1.246, Grok Build 1.0.5 and OpenCode 1.18.19 for
the current 0.1 daemon adapter line. The ordinary release gate tests the adapter contracts and refuses
an image build without an approved bundled CLI for its daemon version. Compatibility with a future
0.2 daemon must be declared explicitly.

## Cache and installation

`GET /agent` reports the cached decision. `GET /agent/software` refreshes the catalog when it is older
than one hour; `?refresh=true` requests an explicit refresh. Requests coalesce and have a five-second
network bound. Failed fetches back off for one minute. Invalid documents, unsupported schemas,
backwards revisions and same-revision content changes never replace the last accepted catalog.

The accepted document persists under `~/.mindwire/toolchains/compatibility.json`. An outage keeps it;
without a usable cache the daemon uses its embedded catalog. Starting a chat uses cached policy and
does not contact the catalog host. Refreshing metadata never installs anything or interrupts a turn.

New managed installations live under `~/.mindwire/toolchains/<binary>/versions/<version>`. The
adapter declares its npm package and invocation in code; the remote catalog cannot provide package
names, executable URLs or shell commands. npm installs the exact approved package into a staging
directory. The daemon verifies its actual native version before moving it into place and atomically
writing `selected.json`. Failed installs preserve the previous selection and its files.

All native invocations, including auth, help/schema discovery and version checks, resolve that same
selection. Login shells restore the selected path after shell startup files run, which matters on
macOS. Managed Claude and OpenCode disable their native auto-updaters; Grok uses its native
`--no-auto-update` flag. Old version directories remain available and native session/config files are
never moved or downgraded. A future rollback action must account for native session-format changes.

Setup reuses an existing CLI. An explicit update installs a newer supported version privately,
leaving system/global binaries untouched. Setup, updates and turns share one idle-harness gate per
workspace, and package-manager writes also share the existing mutation lock. Runtime images obtain
their pinned package list from `mindwired --print-toolchains`, using the same adapter declarations
and catalog, rather than independently installing latest.

`MINDWIRE_TOOLCHAIN_DIR` overrides the managed installation directory. `MINDWIRE_HARNESS_CATALOG_URL`
overrides the HTTPS metadata source for controlled deployments; `off` uses the bundled/disk policy
without network access. HTTP is accepted only on loopback for integration tests. Switching source
does not import a cache accepted from another URL.

## Client contract and migration

Health and catalog advertise `harnessPolicyVersion: 1`. `GET /agent` includes `software`, and the
lightweight software endpoint returns the installed/recommended/latest catalog versions, whether
the installation is managed, compatibility, update availability, any required daemon version, and
catalog revision/source/staleness. Go `Client.Software` and TypeScript `Mindwire.software` use the
same evaluator as HTTP. `POST /setup` and `POST /update` retain their existing asynchronous job API.

`software.missingDependencies` reports missing shared tools even when a CLI is installed. The
daemon preserves the adapter's dependency graph when binding the managed installer. Setup repairs
those tools without upgrading an existing CLI, and invalidates cached dependency checks when the
job ends. Checks are shared for 30 seconds; explicit software refresh bypasses that cache.

Codex requires the distribution's `bubblewrap` package (`bwrap` on PATH) on Linux. It is installed
through the shared setup graph and included in runtime images. macOS uses built-in Seatbelt and
does not install Bubblewrap. This follows [Codex sandbox prerequisites](https://developers.openai.com/codex/sandboxing).
If a host restricts user namespaces, follow its distribution's Bubblewrap/AppArmor profile guidance;
Mindwire does not disable the sandbox or change global kernel security settings.

Clients render the daemon's decisions. A supported update and a newer release requiring a daemon
upgrade can coexist; `recommendedVersion` remains the install target. Known incompatible versions
are rejected on new turn admission, while existing runs finish normally. Unknown future client
policy versions must not unlock an unrestricted install button.

Older daemons have no enforcement capability. They require one daemon upgrade to adopt the policy;
publishing catalog metadata cannot retrofit it. The iOS app keeps existing CLIs usable on those
services but requires the service upgrade before invoking a fresh install or update. Per-harness
software checks are shared by personas and cached for five minutes; manual refresh bypasses that
client cache, and a late response cannot undo a completed installation.
