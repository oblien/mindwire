# Personal computer connections

```sh
npm install -g mindwire
mindwire connect
```

The npm CLI downloads the matching, checksum-verified daemon release and starts a
background controller. It displays a short-lived QR invitation and asks the owner
to approve the phone's key. Concurrent starts reuse the controller; the daemon
also locks its identity directory. Closing the pairing terminal leaves the service
running. First-time pairing asks about per-user automatic startup: launchd on macOS,
systemd's user service on Linux, or Task Scheduler on Windows. Mindwire starts
after the owner signs in, when enabled, and recovers failed controllers, daemons and tunnel
helpers. An existing healthy daemon and relay are adopted after a controller
crash, preserving its running terminals and agent work.

Launch and startup registration share a process-identity lock queue. Concurrent
requests serialize even when reclaiming stale locks, and new controller/guardian
records include process birth so a reused PID cannot certify an unrelated process.
Startup is reported as running only after the native guardian has launched.

Use `mindwire startup status`, `mindwire startup enable`, or
`mindwire startup disable` to manage this. `mindwire connect --no-startup` leaves
existing startup settings unchanged and records a first-time opt-out. A previously disabled
startup preference is retained. Noninteractive runs leave startup unchanged unless passed
`--startup`; use this flag to opt in without a prompt.
Unsupported/unavailable native startup is reported without blocking pairing;
`mindwire start` remains available. No administrator/root service is installed.
`mindwire stop` records a durable intentional stop before signalling the controller,
so recovery and future logins cannot undo it. `mindwire start` resumes it.

QR codes contain the existing raw JSON invitation, which the mobile scanner already
accepts. This avoids base64 expansion while retaining every route, the full host-key
fingerprint and the one-time secret. Copyable `mindwire://pair` links remain supported.

The CLI measures the QR against both terminal dimensions and redraws on resize.
If it cannot fit, it opens a responsive local browser page. Nothing is sent to a QR
hosting service: the page contains an inline SVG in a private temporary file,
removed when pairing finishes, expires or is interrupted. Return to the terminal
to approve the phone. Use `--qr browser` to always use the browser, `--qr terminal`
to keep the code in a terminal (enlarge it when prompted), or `--no-qr` for a link.
Piped output and dumb terminals use plain links; `--json` stays machine-readable.

The default Cloudflare tunnel works across networks. Direct SSH, VPN addresses,
ngrok and a configured WSS relay use the same protocol and pinned identity.
A temporary tunnel's address changes when restarted; refresh it or configure a
stable hostname/VPN. No system SSH account or public daemon HTTP listener is added.
The controller retries the helper with bounded backoff independently of daemon
updates, and withdraws dead routes instead of advertising them as reachable.
Changing the computer's LAN address refreshes its routes. Mobile clients learn
new routes only after verifying the same computer/registry over pinned SSH.
There is no third-party address registry: if every saved route changes while the
phone is away, a temporary tunnel needs another QR scan. A named Cloudflare tunnel,
stable ngrok hostname, custom stable WSS endpoint or VPN avoids this limitation.

`mindwire connect` recognizes approved phones and offers Resume, Refresh address,
or Pair another phone. Noninteractive connect resumes by default. `mindwire connect resume`
never creates an invitation; `mindwire connect pair` explicitly pairs another phone.
`GET /computer/devices` reports `connected` from live authenticated SSH connections.

`mindwire reconnect` generates a five-minute `mindwire://reconnect` code containing only
the computer/registry IDs, pinned fingerprint and routes. It contains no pairing secret or
access credential. The phone must already hold the matching identity and device key,
authenticate over SSH using its saved pin, and verify `/computer` before saving routes.
Scanning it updates the existing workspace. It does not request `/pair` or create another key.

On iPhone, open the saved workspace's **Reconnect Computer** action, then **Scan
Connection Code**. The same action is available in its list context menu and connection
settings, including when the computer is offline. Recovery first tries the saved routes;
scanning supplies changed routes and clears stale offline status after verifying SSH.
Concurrent recovery actions join one attempt. `mindwire reconnect` does not interrupt
recovery with the first-pairing startup prompt, and prints the exact mobile scan path.
Phones with the same display name show separate key identifiers instead of being conflated.

**Remove from This iPhone** forgets the connection, keys, and mobile caches without contacting
the computer or deleting its native chats/files. Pending reads cannot restore a removed entry.

## Workspace and terminals

The approved phone uses the existing authenticated HTTP/SSE workspace API over
SSH `direct-tcpip`. WSS is an optional carrier for the encrypted SSH stream.
Projects, agents, chat, files and Git use the same registry and APIs as other hosts.

`/workspace/terminals` owns persistent PTYs, with stable creation IDs, serialized
input receipts, bounded output replay, resize and explicit close. Detaching the
phone does not end the shell. Unix uses native PTYs; Windows uses ConPTY. The shell
inherits the launching user's environment and runs as that user. Active terminals,
runs, pairing and private port grants prevent an idle update from restarting it.
PTYs do not survive an operating-system reboot or an explicit forced service stop.
The computer must remain awake and online; startup and recovery cannot wake a
powered-off machine or override its sleep policy.

iOS observes actual network path changes and foregrounding, closes stale SSH
connections immediately, and suspends connection attempts while offline. Existing
SSE cursors and terminal input receipts resume the same operation when a route
returns. Private previews rebind the same local port after recovery.

`/healthz.turnRequestVersion: 1` advertises durable `POST /turns` receipts.
Clients keep a `requestId` across retries; the daemon records the original run and
user ingress atomically before starting the harness. The same ID/payload returns
that run even after completion/restart; changed payloads return 409. Receipts contain
a payload digest, not credentials or another transcript. iOS retains the ID with a
failed draft and only automatically retries Send on daemons advertising support.

## Private ports

`GET /computer` advertises `portForwardingVersion: 1`. These routes require the
normal daemon bearer token and exist only in computer mode:

| Route | Purpose |
|---|---|
| `GET /computer/forwards` | List current grants |
| `POST /computer/forwards` | Create or renew `{id, deviceId, port}` |
| `DELETE /computer/forwards/{id}` | Close a grant and its live channels |

A grant authorizes one approved device to connect to `127.0.0.1:port` using SSH
`direct-tcpip`. It expires in two minutes; an attached client renews the same ID
every 30 seconds. The target cannot change under that ID. There is at most one
grant per device/port, 16 per device and 64 in total. Hostnames, other machines and
Mindwire control ports are excluded. Expiry, closure, device revocation and service
shutdown terminate its channels. Grants stay in memory and do not expose public
ports or change the firewall.

The TypeScript SDK exposes `computer.forwards()`, `computer.forward(request)` and
`computer.closeForward(id)`. The iOS app binds an ephemeral listener on the phone's
loopback interface using its existing native SSH forwarder. A native web preview
can consume that local address, including WebSocket traffic. Plain SSH servers use
their own SSH forwarding authorization and do not need these computer grants.

Desktop screen capture and control of a personal computer are separate capabilities;
terminal access and private ports do not advertise desktop sharing.
