// Animated flow: your product → MindWire → isolated sandboxes, each running an agent.
// Pure SVG/SMIL, monochrome via currentColor.
const sandboxes = [
  { agent: "claude-code", cy: 60 },
  { agent: "codex", cy: 180 },
  { agent: "grok", cy: 300 },
];

export default function SandboxViz() {
  return (
    <svg
      viewBox="0 0 920 360"
      role="img"
      aria-label="Your product calls MindWire, which runs each agent in its own isolated sandbox"
      className="h-auto w-full text-neutral-800 dark:text-neutral-200"
    >
      <defs>
        <path id="sb-wp" d="M156 180 L246 180" fill="none" />
        {sandboxes.map((s, i) => (
          <path key={i} id={`sb-w${i}`} d={`M386 180 C 466 180 468 ${s.cy} 548 ${s.cy}`} fill="none" />
        ))}
      </defs>

      {/* wires */}
      <g stroke="currentColor" strokeOpacity="0.2" strokeWidth="1.5" fill="none">
        <use href="#sb-wp" />
        {sandboxes.map((s, i) => (
          <use key={i} href={`#sb-w${i}`} />
        ))}
      </g>

      {/* travelling pulses */}
      <g fill="currentColor">
        <circle r="3.5">
          <animateMotion dur="1.4s" repeatCount="indefinite">
            <mpath href="#sb-wp" />
          </animateMotion>
        </circle>
        {sandboxes.map((s, i) => (
          <circle key={i} r="3.5" opacity="0">
            <animateMotion dur="2.2s" begin={`${i * 0.4}s`} repeatCount="indefinite">
              <mpath href={`#sb-w${i}`} />
            </animateMotion>
            <animate attributeName="opacity" dur="2.2s" begin={`${i * 0.4}s`} repeatCount="indefinite" values="0;1;1;0" keyTimes="0;0.15;0.85;1" />
          </circle>
        ))}
      </g>

      {/* your product */}
      <g>
        <rect x="24" y="150" width="132" height="60" fill="none" stroke="currentColor" strokeOpacity="0.5" />
        <text x="90" y="184" textAnchor="middle" fontSize="14" fill="currentColor" fillOpacity="0.9" className="font-sans">
          Your product
        </text>
      </g>

      {/* MindWire */}
      <g>
        <rect x="246" y="150" width="140" height="60" fill="currentColor" fillOpacity="0.06" stroke="currentColor" strokeOpacity="0.65" />
        <text x="316" y="185" textAnchor="middle" fontSize="17" fontWeight="600" fill="currentColor" className="font-sans">
          MindWire
        </text>
      </g>

      {/* sandboxes */}
      {sandboxes.map((s, i) => (
        <g key={i}>
          <rect x="548" y={s.cy - 39} width="348" height="78" fill="currentColor" fillOpacity="0.03" stroke="currentColor" strokeOpacity="0.4" />
          <circle cx="566" cy={s.cy - 15} r="3.5" fill="currentColor">
            <animate attributeName="opacity" values="1;0.3;1" dur="1.9s" begin={`${i * 0.35}s`} repeatCount="indefinite" />
          </circle>
          <text x="580" y={s.cy - 11} fontSize="10.5" letterSpacing="1.2" fill="currentColor" fillOpacity="0.5" className="font-mono">
            SANDBOX
          </text>
          <text x="880" y={s.cy - 11} textAnchor="end" fontSize="10.5" fill="currentColor" fillOpacity="0.5" className="font-mono">
            microVM
          </text>
          <text x="566" y={s.cy + 15} fontSize="15" fill="currentColor" className="font-mono">
            {s.agent}
          </text>
          <text x="880" y={s.cy + 15} textAnchor="end" fontSize="10.5" fill="currentColor" fillOpacity="0.45" className="font-mono">
            root · net · fs
          </text>
        </g>
      ))}

      {/* captions */}
      <g className="font-mono" fill="currentColor" fillOpacity="0.4" fontSize="10.5" letterSpacing="1.4">
        <text x="90" y="352" textAnchor="middle">YOUR PRODUCT</text>
        <text x="316" y="352" textAnchor="middle">ONE RUNTIME</text>
        <text x="722" y="352" textAnchor="middle">ISOLATED SANDBOXES</text>
      </g>
    </svg>
  );
}
