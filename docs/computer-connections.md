# Persistent computer connections

The computer identity and phone approvals belong to `mindwired`. The CLI owns
startup and the selected network provider. iOS uses the same SSH, workspace HTTP,
event-stream, and terminal APIs for all providers. A relay restart never creates a
new workspace or phone key.

## Ownership and recovery

| Owner | Responsibility |
| --- | --- |
| `mindwired` | SSH host identity, approved phone keys, workspace state, harnesses, files, and PTYs |
| CLI controller | One daemon, one relay, route verification, serialized provider changes, and recovery |
| Per-user startup job | Restart the controller after login or a controller crash; respect a deliberate stop |
| Provider worker | Carry encrypted SSH to the loopback WebSocket bridge using saved provider credentials |
| iOS | Pinned SSH authentication, shared connection leases, network-change handling, and bounded retry |
| Existing Mindwire Console backend | Key-based enrollment, encrypted addresses, signed lookup, durable revisions and revocation |

`mindwire connection` defaults to **Automatic**: a free Cloudflare tunnel with the
Mindwire address directory, requiring no Mindwire or Oblien account. It also
offers Oblien, a Cloudflare named tunnel, a reserved ngrok domain, a VPN hostname,
an existing WSS tunnel, or temporary Cloudflare without discovery. A named address
survives a tunnel restart. The directory lets a paired phone discover a changed
temporary address using its existing key.

`mindwire connection automatic` sets up the default route. `mindwire discovery enable`
adds recovery while keeping the current provider. Update the
[existing Console backend](../apps/console/README.md), or choose a self-hosted
Console with `--directory-url` or `MINDWIRE_DIRECTORY_URL`. The chosen URL is saved
on the computer and learned by the phone through pinned SSH. Host signatures own
the address registration; website login has no role. The backend never carries
workspace traffic. QR establishes the initial trust between computer and phone.

The controller verifies the binary Mindwire SSH banner through the public WSS
route before publishing readiness. Repeated clicks join a settings change. A new
endpoint is verified before committing its configuration. Reauthentication at the
same Oblien/ngrok endpoint briefly replaces its single listener; failure restores
the previous provider settings. Neither path restarts the daemon or its PTYs.

Provider settings, login credentials, and worker configuration are private files
on the computer. A settings change stages new credentials separately from the
working connection. No provider token is put in a QR, daemon response, or mobile
cache. Public connection metadata is only `{provider, address}`. HTTP mutations
are not automatically replayed after a disconnect.

iOS shares an opening SSH connection across screens. Closing one screen releases
its lease without disconnecting other clients. Healthy checks are cached. A real
foreground/network change or a failed transport triggers recovery; repeated
failures back off from one second to one minute. Offline/background work stops.
Host-key and phone-key rejection require explicit repair. A late event from a
replaced pairing cannot invalidate the current connection.

## Oblien integration today

Use the official `oblien` SDK's `edgeTunnel` resource and `TunnelClient`. Browser
authentication currently uses documented auth endpoints through the SDK's shared
HTTP transport because the SDK has no public auth resource. Resource requests are
bounded and are not automatically retried when a mutation's outcome is unknown.

Each installation persists its own UUID before creating a tunnel. Its generated
slug recovers a create whose response was lost. Do not use account-wide port
matching: multiple computers can legitimately use port 8792.

Broker tokens are renewed before expiry. Renewal does not interrupt a healthy
carrier; the next reconnect uses the newest token for the same tunnel ID. The
private browser session is renewed while it is still valid. If the computer is
offline past that session's expiry, the user must sign in again using
`mindwire connection oblien`. Phone pairing remains valid. An expired session is
never exchanged for an anonymous session.

No paid-only behavior is assumed. The provider enforces tunnel/bandwidth limits;
the client handles its allowance error and keeps other providers available.

## Requested Oblien backend capability

The published API does **not** currently document a renewable credential scoped
to one computer tunnel. This is the remaining backend improvement for unattended
operation beyond browser-session expiry. The following is a proposed contract,
not an endpoint already implemented by Mindwire:

1. An authenticated account owner grants one installation access to **one owned
   tunnel ID**. Return a revocable grant ID and a private renewal secret once.
2. The grant can mint short-lived broker JWTs for that tunnel only. It cannot
   list/create other tunnels or access workspaces, files, account settings, or
   billing. The server binds the tunnel ID rather than accepting an arbitrary ID
   from the grant holder.
3. Renew/rotate the secret with an idempotency key and a bounded recovery window
   for a lost response. Concurrent renewal joins the same rotation; a retry must
   not strand a computer after the previous secret was consumed.
4. Owner revocation and tunnel deletion invalidate the grant and terminate its
   active broker connection. Enforce current account limits on issuance and
   renewal. Store only a hash of the renewal secret and redact credentials in logs.
5. Add create, exchange/renew, list-status, and revoke methods to the official SDK.
   Mindwire can then use that grant without storing a renewable account session
   for background operation. Keep ordinary browser login for initial consent.

The edge also needs stable tunnel DNS, long-lived binary WebSocket forwarding,
working heartbeat/close propagation, and monitored automatic TLS renewal. The
live test on 2026-09-30 caught an expired `edge.oblien.com` certificate; delivery
worked after the certificate was renewed. Certificate verification stays enabled.

The single-tunnel grant is independent of the new address directory. Directory
publication uses the computer's key after one signed enrollment and needs no
renewable account session. Cloudflare/ngrok accounts, a VPN, and self-hosted WSS remain independent
alternatives. Providers can see connection metadata and encrypted traffic sizes;
the pinned SSH session encrypts workspace content end to end.

## Acceptance checks

Build the SDK and daemon, then run the computer provider, recovery, transport,
startup, and pairing suites in `packages/sdk/test`. They cover singleflight
startup/provider changes, rollback, persistent address/key reuse, live PTY
preservation, broker renewal, TLS failure, and private credential handling.

`packages/sdk/scripts/computer-oblien-fixture.ts` is an opt-in live fixture. It
creates one disposable tunnel, verifies public WSS → SSH, and coordinates with
the iOS `ComputerPersistentConnectionTests` suite through private fixture files.
It advertises no usable LAN route. The native test pairs, uses files and a PTY,
simulates Wi-Fi loss/return on cellular, and reconnects after the relay process is
killed. It verifies the same daemon PID, phone key, terminal, file, and workspace.
CLI resume must produce no invitation. The fixture removes its temporary tunnel
and credentials afterward.

Cloudflare/ngrok setup and failure paths have isolated provider fixtures. Live
account/domain validation for those providers requires the owner's credentials;
the Oblien acceptance result does not claim those account-specific checks.
