# @mindwire/console

A self-hostable operations console for MindWire. A
Vite + React + shadcn SPA on the left and center, a modern AI chat on the right, all driven by a
Hono/Node backend that owns the SDK. The whole app is about **deploying daemons**: connect running
ones, run them over SSH or in Docker containers, or spin up Oblien sandboxes — pick which daemon and
which agent (adapter) you're driving, configure it, and chat. It's **multi-user and fully
session-protected** — every user signs in (email/password, plus GitHub/Google in cloud mode) and gets
a fully isolated fleet; their API keys live in the daemon, never on this server.

## Fleet model

A session owns a **fleet** of daemons. Each daemon is one `Mindwire` client bound to one target, and
you can see where every daemon runs, spin it up / tear it down, duplicate it, and activate it:

- **Remote** — an already-running `mindwired` reached over its URL (always on; nothing to provision).
- **Docker** — `mindwired` in a container you own from an image, or an already-running container you
  attach to. Requires the optional `dockerode` peer on the server (see below).
- **Oblien** — a fresh microVM. Oblien is just one deployment provider; its API keys are **linked on
  demand** from the Add-daemon dialog (verified and held server-side), not required to use the app.

One daemon is **active** at a time (turns and the config panels target it), and within it one **agent**
(adapter type — claude-code / codex / grok / opencode / …) is selected. The **Fleet** surface manages the
daemons and shows how many agents each hosts and what it's currently doing; the **Agents** surface
picks which adapter the chat and config panels drive. Both switchers also live in the top nav.

Docker/Oblien provisioning streams progress over SSE (`POST /events/daemons/:id/up`); a temporary
daemon is reaped when it's stopped, removed, or the session is reset.

## Why a backend

The SDK is browser-safe **only** through `remote()`. `local()`, `oblien()`, `docker()`, and `ssh()`
pull in Node-only modules, so every non-`remote()` target must run server-side. The browser therefore
talks only to the Hono server (same-origin JSON + SSE); the server is the sole place that imports
`mindwire` at runtime. A server-side session (an httpOnly, signed cookie) is created automatically on
first load — it holds daemon tokens and any linked Oblien credentials, which never cross to the
client. The browser only ever sees a `SessionStatus` (`ready` plus an optional masked Oblien account).

```
Browser (Vite + React + shadcn)  ──fetch JSON + SSE──▶  Hono (Node)  ──SDK──▶  mindwired daemon(s)
                                                          └ session: a fleet of daemons (+ optional
                                                            linked Oblien creds); one Mindwire client
                                                            per daemon, keyed ${sessionId}:${daemonId}
```

The active-agent selection rides the request as `?agent=<type>`; the server applies `withAgent()` so
every capability surface targets the adapter you're managing without threading it through each call.

## Develop

From the repo root, `bun dev` brings up the daemon and this app together:

```bash
bun dev            # daemon (:8790) + console (Vite :5174 → Hono :8787); open http://127.0.0.1:5174
```

Or run just this app (expects a daemon reachable at `DAEMON_URL`):

```bash
bun --filter='@mindwire/console' run dev
```

Vite owns the browser at `:5174` and proxies `/api` + `/events` to the Hono server at `:8787`.

### Testing cloud / SaaS mode

`bun dev` mirrors a **self-hosted** deploy: it starts the daemon and seeds every new session with it, so
you can chat immediately. To exercise the **cloud / SaaS** experience — where a signed-in user starts
with an **empty fleet** and wires their own runtime (nothing is auto-run for them) — run the two halves
in separate terminals:

```bash
bun dev:demon      # the daemon only (:8790) — what an end user would connect to
bun dev:saas       # the console in cloud mode (CONSOLE_MODE=cloud); no daemon started, empty fleet
```

Then sign in, open the Console, **Add a runtime**, and point a `remote` daemon at `http://127.0.0.1:8790`
— exactly what a real SaaS user does. Social sign-in buttons appear only when the matching OAuth apps are
configured (see below).

## Build & deploy

```bash
bun --filter='@mindwire/console' run build   # vite build → dist/  +  tsup → dist-server/
bun --filter='@mindwire/console' run start   # node dist-server/index.js  (serves the SPA + API on one port)
```

In production one Node process serves the built SPA statically and the JSON API + SSE on the same
origin. Deploy it to `mindwire.sh` as a long-running Node server (not a static export — the SSE turn
stream needs the process).

### Environment

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8787` | Port the Hono server listens on (SPA + API in prod). |
| `DAEMON_URL` | `http://127.0.0.1:8790` | Default daemon a new session connects to (the user can override it in-app). |
| `MINDWIRE_RUNTIME_TOKEN` | *(empty)* | Bearer token for the deployment runtime. Server-side only. `DAEMON_TOKEN` remains a compatibility fallback. |
| `SEED_DEFAULT_DAEMON` | on for self-hosted, off for cloud | Seed a new session's fleet with `DAEMON_URL`. Off ⇒ users start with an empty fleet and wire their own runtime (the multi-tenant SaaS model). |
| `MINDWIRE_AGENT` | `claude-code` | Harness selected for the session. |
| `AUTH_SECRET` | *(dev placeholder)* | Better Auth signing key (≥32 chars). **Must be overridden in any real deployment.** `SESSION_SECRET` is accepted as a fallback name. |
| `CONSOLE_USERNAME`, `CONSOLE_PASSWORD` | required in self-host production | The one deployment-admin login. Self-host has no signup or OAuth. |
| `DATABASE_URL` | *(empty)* | Explicit Postgres connection URL (for example a managed database). Takes precedence over the `POSTGRES_*` settings and SQLite. |
| `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | *(empty)* | Postgres connection pieces. When `POSTGRES_HOST` is set, the console safely constructs its connection URL; root SaaS Compose supplies these automatically. |
| `AUTH_DB_PATH` | `/data/auth.db` in production, `../.data/auth.db` in dev | SQLite fallback (`node:sqlite`, Node 22+) for self-hosting. Point it at a persistent volume. |
| `BASE_URL` | `http://127.0.0.1:$PORT` | Public origin — used for auth cookies, CSRF/origin checks, and the OAuth callback base. Set this to your real URL in prod. |
| `TRUSTED_ORIGINS` | *(dev proxy in dev)* | Extra comma/space-separated browser origins allowed to call the auth endpoints. |
| `ALLOW_LOCAL_RUNTIME` | `false` in prod / `true` in dev | Allow the "control the current host" runtime (an embedded daemon on this machine). Keep **off** for multi-tenant cloud; opt in for a single-tenant self-host. |
| `OBLIEN_BASE_URL` | *(SDK default)* | Optional Oblien API base override. |
| `APP_NAME` | `MindWire` | Product name shown in the login + top-nav chrome. |
| `DOCS_URL` | `https://mindwire.sh/docs` | External docs link (in-app and on the login screen). |
| `GITHUB_URL` | `https://github.com/oblien/mindwire` | Public repo link (login footer + GitHub social button home). |

### Cloud vs. self-hosted (and social sign-in)

The console runs in one of two modes, chosen by `CONSOLE_MODE`:

| Var | Default | Purpose |
|---|---|---|
| `CONSOLE_MODE` | `self-hosted` | `cloud` = the hosted MindWire SaaS (enables social sign-in when OAuth apps are configured); anything else = `self-hosted`, email/password only. |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | *(empty)* | GitHub OAuth app. Both must be set for the **Continue with GitHub** button to appear (cloud mode only). |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | *(empty)* | Google OAuth app. Both must be set for the **Continue with Google** button to appear (cloud mode only). |

Register each provider's OAuth callback as `${BASE_URL}/api/account/callback/{github|google}`. A
half-configured provider (id without secret, or vice-versa) is treated as absent, so the login screen
never offers a button that can't complete. Client secrets stay server-side; the browser only reads the
secret-free `GET /api/public-config` (which social buttons to show, branding, links).

### Docker support (optional)

The **Docker** daemon provider needs the `dockerode` peer, declared here as an `optionalDependency`
and externalized from the server bundle (never inlined). If it isn't installed, the server reports
Docker as unavailable and the Add-daemon dialog disables that option — remote and Oblien daemons work
regardless. Install it where the server runs to enable Docker:

```bash
bun add dockerode        # (already pulled in by a normal workspace install)
```

Docs are external — the top-nav **Docs** link points at `https://mindwire.sh/docs`; nothing is rendered
in-app.
