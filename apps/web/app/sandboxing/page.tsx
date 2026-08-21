import type { Metadata } from "next";
import { Shield, Timer, KeyRound, Layers, Cpu, Terminal, Network, Activity, Server, Cloud, Plug } from "lucide-react";
import Corners from "@/components/Corners";
import SandboxViz from "@/components/SandboxViz";

export const metadata: Metadata = {
  title: "Sandboxing — MindWire",
  description:
    "Coding agents run untrusted shell, edits, and network calls — so give every run its own isolated machine. Connect infrastructure you already own, use a managed microVM, or bring your own provider.",
};

const heading = "text-neutral-950 dark:text-white font-semibold tracking-tight";

const reasons = [
  {
    icon: Shield,
    t: "It's untrusted code",
    d: "An agent runs shell commands, edits files, and hits the network on its own. That belongs in a hardware-isolated VM — not on your host or in prod.",
  },
  {
    icon: Timer,
    t: "Fresh per run",
    d: "A sandbox that boots in milliseconds gives each turn a clean, reproducible environment. Snapshot state, or throw it away when the run ends.",
  },
  {
    icon: KeyRound,
    t: "Safe for many tenants",
    d: "Scope each workspace to a namespace with short-lived tokens, so one user's run can never see another's files or secrets.",
  },
  {
    icon: Layers,
    t: "Built to scale out",
    d: "Real work means many agents at once. Run them in parallel behind resource pools and quotas instead of fighting over one machine.",
  },
];

const origins = [
  {
    icon: Server,
    t: "Your own infrastructure",
    d: "Connect a box over SSH or a Docker host you already run. MindWire deploys the runtime there and drives agents inside your network, on your own metal.",
  },
  {
    icon: Cloud,
    t: "Managed sandboxes",
    d: "Point MindWire at Oblien and every turn gets its own microVM \u2014 booted on demand, torn down after, metered. Nothing to provision or babysit.",
  },
  {
    icon: Plug,
    t: "Third-party providers",
    d: "A target is two primitives \u2014 run a command, put a file \u2014 so any exec-capable backend composes. Adapters for other sandbox platforms are next.",
    soon: true,
  },
];

const platform = [
  {
    icon: Shield,
    t: "Isolation & security",
    d: "Firecracker microVMs + Docker images. Hardware isolation, a network firewall, and token auth — designed for untrusted agent code.",
  },
  {
    icon: Cpu,
    t: "Lifecycle & compute",
    d: "Create, snapshot, restore, and destroy workspaces. Adjustable CPU / memory / disk, TTLs, and restart policies.",
  },
  {
    icon: Terminal,
    t: "Runtime & filesystem",
    d: "File ops, command execution, interactive terminals, ripgrep search, and a real-time file watcher — over API or MCP.",
  },
  {
    icon: Network,
    t: "Networking & edge",
    d: "Firewalls, private links, preview URLs, custom domains, SSH, plus edge proxy and tunnels to reach loopback services.",
  },
  {
    icon: Layers,
    t: "Scale & multi-tenancy",
    d: "Namespaces, resource pools, quotas, and scoped JWTs — run many agents in parallel, isolated per tenant.",
  },
  {
    icon: Activity,
    t: "Observability",
    d: "Traffic analytics, request logs, live stats, usage and credit tracking, and webhooks.",
  },
];

export default function Sandboxing() {
  return (
    <>
      {/* header */}
      <section className="mx-auto max-w-6xl px-4 pb-4 pt-16 sm:px-6 sm:pt-24">
        <div className="max-w-2xl">
          <span className="eyebrow">Sandboxing</span>
          <h1 className={`mt-5 text-4xl sm:text-5xl ${heading}`}>
            The right place to run an agent is a sandbox.
          </h1>
          <p className="mt-5 text-lg leading-relaxed text-neutral-600 dark:text-neutral-400">
            The moment an agent runs real work, it&rsquo;s executing untrusted code on your behalf.
            Give every run its own isolated machine &mdash; on infrastructure you already own, or on a
            managed microVM from{" "}
            <a href="https://oblien.com" rel="noopener" className="text-neutral-950 underline underline-offset-4 dark:text-white">
              Oblien
            </a>{" "}
            booted on demand and torn down after. MindWire drives the agent; you choose where it runs.
          </p>
        </div>
      </section>

      {/* flow visual */}
      <section className="mx-auto max-w-6xl px-4 pt-8 sm:px-6">
        <div className="relative">
          <Corners />
          <div className="bento">
            <div className="cell px-6 py-10 sm:px-12 sm:py-14">
              <SandboxViz />
            </div>
          </div>
        </div>
      </section>

      {/* why a sandbox */}
      <section className="mx-auto max-w-6xl px-4 pt-20 sm:px-6 sm:pt-28">
        <div className="mb-8 max-w-2xl">
          <span className="eyebrow">Why a sandbox</span>
          <h2 className={`mt-4 text-3xl sm:text-4xl ${heading}`}>Isolate the run, not your nerves.</h2>
        </div>
        <div className="relative">
          <Corners />
          <div className="bento sm:grid-cols-2">
            {reasons.map((r) => {
              const Icon = r.icon;
              return (
                <div key={r.t} className="cell p-8">
                  <div className="inline-grid h-10 w-10 place-items-center border border-neutral-200 text-neutral-700 dark:border-white/10 dark:text-neutral-200">
                    <Icon size={17} strokeWidth={1.5} />
                  </div>
                  <h3 className={`mt-5 text-lg ${heading}`}>{r.t}</h3>
                  <p className="mt-2 text-neutral-600 dark:text-neutral-400">{r.d}</p>
                </div>
              );
            })}
          </div>
        </div>
      </section>

      {/* where the sandbox comes from */}
      <section className="mx-auto max-w-6xl px-4 pt-20 sm:px-6 sm:pt-28">
        <div className="mb-8 max-w-2xl">
          <span className="eyebrow">Where it comes from</span>
          <h2 className={`mt-4 text-3xl sm:text-4xl ${heading}`}>Your infrastructure, or ours.</h2>
          <p className="mt-4 text-lg leading-relaxed text-neutral-600 dark:text-neutral-400">
            A sandbox is a destination, not a dependency. Connect machines you already run, hand the
            machine to someone else, or bring the provider you already pay for.
          </p>
        </div>
        <div className="relative">
          <Corners />
          <div className="bento sm:grid-cols-3">
            {origins.map((r) => {
              const Icon = r.icon;
              return (
                <div key={r.t} className="cell p-8">
                  <div className="inline-grid h-10 w-10 place-items-center border border-neutral-200 text-neutral-700 dark:border-white/10 dark:text-neutral-200">
                    <Icon size={17} strokeWidth={1.5} />
                  </div>
                  <div className="mt-5 flex items-center gap-2">
                    <h3 className={`text-lg ${heading}`}>{r.t}</h3>
                    {r.soon && (
                      <span className="font-mono text-[9px] uppercase tracking-wider text-neutral-400 dark:text-neutral-600">
                        soon
                      </span>
                    )}
                  </div>
                  <p className="mt-2 text-neutral-600 dark:text-neutral-400">{r.d}</p>
                </div>
              );
            })}
          </div>
        </div>
      </section>

      {/* oblien platform */}
      <section className="mx-auto max-w-6xl px-4 pt-20 sm:px-6 sm:pt-28">
        <div className="mb-8 flex flex-wrap items-end justify-between gap-4">
          <div className="max-w-2xl">
            <span className="eyebrow">The managed option</span>
            <h2 className={`mt-4 text-3xl sm:text-4xl ${heading}`}>The machine, handled for you.</h2>
          </div>
          <span className="font-mono text-xs text-neutral-400 dark:text-neutral-600">
            REST · CLI · MCP · dashboard
          </span>
        </div>
        <div className="relative">
          <Corners />
          <div className="bento sm:grid-cols-2 lg:grid-cols-3">
            {platform.map((p) => {
              const Icon = p.icon;
              return (
                <div key={p.t} className="cell p-8">
                  <div className="inline-grid h-9 w-9 place-items-center border border-neutral-200 text-neutral-700 dark:border-white/10 dark:text-neutral-200">
                    <Icon size={16} strokeWidth={1.5} />
                  </div>
                  <h3 className={`mt-4 text-base ${heading}`}>{p.t}</h3>
                  <p className="mt-2 text-sm text-neutral-600 dark:text-neutral-400">{p.d}</p>
                </div>
              );
            })}
          </div>
        </div>
      </section>

      {/* cta */}
      <section className="mx-auto max-w-6xl px-4 py-20 sm:px-6 sm:py-28">
        <div className="relative">
          <Corners />
          <div className="bento">
            <div className="cell flex flex-col items-center gap-5 px-6 py-16 text-center sm:py-20">
              <h2 className={`max-w-2xl text-3xl sm:text-4xl ${heading}`}>
                Run your agents in sandboxes. Oblien scales them.
              </h2>
              <p className="max-w-lg text-neutral-500 dark:text-neutral-400">
                Point MindWire at Oblien and every turn gets its own microVM — parallel, isolated,
                metered. No infrastructure to babysit.
              </p>
              <div className="flex flex-wrap justify-center gap-3">
                <a
                  href="mailto:hello@oblien.com?subject=Oblien%20sandboxes%20for%20agents"
                  className="btn btn-primary"
                >
                  Request a demo
                </a>
                <a href="https://oblien.com" rel="noopener" className="btn btn-ghost">
                  Oblien docs ↗
                </a>
              </div>
            </div>
          </div>
        </div>
      </section>
    </>
  );
}
