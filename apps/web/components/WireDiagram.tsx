// Pure-SVG (SMIL) diagram, vertical flow: five coding agents wire down into MindWire, which
// wires down into your app. Agent logos ride on white "app-icon" tiles up top, in the brand's own
// colors where one exists (Claude's coral, Codex's indigo gradient — matching the console/login);
// Grok/Copilot/opencode ship no chromatic mark, so they render monochrome. Wires/hub use currentColor.
const agents = [
  { label: "Claude", cx: 44, logo: "/logos/claude-color.svg" },
  { label: "Codex", cx: 138, logo: "/logos/codex-color.svg" },
  { label: "Grok", cx: 232, logo: "/logos/grok.svg" },
  { label: "Copilot", cx: 326, logo: "/logos/copilot.svg" },
  { label: "opencode", cx: 420, logo: "/logos/opencode.svg" },
];

export default function WireDiagram() {
  return (
    <svg
      viewBox="0 0 464 424"
      role="img"
      aria-label="Five coding agents connect through MindWire to your app"
      className="h-auto w-full max-w-[430px] text-neutral-800 dark:text-neutral-200"
    >
      <defs>
        {agents.map((a, i) => (
          <path key={i} id={`wa${i}`} d={`M${a.cx} 56 C ${a.cx} 130 232 130 232 214`} fill="none" />
        ))}
        <path id="wapp" d="M232 268 L232 366" fill="none" />
      </defs>

      {/* wires */}
      <g stroke="currentColor" strokeOpacity="0.2" strokeWidth="1.5" fill="none">
        {agents.map((a, i) => (
          <use key={i} href={`#wa${i}`} />
        ))}
        <use href="#wapp" />
      </g>

      {/* junction dots */}
      <g fill="currentColor" fillOpacity="0.35">
        <circle cx="232" cy="214" r="2.5" />
        <circle cx="232" cy="268" r="2.5" />
      </g>

      {/* travelling pulses (agents → MindWire → app); opacity-gated */}
      <g fill="currentColor">
        {agents.map((a, i) => (
          <circle key={i} r="3.5" opacity="0">
            <animateMotion dur="2.8s" begin={`${i * 0.42}s`} repeatCount="indefinite">
              <mpath href={`#wa${i}`} />
            </animateMotion>
            <animate attributeName="opacity" dur="2.8s" begin={`${i * 0.42}s`} repeatCount="indefinite" values="0;1;1;0" keyTimes="0;0.15;0.85;1" />
          </circle>
        ))}
        <circle r="3.5" opacity="0">
          <animateMotion dur="1.5s" begin="0.2s" repeatCount="indefinite">
            <mpath href="#wapp" />
          </animateMotion>
          <animate attributeName="opacity" dur="1.5s" begin="0.2s" repeatCount="indefinite" values="0;1;1;0" keyTimes="0;0.2;0.8;1" />
        </circle>
      </g>

      {/* agent logo tiles + labels */}
      {agents.map((a, i) => (
        <g key={i}>
          <rect x={a.cx - 14} y="2" width="28" height="28" rx="6" fill="#ffffff" stroke="rgba(0,0,0,0.12)" strokeWidth="1" />
          <image href={a.logo} x={a.cx - 9} y="7" width="18" height="18" />
          <text x={a.cx} y="46" textAnchor="middle" fontSize="12" fill="currentColor" fillOpacity="0.8" className="font-mono">
            {a.label}
          </text>
        </g>
      ))}

      {/* MindWire hub */}
      <g>
        <rect x="157" y="214" width="150" height="54" fill="currentColor" fillOpacity="0.06" stroke="currentColor" strokeOpacity="0.65" />
        <text x="232" y="247" textAnchor="middle" fontSize="18" fontWeight="600" fill="currentColor" className="font-sans">
          MindWire
        </text>
      </g>

      {/* your app */}
      <g>
        <rect x="170" y="366" width="124" height="44" fill="none" stroke="currentColor" strokeOpacity="0.5" />
        <text x="232" y="393" textAnchor="middle" fontSize="14" fill="currentColor" fillOpacity="0.9" className="font-sans">
          Your app
        </text>
      </g>
    </svg>
  );
}
