import Link from "next/link";
import { Boxes, Repeat, Radio, HardDrive, Braces, Terminal, Server, Gauge, Layers } from "lucide-react";
import WireDiagram from "@/components/WireDiagram";
import Corners from "@/components/Corners";
import { consoleUrl } from "@/lib/console-url";

// Real brand marks in the brand's own colors (Claude's coral, Codex's indigo gradient) — matching the
// console/login `AgentIcon`, shown on white tiles like the hero wire diagram. `logo` is the file under
// /public/logos. Grok, Copilot and opencode ship no chromatic mark, so they render as their true
// monochrome logo (black on the white tile) in either theme.
const agents = [
  { name: "Claude Code", slug: "claude", logo: "claude-color" },
  { name: "Codex", slug: "codex", logo: "codex-color" },
  { name: "Grok Build", slug: "grok", logo: "grok" },
  { name: "GitHub Copilot", slug: "copilot", logo: "copilot", soon: true },
  { name: "opencode", slug: "opencode", logo: "opencode" },
];

const events = ["text", "thinking", "tool_use", "tool_result", "result"];

const features = [
  {
    icon: Boxes,
    title: "Wrapping a CLI is the easy half.",
    body: "Auth, models, memory, prompts, subagents, MCP, approvals \u2014 all of it reduces to one protocol, one auth flow, one typed event stream, the same behind Claude Code, Codex, or opencode.",
    chips: true,
  },
  {
    icon: Repeat,
    title: "Swap the agent like a model.",
    body: "Change one string to switch which agent runs. Each agent publishes its capability matrix, and a prompt, MCP, or subagent override it can't honor returns a 400 with the reason, not a silent drop.",
  },
  {
    icon: Radio,
    title: "Turns outlive their client.",
    body: "A turn runs on the runtime, not on your request. Kill the client, lose the network, redeploy your UI, then reconnect: the stream replays what you missed and continues live.",
  },
  {
    icon: HardDrive,
    title: "It runs where the code is.",
    body: "The runtime spawns the agent on your own machine, in your real working tree, with your own credentials. Nothing to provision. One option moves the same run to a box, a container, or a microVM.",
  },
  {
    icon: Braces,
    title: "TypeScript, Go, or plain HTTP.",
    body: "Call the TypeScript SDK, embed the Go SDK in-process, or drive raw HTTP and SSE from any language. Same protocol under all three. Apache-2.0, and yours to self-host.",
  },
  {
    icon: Terminal,
    title: "One history with your terminal.",
    body: "Claude Code and Codex keep their own transcripts, and the runtime reads them \u2014 so a session you ran in your own terminal shows up in your product, and back again.",
  },
];

// The console section. Claims are scoped to what apps/console verifiably does today: a live fleet,
// live spend, and capability-driven panels. Deliberately NOT claiming durable metrics history \u2014
// fleet and usage state is in-process, so this stays "live", never "history".
const consoleCells = [
  {
    icon: Server,
    title: "The fleet",
    body: "Every runtime you run \u2014 a remote URL, a box over SSH, a container, a microVM \u2014 added, activated and torn down from one view.",
  },
  {
    icon: Gauge,
    title: "What it is doing",
    body: "Turns, tokens and cost per agent per runtime, rolled up fleet-wide, with live CPU and memory for each turn in flight.",
  },
  {
    icon: Layers,
    title: "Every surface",
    body: "Auth, models, MCP, memory, prompts, subagents, providers, notifications \u2014 panels follow what each runtime reports it can do.",
  },
];

const heading = "text-neutral-950 dark:text-white font-semibold tracking-tight";

export default function Home() {
  return (
    <>
      {/* ===== hero ===== */}
      <section className="mx-auto max-w-6xl px-4 pt-14 sm:px-6 sm:pt-20">
        <div className="relative">
          <Corners />
          <div className="bento lg:grid-cols-5">
            {/* headline cell */}
            <div className="cell flex flex-col justify-center p-8 sm:p-12 lg:col-span-3">
              <span className="eyebrow">
                <span className="inline-block h-1.5 w-1.5 bg-neutral-400 dark:bg-neutral-500" />
                Stop building the agent
              </span>
              <h1 className={`mt-6 text-5xl sm:text-6xl lg:text-[4.75rem] lg:leading-[0.95] ${heading}`}>
                Wire in the<br className="hidden sm:block" /> best ones.
              </h1>
              <p className="mt-6 max-w-lg text-lg leading-relaxed text-neutral-600 dark:text-neutral-400">
                The world&rsquo;s best coding agents already do the hard part. MindWire is the
                runtime that drives them inside your product &mdash; on your own machine, against
                your real working tree, behind one protocol you can swap agents under.
              </p>
              <div className="mt-8 flex flex-wrap gap-3">
                <a href={consoleUrl} className="btn btn-primary">Start building</a>
                <a href={`${consoleUrl}/docs`} rel="noopener" className="btn btn-ghost">
                  See Docs
                </a>
              </div>
            </div>

            {/* the wire: agents → MindWire → your app */}
            <div className="cell flex items-center justify-center p-6 sm:p-8 lg:col-span-2">
              <WireDiagram />
            </div>
          </div>
        </div>

        {/* agent logo cells */}
        <div className="relative mt-4">
          <Corners />
          <div className="bento grid-cols-2 sm:grid-cols-5">
            {agents.map((a) => (
              <div
                key={a.slug}
                className={`cell flex items-center justify-center gap-2.5 px-3 py-7 text-neutral-600 transition-colors hover:text-neutral-950 dark:text-neutral-300 dark:hover:text-white${a.soon ? " opacity-60" : ""}`}
              >
                <span className="grid size-7 shrink-0 place-items-center rounded-[6px] bg-white ring-1 ring-black/10">
                  <span
                    aria-hidden
                    className="size-[18px]"
                    style={{
                      backgroundImage: `url(/logos/${a.logo}.svg)`,
                      backgroundSize: "contain",
                      backgroundPosition: "center",
                      backgroundRepeat: "no-repeat",
                    }}
                  />
                </span>
                <span className="font-mono text-[13px] tracking-tight">{a.name}</span>
                {a.soon && (
                  <span className="font-mono text-[9px] uppercase tracking-wider text-neutral-400 dark:text-neutral-600">
                    soon
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ===== how it works ===== */}
      <section id="how" className="mx-auto max-w-6xl px-4 pt-20 sm:px-6 sm:pt-28">
        <div className="mb-8 flex items-end justify-between gap-6">
          <div className="max-w-2xl">
            <span className="eyebrow">How it works</span>
            <h2 className={`mt-4 text-3xl sm:text-4xl ${heading}`}>Built for production, not demos.</h2>
          </div>
          <span className="hidden font-mono text-xs text-neutral-400 dark:text-neutral-600 sm:block">[ 01 — 06 ]</span>
        </div>
        <div className="relative">
          <Corners />
          <div className="bento sm:grid-cols-2">
            {features.map((f, i) => {
              const Icon = f.icon;
              return (
                <div key={f.title} className="cell p-8">
                  <div className="flex items-center justify-between">
                    <div className="inline-grid h-10 w-10 place-items-center border border-neutral-200 text-neutral-700 dark:border-white/10 dark:text-neutral-200">
                      <Icon size={17} strokeWidth={1.5} />
                    </div>
                    <span className="font-mono text-xs text-neutral-300 dark:text-white/20">
                      {String(i + 1).padStart(2, "0")}
                    </span>
                  </div>
                  <h3 className={`mt-5 text-xl ${heading}`}>{f.title}</h3>
                  <p className="mt-2 text-neutral-600 dark:text-neutral-400">{f.body}</p>
                  {f.chips && (
                    <div className="mt-5 flex flex-wrap gap-2">
                      {events.map((e) => (
                        <span key={e} className="chip">{e}</span>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </section>

      {/* ===== the console ===== */}
      <section id="console" className="mx-auto max-w-6xl px-4 pt-20 sm:px-6 sm:pt-28">
        <div className="mb-8 max-w-2xl">
          <span className="eyebrow">The console</span>
          <h2 className={`mt-4 text-3xl sm:text-4xl ${heading}`}>Bring the machine. Watch the agents.</h2>
          <p className="mt-4 text-lg leading-relaxed text-neutral-600 dark:text-neutral-400">
            A console ships with the runtime &mdash; self-hosted, multi-user, Apache-2.0, free. Point it
            at your own server and it runs the fleet: what is up, what it is spending, and every
            surface behind each agent.
          </p>
        </div>
        <div className="relative mb-4">
          <Corners />
          <div className="bento">
            <div className="cell p-3 sm:p-4">
              {/* Real capture of the console at 1600x700, one per theme — exactly one is in the DOM
                  (and the a11y tree) at a time, so both carry the same alt. */}
              <div className="border border-neutral-200 dark:border-white/10">
                <img
                  src="/console-light.png"
                  alt="The MindWire console: a fleet of runtimes, live turn, token and cost tiles, and a sidebar of every agent surface."
                  width={2400}
                  height={1050}
                  className="block w-full dark:hidden"
                />
                <img
                  src="/console-dark.png"
                  alt="The MindWire console: a fleet of runtimes, live turn, token and cost tiles, and a sidebar of every agent surface."
                  width={2400}
                  height={1050}
                  className="hidden w-full dark:block"
                />
              </div>
            </div>
          </div>
        </div>
        <div className="relative">
          <Corners />
          <div className="bento sm:grid-cols-3">
            {consoleCells.map((c) => {
              const Icon = c.icon;
              return (
                <div key={c.title} className="cell p-8">
                  <div className="inline-grid h-10 w-10 place-items-center border border-neutral-200 text-neutral-700 dark:border-white/10 dark:text-neutral-200">
                    <Icon size={17} strokeWidth={1.5} />
                  </div>
                  <h3 className={`mt-5 text-lg ${heading}`}>{c.title}</h3>
                  <p className="mt-2 text-neutral-600 dark:text-neutral-400">{c.body}</p>
                </div>
              );
            })}
          </div>
        </div>
        <p className="mt-6 font-mono text-xs uppercase tracking-[0.16em] text-neutral-500 dark:text-neutral-500">
          Your machine while you build &middot; your server when you ship &middot; ours when you&rsquo;d rather not run one
        </p>
      </section>

      {/* ===== final cta ===== */}
      <section className="mx-auto max-w-6xl px-4 pt-20 sm:px-6 sm:pt-28">
        <div className="relative">
          <Corners />
          <div className="bento">
            <div className="cell flex flex-col items-center gap-5 px-6 py-16 text-center sm:py-24">
              <h2 className={`max-w-2xl text-3xl sm:text-[2.75rem] sm:leading-[1.05] ${heading}`}>
                Power your product with agents you didn&rsquo;t have to build.
              </h2>
              <p className="max-w-md text-neutral-500 dark:text-neutral-400">
                The world&rsquo;s best coding agents, driven by one runtime &mdash; on your machine
                while you build, on a box, a container, or a microVM when you ship.
              </p>
              <div className="mt-1 flex flex-wrap justify-center gap-3">
                <a href={consoleUrl} className="btn btn-primary">Start building</a>
                <Link href="/docs" className="btn btn-ghost">Read the docs</Link>
              </div>
            </div>
          </div>
        </div>
      </section>
    </>
  );
}
