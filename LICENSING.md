# Component licenses

Mindwire is a monorepo containing separately licensed components. The Apache-2.0 license at the
repository root does not replace the licenses attached to imported application code.

| Component | License |
| --- | --- |
| `packages/daemon`, including its Go SDK | Apache-2.0 |
| `packages/sdk` | Apache-2.0 |
| `packages/python` | Apache-2.0 |
| `apps/console`, `apps/web` | Apache-2.0 |
| `docker` and shared runtime/release scripts | Apache-2.0 |
| Original Mindwire material in `apps/apple` | Mindwire Apple Client Source-Available License 1.0; see `apps/apple/LICENSE` |
| `docs/apple` | AGPL-3.0; see its `LICENSE` |
| `scripts/app-store`, `scripts/icons` | AGPL-3.0; see their `LICENSE` files |

Third-party libraries, icons, harness logos, and other attributed artwork retain their own licenses
and notices. See [Apple third-party notices](apps/apple/THIRD_PARTY_NOTICES.md). Repository
licenses do not grant rights to third-party trademarks.

## Apple client

The Apple client is source-available, not open source. Its license permits inspection and private
local builds for personal use, including the user's own work. Redistribution, rebranding,
commercial exploitation of the client code, and publishing derivative apps require separate
written permission, whether the resulting app is free or paid. Source-hosting platform rights
and third-party licenses are unaffected. Using the authorized official app for work or business
is not prohibited by this source-code license.

Independently written clients may use the Apache-2.0 runtime and SDKs, including in commercial
products, subject to their licenses. The Apple restrictions protect the Covered Material defined
in its license; they do not reserve exclusive access to the runtime or its interfaces.

The Apple license applies to original material that the licensor owns or is authorized to license.
It does not retroactively revoke any valid license rights previously granted, including AGPL
rights if any copies were previously supplied under those terms. No Git history is rewritten.

Contributions follow the license of the component being changed. Apple contributions also carry
the explicit copyright grant in section 5 of its license so they can be included in official
commercial distributions. Contributors retain their copyright. Do not copy Apple code into
Apache-2.0 components or otherwise treat a different component's license as permission to reuse it.
