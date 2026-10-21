# Mindwire Console

Mindwire's hosted account and Cloud Computer management service. The browser portal manages account details, signed-in connections, notification devices, computer purchases, power and monthly subscriptions. Agent conversations, terminals and personal computers stay in the Mindwire app.

Console is not a self-hosted Mindwire client. Personal computers run the released `mindwired` binary independently and do not need Console or a Cloud subscription.

## Development

From the repository root, run `bun install` then `bun dev` to start the landing site (http://localhost:3000), account API, and SaaS console together. Open http://127.0.0.1:5174 and create a development account. Use `bun dev console` to start only the console and API. The API runs on port 8787 with SQLite; no Go compiler, Docker engine, agent runtime or provider credentials are required to open the portal. Set server-only reseller credentials to exercise Cloud purchases against an appropriate provider environment.

Run `bun --filter=@mindwire/console run check`, `run test`, and `run build` to validate changes. Cloud ownership, billing reconciliation, push migration and account management tests use a local provider fixture. Set `CLOUD_TEST_POSTGRES_URL` to run the Cloud tests against PostgreSQL.

## Hosted deployment

The root `docker-compose.yml` builds the website and Console from source and starts PostgreSQL. Configure `.env.example` before deployment. Required production values are `AUTH_SECRET`, a separate `SECRETS_ENCRYPTION_KEY`, `POSTGRES_PASSWORD` (or `DATABASE_URL`) and the public `BASE_URL`. Preserve signing/encryption keys across releases. Cloud purchases also require server-only `OBLIEN_CLIENT_ID`, `OBLIEN_CLIENT_SECRET` and the webhook secret; configure `computers.mindwire.sh` with the runtime gateway before releasing the native app.

See [Cloud Computers](../web/content/docs/guides/cloud-computers.mdx) and [source deployment](../../docker/README.md). Firebase credentials remain outside the repository and build context; mount them read-only using the push Compose overlay.

The reseller credentials must belong to an Enterprise provider account with the
permissions needed for namespaces, billing and computers. The provider must also
allow the host of `BASE_URL` (normally `console.mindwire.sh`) in its own
`REDIRECT_ALLOWED_HOSTS`. Setting that variable on Console does not update the
provider's allowlist.

Cloud failures return a stable code and diagnostic reference. Find the matching
`[cloud] provider request failed` line in the Console server log for the upstream
operation, status and provider request reference. Credentials and response bodies
are excluded. For example, `billing_redirect_not_allowed` requires the provider's
return-host configuration; `reseller_enterprise_required` requires enabling the
provider account for resale. These are not fixed by resetting the Console database.
After correcting the configuration, use **Continue checkout** on the saved
computer. Its original purchase and idempotency key are reused.

## API boundaries

- `/api/account`: Better Auth sessions, profile, password and native device authorization.
- `/api/portal`: account-owned connection listing and revocation by opaque ID. Session tokens never appear in this listing.
- `/api/cloud`: owned computer inventory, purchases, checkout, subscription management and the constrained provider gateway.
- `/api/notifications`: notification registrations and send-only credentials.
- `/api/computer-directory/v1`: encrypted personal computer discovery, independent from browser login.

Console does not expose agent execution, chat, SSH or local Docker APIs. Existing database records are retained; this refactor does not erase stored account, Cloud, directory or notification data.

## Visual theme

The portal and sign-in screens share the website palette in
[`packages/theme/tokens.css`](../../packages/theme/tokens.css). Console primitives
map their semantic tokens in `src/index.css`; feature CSS owns layout and
illustrations. The light default and stored `mw-theme` preference are applied
before paint. Both themes use the same rounded controls and Gellix typeface.

The account shell uses an inset desktop sidebar and an expandable mobile menu.
Check empty and populated accounts, error states, form focus, plan selection,
and confirmation dialogs at narrow widths. For visual review, intercept API
requests in a browser test with local fixtures; never use a real account change,
checkout, session revocation, or provider operation to test appearance.
