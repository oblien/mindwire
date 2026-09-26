# Security Policy

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately via one of:

- GitHub's [private vulnerability reporting](https://github.com/oblien/mindwire/security/advisories/new)
  ("Report a vulnerability" under the repository's **Security** tab), or
- Email **security@oblien.com**.

Please include:

- A description of the issue and its impact.
- Steps to reproduce (proof-of-concept if possible).
- Affected component (daemon / SDK) and version or commit.
- Any suggested remediation.

We aim to acknowledge reports within **3 business days** and to provide a remediation timeline
after triage. We'll keep you informed as we work on a fix and will credit you in the advisory
unless you prefer to remain anonymous.

## Scope & notes

- The **daemon** binds `127.0.0.1` by default and is intended to run on a trusted host/network
  or behind a gateway/tunnel. Startup requires `DAEMON_TOKEN` — the auth middleware requires a
  matching bearer token and compares it in constant time.
- The daemon executes agent CLIs and, through them, shell commands and file edits in its working
  directory. Treat the workspace (`AGENT_CWD`) and any configured agent as trusted, and scope
  agent permissions appropriately for automated/headless use.
- Credentials are stored in the daemon's local state file, namespaced per agent; secrets are
  never returned by the config API.

## Personal computer connections

`mindwire connect` exposes an SSH transport, directly or inside a WebSocket tunnel.
The workspace HTTP API stays on authenticated loopback. Relay providers terminate
HTTPS but carry encrypted SSH content; they can observe addresses and traffic timing
and can interrupt delivery. They cannot impersonate the computer without its pinned
SSH host key.

- Pairing invitations expire after five minutes and use a random 256-bit secret.
  The phone verifies the QR's host fingerprint before sending that secret. Scan a
  code displayed by the computer you intend to trust.
- A pairing connection can reach only the pairing handler, never workspace APIs,
  arbitrary ports, or a shell. A new device key needs explicit owner approval.
- `pairingVersion: 3` requires an Ed25519 signature on every `POST /pair`, including
  acknowledgement retries. A request ID or the QR secret alone cannot retrieve the
  approved credential. Status lookups omit credentials and retained request proofs.
  Existing mobile clients that sign their requests remain compatible; older unsigned
  pairing clients must update.
- Device revocation and replacement close the affected device's SSH connections,
  pairing connections and port grants, invalidate pending acknowledgements, and reject
  subsequent connections. Pairing transports close at invitation expiry. Revocation
  does not undo commands the device already executed or stop detached shell processes.
- Device private keys are generated on the phone. Computer credentials use iOS
  Keychain's `AfterFirstUnlockThisDeviceOnly` protection; all existing copies, including
  pending receipts, migrate in place on first Keychain use. New backups must not clone
  an approval onto another phone. This does not erase backups made before the change.
  Computer identity files use owner-only permissions. Workspace caches contain
  credential references.
- Private port forwards are explicitly granted per device, expire without renewal,
  and target computer loopback only. The phone's local forwarding listeners also bind
  loopback. No permission opens an unauthenticated public workspace API.

An approved phone has the files and command privileges of the computer account
running Mindwire. `--directory` chooses the initial directory; it is not a sandbox.
Mindwire cannot protect that account from an already compromised approved phone or
computer, malicious commands the user authorizes, or a compromised software supply
chain. Only pair devices you trust, and use `mindwire devices` / `mindwire revoke ID`
to manage access. Removing an offline computer from the app removes local keys; it
does not send a revocation to the computer.

Daemon and helper downloads use HTTPS and verified release checksums. CI and the
release gate run the computer security/race tests and `govulncheck`; a known
vulnerability in reachable daemon code blocks publication. This is defense in depth,
not a guarantee against unknown vulnerabilities or an independent penetration test.

## Supported versions

mindwire is pre-1.0. Security fixes are applied to the latest `main` and the most recent release.
