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
  or behind a gateway/tunnel. When exposed, set `DAEMON_TOKEN` — the auth middleware requires a
  matching bearer token and compares it in constant time.
- The daemon executes agent CLIs and, through them, shell commands and file edits in its working
  directory. Treat the workspace (`AGENT_CWD`) and any configured agent as trusted, and scope
  agent permissions appropriately for automated/headless use.
- Credentials are stored in the daemon's local state file, namespaced per agent; secrets are
  never returned by the config API.

## Supported versions

mindwire is pre-1.0. Security fixes are applied to the latest `main` and the most recent release.
