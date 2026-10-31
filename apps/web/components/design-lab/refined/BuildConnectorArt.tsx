/** An original study of three surfaces becoming one interface. */
export default function BuildConnectorArt() {
  return (
    <svg viewBox="0 0 180 180" fill="none" aria-hidden="true">
      <g
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path
          d="M24 130H132Q154 130 154 108V43"
          strokeDasharray="2 5"
          opacity=".45"
        />
        <path d="M26 32H42M34 24V40M138 152H154M146 144V160" opacity=".4" />
        <rect x="38" y="43" width="83" height="83" rx="20" fill="#e3d6e9" />
        <rect x="58" y="61" width="83" height="83" rx="20" fill="#e1e8d7" />
        <rect x="78" y="79" width="64" height="65" rx="17" fill="#f8f7f3" />
        <path d="M93 102H127M93 112H116M93 122H121" opacity=".6" />
        <circle cx="121" cy="43" r="4" fill="#292c27" stroke="none" />
        <circle cx="24" cy="130" r="3" fill="#292c27" stroke="none" />
        <circle cx="154" cy="43" r="4" fill="#f8f7f3" />
      </g>
    </svg>
  );
}
