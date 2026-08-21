# Sandbox / Runtime Strategy

> Status: **draft for review** (no code yet). Author decision on 2026-08-09:
> build scope = *strategy doc first*; runtime layer = *yes, mirror the agent registry*.
> This document is internal strategy + architecture, not product docs — it does **not** live in
> `apps/web/content/docs` (that tree is user-facing only).

---

## 1. Thesis in one line

MindWire and Oblien are **two layers of one stack**:

- **MindWire = the agent control plane.** Normalize + drive any coding agent through one API/SDK.
  Open, free, the *wedge*.
- **Oblien = the runtime / data plane.** The isolated machine where the agent actually executes.
  Usage-metered, paid, the *monetization*.

MindWire's public promise is already *"swap the agent like a model."* The runtime layer gives it a
twin: **swap the sandbox like a model.** Same design DNA — an adapter registry — applied one layer
down. Agents plug in (`agent.All()`); runtimes plug in (`sandbox.All()`).

---

## 2. Why this is the right monetization

The thing MindWire makes easy — *let a coding agent run real operations inside your product* — is
also the thing that forces a hard infrastructure question the moment you ship: **where does the agent
run?** An agent runs untrusted shell, edits files, and hits the network on its own behalf. In
production, per end-user, that cannot sit on your host. It needs a real isolated machine with
lifecycle, networking, persistence, quotas, and per-tenant scoping.

So the control plane *manufactures demand* for the runtime plane. We don't have to convince anyone
they need a sandbox — the act of going to production does. Our job is to make Oblien the path of
least resistance for that runtime, on merit.

**Open-core, done honestly.** The generic / bring-your-own path stays real: MindWire runs anywhere
(local dir, Docker, any microVM). "No lock-in" is a *feature that sells the interface*, not a
liability. Oblien wins production by being genuinely better for it (Section 4), never by crippling
the generic path. This keeps the wedge credible — which is what makes the pull-through work.

---

## 3. The funnel (pull-through motion)

1. **Adopt free.** Dev installs MindWire, runs agents locally (`sandbox: 'local'`, today's behavior).
2. **Hit the wall.** Ships to prod → *"where do agents run safely, at scale, per user?"*
3. **Flip one line.** `sandbox: 'oblien'` → metered microVMs, no infra to babysit.
4. **Compound.** Multi-tenant SaaS builders use Oblien namespaces + scoped tokens + billing API to
   give *their* users isolated agent workspaces. Oblien becomes infrastructure-of-infrastructure;
   usage grows with the customer's own growth.

Conversion triggers to instrument later: first parallel run, first multi-tenant deployment, first
"expose the agent's output as a URL," first request to resume/fork a session.

---

## 4. Why Oblien wins production — feature → need (each maps to a real Oblien capability)

Source: `oblien.com/llms.txt`. This table is the spine of the marketing page **and** the provider's
declared capability matrix (Section 5) — the page should render from the code, so the claim is true.

| Production agent need | Oblien capability |
|---|---|
| Run agent-generated untrusted code safely, per tenant | Firecracker microVMs — hardware isolation + network firewall + token auth |
| Fan out N agents per turn without warm-pool cost | boot in milliseconds |
| Durable / resumable / forkable agent sessions | Snapshots & Archives (save/restore workspace state) |
| One isolated workspace per end-user (multi-tenant SaaS) | Namespaces + Scoped JWTs (short-lived, namespace/workspace-scoped) |
| Meter & bill agent usage per tenant without building it | Resource Pools / quotas + Billing API (sell plans, entitle credits — no Stripe) |
| Agent builds something → expose it to the user instantly | Preview URLs, Edge Tunnel, Edge Proxy, Routes, Pages |
| Reach the agent's runtime directly | Runtime API/MCP (files, search, terminal, command exec, file watcher) |
| Observe production agent workloads | Analytics, Metrics & Stats (SSE), Webhooks, Usage & Activity |

Not claimed (not in the source): specific prices, regions, or CPU/mem/disk numbers. Don't invent them
on the page.

---

## 5. Product architecture — the sandbox / runtime provider layer

**Committed direction (author-approved): mirror the agent adapter registry.** This is what turns the
pitch from marketing into product — `sandbox: 'oblien'` becomes a literal one-line switch, and the
capability matrix above is generated from code rather than hand-written.

### 5.1 Shape (mirror of `internal/agent`)

- **`sandbox.Provider` interface** + **`sandbox.All()` registry**, exactly paralleling `agent.All()`
  and the adapter pattern in `daemon/internal/agent/`.
- **`register.go` blank-imports** the built-in providers so the registry is populated at import —
  same mechanism as the agent/notify factories wired in `daemon/cmd/daemon/main.go` and the SDK's
  `register.go`.
- **Providers:**
  - `local` — the default; run the turn in the working directory exactly as today (zero behavior
    change when unset — critical for backward compat).
  - `oblien` — the reference runtime: provision or attach an Oblien microVM, run the agent's turn
    *inside* it, stream unified events back out.
  - **BYO** — third parties can `sandbox.Register(...)` their own (Docker, E2B, Modal, a private
    cloud). This is the honest "generic listing"; it also seeds an ecosystem.

### 5.2 Declared capabilities (mirror `agent.Capabilities`)

Each provider **declares** what it supports, so clients/SDK read it as hints and the core switches on
the few that are behavioral:

- `Persistence` (snapshots/archives), `ScopedTokens` (per-tenant), `PreviewURLs`/networking,
  `ResourceControl` (CPU/mem/disk), `ParallelBoot`, `Regions`, `Metering`.
- Same `none | native | emulated` discipline where it makes sense (`local` = mostly `none`/host,
  `oblien` = `native` across the board). An unsupported request returns an **honest error**, never a
  silent drop — the same contract the agent layer already enforces (the 400 on an unsupported turn
  option).

### 5.3 Secrets discipline (LOCKED — do not weaken)

Provider credentials (Oblien API key / scoped token) flow **only** through a provider auth module —
the exact analogue of `agent.AuthModule.EnvForRun()`. They must **never** pass through
`TurnInput.Config`, `Options`, `Inbound`, or the shell string. The runtime injects them internally,
just as agent secrets are injected today. This is the same invariant that governs the agent layer and
it carries over verbatim.

### 5.4 Execution model

A turn on a remote provider: `provider.Acquire(ctx, spec)` → get a workspace handle (fresh, or
attach an existing one for resume) → run the agent's turn inside it → stream unified events back →
release/snapshot/destroy per policy. The existing `internal/driver` + `internal/runner` compose
underneath; the provider wraps *where* the process runs, orthogonal to *how the agent is driven*
(CLI/HTTP/persistent). The core keeps reading unified events and never branches on the provider.

### 5.5 Oblien-specific deep hooks (the "end-to-end control")

Because Oblien owns the whole VM lifecycle, the `oblien` provider can do what a generic container
can't:

- **Snapshot ⇄ session resume/fork** — pause a workspace after a turn, restore or fork it for the
  next; instant retries and branch-and-compare.
- **Scoped token ⇄ per-tenant isolation** — one workspace per end-user, JWT-scoped; multi-tenant
  SaaS with no shared state.
- **Preview URL / edge tunnel ⇄ agent output** — the agent builds something, MindWire hands back a
  live URL.
- **Billing API ⇄ metering** — meter agent runs per tenant and bill without touching Stripe.

### 5.6 Wire impact (watch this when we build)

Adding a `sandbox` option is a **deliberate, additive wire change** to `openapi.json` / `types.ts`
when Phase 2 lands — it must keep `TestOpenAPIRouteParity` and catalog freshness **green**, and keep
the Go module path `github.com/oblien/mindwire/daemon`. For this doc (Phase 0) there is **no** wire
change. The SDK surface mirrors the agent option: a client-level default + per-turn override
(`WithSandbox(...)`), with the provider capability matrix exposed the same way agent capabilities are.

---

## 6. Commercial model

- **MindWire:** free / open. Adoption engine. No revenue target of its own.
- **Oblien:** usage-metered — compute-seconds, storage, egress/bandwidth (align to Oblien's existing
  Billing & Credits + Plans & Limits). This is the revenue.
- **Land-and-expand:** free local dev → metered prod → multi-tenant resale (compounds with the
  customer's growth).
- **Enterprise:** dedicated resource pools, BYO-cloud, SSO, SLA, private regions.
- **Pricing handoff:** the existing `apps/web/app/pricing` page should route MindWire tiers into
  Oblien usage — MindWire "free/pro/enterprise" tiers describe the interface + support; the metered
  spend is Oblien. (Exact dimensions = open question, Section 9.)

---

## 7. Marketing page + docs plan (deferred to build phase, captured here)

Keep the locked monochrome/square system; the current [sandboxing page](apps/web/app/sandboxing/page.tsx)
is a solid v1 base. Planned changes when we build:

- **Two tiers:** *"Run anywhere"* (generic/BYO listing — no lock-in) and *"Oblien for production"*
  (the Section 4 feature→need block).
- **Concrete code moment:** the one-line `sandbox: 'oblien'` switch in **TS / Go / REST** tabs,
  matching the tri-surface docs we just shipped.
- **Production proof points:** multi-tenant (scoped tokens), metering (billing API), resume
  (snapshots), preview URLs.
- **CTA:** "Start free → scale on Oblien" + enterprise demo.
- **New docs pages** (in `apps/web/content/docs`, the single source): *Concept: Runtimes / Sandboxes*
  and *Guide: Running on Oblien* (tri-tab). Cross-linked from the page.

---

## 8. Phasing

- **Phase 0 — this doc.** Align on thesis, funnel, architecture, pricing shape. *(now)*
- **Phase 1 — narrative.** Restructure the sandboxing page + add the two docs pages. Cheap, ships
  the story, no daemon changes.
- **Phase 2 — provider layer.** `sandbox.Provider` + registry + `local` + `oblien` in daemon & SDK
  (additive wire change, parity green). Makes `sandbox: 'oblien'` real.
- **Phase 3 — commercialize.** Metering hooks, per-tenant scoped-token recipe, billing integration,
  dashboard surfacing.

---

## 9. Open questions (your call)

1. **Metering dimensions** — compute-seconds + storage + egress? Or a simpler "agent-run" credit?
   Should MindWire show cost estimates, or stay silent and let Oblien meter?
2. **`local` stays the default** (zero-config, no behavior change when `sandbox` unset) — confirm.
3. **BYO providers** — do we actively support third-party `sandbox.Register` in v1 (docs + a stable
   interface), or keep the interface internal and ship only `local` + `oblien` first?
4. **Pricing-page handoff** — how explicit is the MindWire→Oblien tier mapping on
   `apps/web/app/pricing`? Co-brand, or keep MindWire pricing about the interface and link out?
5. **Enterprise packaging** — is BYO-cloud / dedicated pools a near-term SKU or a "contact us" for now?
6. **Naming** — is the layer called *Sandbox*, *Runtime*, or *Workspace* in the public API? Oblien
   calls them "Workspaces"; MindWire's page says "sandbox." Pick one word and use it everywhere.

---

## Appendix — design DNA reference

The provider layer deliberately reuses the agent layer's proven patterns:

- Capability matrix as declared truth: [capabilities.go](daemon/internal/agent/capabilities.go)
- Unified vs custom (declared, never open passthrough): [schema.go](daemon/internal/agent/schema.go)
- Secrets only via an auth module's env injection: `agent.AuthModule.EnvForRun()`
- Blank-import registry population: `daemon/cmd/daemon/main.go`, SDK `register.go`
