# Personal computer connections

```sh
npm install -g mindwire
mindwire connect
```

The npm CLI downloads the matching, checksum-verified daemon release and starts a
background controller. It displays a short-lived QR invitation and asks the owner
to approve the phone's key. Concurrent starts reuse the controller; the daemon
also locks its identity directory. Closing the pairing terminal leaves the service
running. This does not install a system login/startup service: after reboot, use
`mindwire start`.

The default Cloudflare tunnel works across networks. Direct SSH, VPN addresses,
ngrok and a configured WSS relay use the same protocol and pinned identity.
A temporary tunnel's address changes when restarted; re-pair it or configure a
stable hostname/VPN. No system SSH account or public daemon HTTP listener is added.

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
