# Computer address directory v1

Implementation: the existing [Mindwire Console backend](../apps/console/README.md),
the computer daemon/CLI, and iOS. The directory shares the website’s
database, server and deployment, but uses computer and phone keys instead of login. Deploy the updated Console before enabling the
hosted URL. Existing SSH, VPN, named tunnels and pairing remain independent.

## Service boundary

The hosted API is `https://console.mindwire.sh/api/computer-directory/v1`.
Self-hosted Console deployments use the same path under their existing `BASE_URL`.
Enrollment and publication use the computer’s Ed25519 key; lookup uses the paired
phone’s key. No Mindwire or Oblien account is needed, including first setup. The
directory stores no provider credentials, SSH private keys, encryption keys,
files, terminal output or chat content. Traffic stays on the chosen SSH carrier.

The backend implements atomic compare-and-swap publication, durable sequence
high-water marks, host-key revocation, durable enrollment quotas, and per-key/network
request limits.
TLS is mandatory; do not redirect these authenticated requests.
Signed payloads are opaque base64: do not rewrite or sanitize their decoded JSON.

`apps/console/server/computer-directory/` uses the existing Hono server and the
same PostgreSQL or SQLite connection as Better Auth. Checked-in Drizzle migrations
add `computer_directory` and `computer_directory_enrollment_budget`. A forward
migration removes legacy account ownership without changing saved keys, ciphertext,
revision numbers or revocation tombstones. The existing
Console image already ships these migrations. No additional service, hostname or
proxy is needed. Directory requests skip fleet creation and use their own precise
authorization rules.

`COMPUTER_DIRECTORY_MAX_REGISTRATIONS` defaults to 100,000 (maximum 10,000,000),
including revoked identity tombstones. `COMPUTER_DIRECTORY_ENROLLMENTS_PER_DAY`
defaults to 100 (maximum 10,000) per source IPv4 address or IPv6 /64. The database
stores an HMAC of the network, never its raw address. Idempotent enrollment does
not consume a second slot. Only new enrollment takes a database transaction lock;
publishing and lookup remain independent of admission checks. Daily budgets are
pruned after their UTC day; identity tombstones are retained to prevent rollback.

Optional `COMPUTER_DIRECTORY_TRUSTED_PROXIES` accepts IP CIDRs. Only those socket
peers may supply `X-Forwarded-For`; the chain is walked from the nearest proxy to
the first untrusted hop. Otherwise limits use the socket address. Quotas and
revisions live in the shared database; request rate limits are bounded per
process. Use the existing deployment’s ingress limits when scaling replicas.

## Setup and self-hosting

The first/default choice in `mindwire connect` is **Automatic**: a free Cloudflare
carrier plus this directory, without an account. To enable it explicitly:

```sh
mindwire connection automatic
```

Self-host the existing Console at a stable HTTPS origin with its persistent
database. Set its normal `BASE_URL` and server secrets, then choose its directory:

```sh
mindwire connection automatic --directory-url https://your-console.example/api/computer-directory/v1
# Or keep the current carrier and enable only address recovery:
mindwire discovery enable --directory-url https://your-console.example/api/computer-directory/v1
```

The CLI resolves the URL in this order: `--directory-url`, `MINDWIRE_DIRECTORY_URL`,
the daemon’s saved directory URL, a pending setup preference, then the hosted
default above. The desired URL is saved before enrollment so setup can retry
after a network failure. The daemon saves its signed configuration after successful
enrollment. The phone learns that exact URL and its
decryption key over pinned SSH; there is no second login or URL setting on the
phone. A default endpoint is convenient, not mandatory.

Existing quick Cloudflare connections enroll during controller reconciliation
unless the owner disabled recovery or selected the explicit temporary option.
Healthy setup does not poll the hosted directory from the CLI. Each saved phone
must connect once to save and acknowledge its key; only then can the directory
recover that phone after every old address has changed. The status/menu reports
this incomplete migration instead of claiming the phone is ready.

The directory’s origin and the computer’s key identity remain stable. The carrier
address may change after every restart. The directory is an address mailbox, not
a traffic relay: it stores signed, encrypted address updates, while files, chat,
terminal output and desktop traffic travel through the selected SSH carrier.
An Oblien account is needed only if the user explicitly chooses the Oblien carrier.

To change directories, run `mindwire discovery disable`, wait for withdrawal in
`mindwire discovery status`, then enable the new URL. Connect each saved phone
once while a cached route still works so it learns the new descriptor.

## Cryptography and wire format

All times are integer Unix seconds. Sequence numbers are integers 1 through
`9007199254740991`. Base64 uses the standard alphabet and padding, except IDs,
which use unpadded base64url.

`directoryId` is SHA-256 of the SSH wire encoding of the computer's Ed25519 public
key, encoded as base64url. This is the existing SSH fingerprint digest. A
`deviceId` is the same derivation for the approved phone key. The raw Ed25519 public
key is 32 bytes. Its SSH wire encoding is `uint32(11) || "ssh-ed25519" ||
uint32(32) || rawKey`, with big-endian lengths.

A signed envelope is `{publicKey, payload, signature}`. `publicKey` is the raw
32-byte key in base64. `payload` is base64 of UTF-8 JSON. Verify the 64-byte
Ed25519 signature over `UTF8(domain + "\n" + payload)`. The exact base64 payload
is signed; consumers do not reserialize JSON to verify it. Reject malformed or
oversized encodings before cryptographic work.

Domains are `mindwire-directory-enroll-v1`, `mindwire-directory-publish-v1`,
`mindwire-directory-record-v1`, and `mindwire-directory-revoke-v1`. Every payload binds `version: 1`, `audience` (the
exact base URL above, without a trailing slash), `directoryId`, `computerId`,
`issuedAt`, and `expiresAt`. Accept at most 120 seconds of future clock skew.

## Enrollment — computer key required, no account

1. `POST /registrations/challenge`, body `{directoryId}`. Return
   `{challenge, expiresAt}`. The opaque, HMAC-signed challenge binds the directory
   audience, computer ID and hashed source network and expires within five
   minutes. It uses the existing server-only `AUTH_SECRET`, so a backend restart
   does not invalidate in-flight enrollment. Its HMAC purpose is
   `mindwire-directory-challenge-v2`; old account-bound challenges are rejected.
   There is no challenge table, cookie, bearer credential or browser redirect.
2. The local daemon signs an enrollment payload containing the envelope header
   fields above plus `challenge`. The proof expires within five minutes.
3. `POST /registrations`, body: enrollment envelope. Verify the host signature,
   key-derived ID, audience, expiry and challenge binding. Atomically admit the
   registration under the configured quotas. Store its host public key and
   computer ID, with no account relationship. Return `{directoryId}` with 201, or
   200 for the same existing identity. Retrying after a lost response never creates
   another record, charges another quota slot or resets the revision. Return 409
   for a conflicting identity or revoked registration, 429 for daily admission
   limits, or 503 for global capacity. Existing publication/lookup remains available
   when enrollment is full.

There is no public or account-based registration list. Website cookies cannot
claim, modify or revoke a computer. Website signup, login and account deletion
have no effect on key-owned directory records; ordinary Console workspace APIs
remain protected by their existing session authentication.

`DELETE /registrations/:directoryId` requires an envelope signed by the registered
host key with domain `mindwire-directory-revoke-v1`, the same header fields and a
maximum five-minute lifetime. The path, key-derived ID and saved computer ID must
match. It atomically clears reader ciphertext and retains the key and revision
tombstone. Revocation is idempotent and permanent; enrollment/publication cannot
reactivate it. Use the ordinary signed `enabled:false` publication for reversible
withdrawal (`mindwire discovery disable`).

## Publication — host signature required

`PUT /records/:directoryId`, body: publication envelope. The signed payload adds:

```json
{
  "sequence": 12,
  "enabled": true,
  "readers": [
    {
      "deviceId": "<key digest>",
      "publicKey": "<raw phone key, base64>",
      "record": { "publicKey": "<host key>", "payload": "…", "signature": "…" }
    }
  ]
}
```

Verify against the registered host key. Maximum 64 unique readers; each
`deviceId` must match its phone public key. Each inner record must be signed by
the same host and match the outer audience, IDs, sequence and times. No reader
name is needed. An inner record additionally binds `registryId`, `deviceId`, and
`ciphertext`. The latter is base64 of `nonce(12) || AES-256-GCM ciphertext ||
tag(16)`. Its plaintext is `{routes, connection}` using Mindwire's existing route
and provider metadata types. Additional authenticated data is UTF-8:

```
mindwire-directory-address-v1\n<audience>\n<directoryId>\n<deviceId>\n<sequence>
```

The daemon delivers each phone's random-looking 32-byte encryption key only over
its authenticated, pinned SSH connection. It derives that key with HMAC-SHA256
keyed by the host's Ed25519 seed over
`mindwire-directory-key-v1\n<audience>\n<deviceId>`. Neither API nor QR receives it.
Every new record uses a cryptographically random GCM nonce.

The local daemon advertises discovery capability/status in `GET /computer`.
`POST /computer/discovery/enrollment` signs the hosted challenge without changing
settings. Only after successful enrollment does `PUT /computer/discovery`
enable publication. `GET /computer/discovery/devices/:id` supplies that phone's
key and the current sequence floor over SSH. iOS saves both in Keychain before
acknowledging `POST /computer/discovery/devices/:id/ready`. The CLI resumes
temporary addresses without a QR only for phones that acknowledged this setup.
These are local workspace-daemon APIs, **not endpoints on the hosted backend**.

Atomically compare sequence, replace the entire reader set and save the envelope
digest. Higher sequence wins. Same sequence **and identical envelope** is a
successful retry. The digest covers the three exact envelope field values;
outer JSON whitespace/field ordering does not change a retry. Same sequence with
different signed bytes or lower sequence returns
409 `stale_sequence`. Never lower/reset the high-water mark, including after
expiry. Return `{sequence}` only after durable commit. Readers omitted from the
new document lose lookup access immediately. `enabled:false` must have zero
readers and withdraws all discovery data while keeping the enrollment.

Maximum HTTP body: 1 MiB, with a ten-second read deadline; maximum decoded
plaintext address record: 32 KiB. At most 32 directory requests run concurrently
per server process.
Publications expire within 24 hours; the daemon renews after 12 hours. Expiry
removes address ciphertexts from serving/storage but preserves registration and
sequence metadata. One maintenance timer per server clears expired ciphertext,
past daily admission budgets and idle rate buckets. No
WebSocket, keepalive or per-computer background job is required on the backend.

## Lookup — approved phone signature required

`POST /records/:directoryId/lookup`, JSON:

```json
{ "deviceId": "…", "timestamp": 1790000000, "nonce": "<32 random bytes, base64url>", "signature": "<base64>" }
```

Verify with the phone public key in the **current** reader set. Signed bytes are:

```
mindwire-directory-lookup-v1\n<audience>\n<directoryId>\n<deviceId>\n<timestamp>\n<nonce>
```

Accept a 120-second timestamp window and strictly validate the nonce. The proof
authorizes only this read; it cannot enroll, publish, revoke or open SSH. Rate
limit reads per registered device and IP. A replay within the window can only
read that device's already-encrypted record. Do not introduce single-use tokens
whose lost response strands a phone. Return **only that phone's signed inner
record**, with `Cache-Control: no-store`, without other readers.

401: invalid proof. 403: revoked/disabled or phone not approved. 404: registration
or live record unavailable. 409: conflicting publication/enrollment. 413: too
large. 429: rate limited, with bounded `Retry-After`. 503: transient service issue.
Errors use `{error: {code, message}}`, with no credentials or request bodies.

The phone verifies the host fingerprint, signature, audience, computer/registry
and device IDs, expiry and sequence before decrypting. It durably retains the
last accepted sequence and digest to reject replay/rollback, then still pins the
SSH host key on the discovered route. Directory data alone grants no access.

## Failure behavior

The daemon saves a new signed publication and sequence **before** sending it;
after interruption it retries the exact pending envelope. Address/approval changes
coalesce, and only one publish runs at once. Failed delivery never replaces the
working SSH connection. Revocation closes the phone's SSH access immediately,
even when directory publication must wait for internet recovery.

The phone tries cached routes immediately. A delayed directory lookup runs only
when opening a saved connection is slow or fails, joins the shared connection
attempt and is cancelled if a cached route succeeds. Invalid/expired directory
data never overwrites cached routes or keys. Deleting the computer cancels pending
recovery and removes its encryption key; a late response cannot recreate it.

An old pairing learns discovery on its next successful SSH connection after both
clients are updated. If its old address is already gone before that first upgrade
connection, one QR refresh is still necessary. Offline/asleep computers cannot
be reached; recovery resumes when the computer and its carrier return. P2P/NAT
traversal is outside v1. Providers remain interchangeable.

## Validation

`go test -race ./internal/computer ./internal/api` (from `daemon`) covers durable
publication, lost receipts, concurrent updates, withdrawal after prolonged
offline time, per-phone encryption, revocation and API route parity.

`bun run test` (from `apps/console`) starts the real Node/Hono backend with its
isolated database. It verifies account-free enrollment, website/login independence,
key ownership, source-bound challenges, durable atomic quotas, IPv6/proxy handling,
signed publication, lost receipts, concurrent updates, restart persistence, expiry,
revocation and migration of legacy rows.

`bun test test/computer-directory.test.ts` (from `packages/sdk`) verifies zero-login
serialized setup, custom/environment URLs, safe setup failure and redirect rejection
against that same backend. Fixtures default to temporary SQLite. Set
`MINDWIRE_DIRECTORY_TEST_DATABASE_URL` to a test PostgreSQL administrator URL to
run the same suites with a fresh disposable database per fixture. Fixtures never
use the deployment's `DATABASE_URL`; each temporary database is removed afterward.
Existing provider/recovery suites exercise the controller and SSH carrier.

`packages/sdk/scripts/computer-directory-fixture.ts <test-mindwired-binary>` runs
a disposable real daemon, the existing Console backend with zero accounts, and a replaceable loopback
WebSocket carrier. Pass its private `nativeFile` (path recorded in
`/tmp/mindwire-directory-native-state.json`) to iOS tests as
`MINDWIRE_DIRECTORY_FIXTURE`. `ComputerDirectoryNativeTests` pairs once, opens a
terminal and writes a file, loses the old address, and reconnects concurrently
through one directory lookup after restarting the backend. It checks the same daemon, phone, PTY, file and
workspace, then verifies cached-route recovery while the directory is down.
Fixtures use HTTP/WS only on loopback in debug builds; production requires HTTPS
and WSS. This is local integration coverage, not proof of a hosted deployment.
