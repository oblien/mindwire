// The landing's signature visual, ported to the console's ink tokens: the supported agents wire down
// into MindWire, which wires down into your app. Agent tiles reuse the app's own <AgentIcon> brand marks
// (Claude coral, Codex gradient, opencode mono) — the one sanctioned color exception; everything else is
// monochrome `currentColor` over the canvas, sharp-cornered to match the design. Wires pulse via SMIL, no
// JS. `WIRE_AGENTS` is exported so the footer strip names the same set.
import { AgentIcon } from "@/components/AgentIcon";

export const WIRE_AGENTS = [
  { id: "claude-code", label: "Claude Code", cx: 47 },
  { id: "codex", label: "Codex", cx: 140 },
  { id: "opencode", label: "opencode", cx: 233 },
];

export function WireDiagram({ className }: { className?: string }) {
  return (
    <div className={`mx-auto w-full max-w-[280px] text-foreground ${className ?? ""}`}>
      {/* agent tiles + labels */}
      <div className="grid grid-cols-3">
        {WIRE_AGENTS.map((a) => (
          <div key={a.id} className="flex flex-col items-center gap-2">
            <span className="grid size-11 place-items-center border border-border bg-ink/[0.03]">
              <AgentIcon agentId={a.id} className="size-5" />
            </span>
            <span className="font-mono text-[10px] tracking-tight text-muted-foreground">
              {a.label}
            </span>
          </div>
        ))}
      </div>

      {/* converging wires with travelling pulses */}
      <svg viewBox="0 0 280 60" className="w-full" aria-hidden>
        <defs>
          {WIRE_AGENTS.map((a, i) => (
            <path key={i} id={`w${i}`} d={`M${a.cx} 0 C ${a.cx} 34 140 26 140 58`} fill="none" />
          ))}
        </defs>
        <g stroke="currentColor" strokeOpacity="0.22" strokeWidth="1.5" fill="none">
          {WIRE_AGENTS.map((_, i) => (
            <use key={i} href={`#w${i}`} />
          ))}
        </g>
        <circle cx="140" cy="58" r="2.5" fill="currentColor" fillOpacity="0.35" />
        <g fill="currentColor">
          {WIRE_AGENTS.map((_, i) => (
            <circle key={i} r="3" opacity="0">
              <animateMotion dur="2.6s" begin={`${i * 0.4}s`} repeatCount="indefinite">
                <mpath href={`#w${i}`} />
              </animateMotion>
              <animate
                attributeName="opacity"
                dur="2.6s"
                begin={`${i * 0.4}s`}
                repeatCount="indefinite"
                values="0;1;1;0"
                keyTimes="0;0.15;0.85;1"
              />
            </circle>
          ))}
        </g>
      </svg>

      {/* MindWire hub */}
      <div className="border border-ink/50 bg-ink/[0.06] py-2.5 text-center text-sm font-semibold tracking-tight">
        MindWire
      </div>

      {/* hub → your app, with a downward pulse */}
      <svg viewBox="0 0 4 30" className="mx-auto block h-[30px] w-1" aria-hidden>
        <line x1="2" y1="0" x2="2" y2="30" stroke="currentColor" strokeOpacity="0.22" strokeWidth="1.5" />
        <circle r="2.5" fill="currentColor" opacity="0">
          <animateMotion dur="1.6s" repeatCount="indefinite" path="M2 0 L2 30" />
          <animate
            attributeName="opacity"
            dur="1.6s"
            repeatCount="indefinite"
            values="0;1;1;0"
            keyTimes="0;0.2;0.8;1"
          />
        </circle>
      </svg>

      {/* your app */}
      <div className="mx-auto w-max border border-border px-6 py-2 text-center text-xs text-muted-foreground">
        Your app
      </div>
    </div>
  );
}
