/** Original vector illustrations for Mindwire's connected product story. */
export function KnowledgeArtwork() {
  return (
    <svg viewBox="0 0 400 240" fill="none" aria-hidden="true">
      <defs>
        <linearGradient
          id="knowledge-coral"
          x1="35"
          y1="40"
          x2="130"
          y2="230"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#f2ac7e" />
          <stop offset="1" stopColor="#e16c43" />
        </linearGradient>
        <linearGradient
          id="knowledge-blue"
          x1="255"
          y1="60"
          x2="360"
          y2="225"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#617edb" />
          <stop offset="1" stopColor="#3556b2" />
        </linearGradient>
        <filter
          id="knowledge-shadow"
          x="-35%"
          y="-30%"
          width="170%"
          height="180%"
        >
          <feDropShadow
            dx="0"
            dy="6"
            stdDeviation="5"
            floodColor="#22251d"
            floodOpacity=".13"
          />
        </filter>
      </defs>
      <g transform="rotate(-17 92 150)" filter="url(#knowledge-shadow)">
        <rect
          x="24"
          y="72"
          width="135"
          height="182"
          rx="12"
          fill="url(#knowledge-coral)"
        />
        <path d="M41 91H142" stroke="#772f21" strokeOpacity=".35" />
        <text x="41" y="115" fontSize="9" fill="#4c251d" letterSpacing="1">
          YOUR POINT
        </text>
        <text x="41" y="129" fontSize="9" fill="#4c251d" letterSpacing="1">
          OF VIEW.
        </text>
        <circle cx="91" cy="188" r="31" stroke="#fff1cc" strokeWidth="1.1" />
        <ellipse
          cx="91"
          cy="188"
          rx="15"
          ry="31"
          stroke="#fff1cc"
          strokeWidth="1.1"
        />
        <path
          d="M60 188H122M66 171H117M66 205H117"
          stroke="#fff1cc"
          strokeWidth="1.1"
        />
      </g>
      <g transform="rotate(-2 210 138)" filter="url(#knowledge-shadow)">
        <path
          d="M150 31H241L271 61V230Q271 242 259 242H150Q138 242 138 230V43Q138 31 150 31Z"
          fill="#fffef8"
        />
        <path d="M241 31V49Q241 61 253 61H271" fill="#e4e4da" />
        <text x="156" y="63" fontSize="7" fill="#77796c" letterSpacing="1">
          PRODUCT NOTES
        </text>
        <path
          d="M156 86H236M156 96H248M156 106H216"
          stroke="#262b24"
          strokeWidth="5"
        />
        <path
          d="M156 131H248M156 139H239M156 147H243M156 155H220M156 177H241M156 185H226"
          stroke="#b8bcb0"
          strokeWidth="2"
        />
        <rect x="156" y="206" width="39" height="7" rx="2" fill="#d7df9e" />
        <path d="M237 207V217M232 212H242" stroke="#676e4d" />
      </g>
      <g transform="rotate(13 322 165)" filter="url(#knowledge-shadow)">
        <rect
          x="263"
          y="82"
          width="125"
          height="168"
          rx="10"
          fill="url(#knowledge-blue)"
        />
        <path d="M275 82V250" stroke="#a1b7fc" strokeOpacity=".5" />
        <text x="288" y="107" fontSize="8" fill="#f3f4e9" letterSpacing="1.1">
          THE BIG
        </text>
        <text x="288" y="121" fontSize="8" fill="#f3f4e9" letterSpacing="1.1">
          PICTURE.
        </text>
        {[0, 1, 2, 3, 4].map((i) => (
          <path
            key={i}
            d={`M283 ${190 + i * 7}C306 ${128 + i * 7} 326 ${220 + i * 7} 359 ${152 + i * 7}C370 ${130 + i * 7} 379 ${144 + i * 7} 388 ${138 + i * 7}`}
            stroke="#d8e2ff"
            strokeOpacity={0.9 - i * 0.08}
          />
        ))}
      </g>
    </svg>
  );
}

export function ToolsArtwork() {
  return (
    <svg viewBox="0 0 400 240" fill="none" aria-hidden="true">
      <defs>
        <filter id="tools-shadow" x="-30%" y="-30%" width="160%" height="175%">
          <feDropShadow
            dx="0"
            dy="6"
            stdDeviation="5"
            floodColor="#22251d"
            floodOpacity=".12"
          />
        </filter>
      </defs>
      <g transform="rotate(-12 95 165)" filter="url(#tools-shadow)">
        <rect x="19" y="92" width="150" height="155" rx="14" fill="#e0e8a2" />
        <rect x="35" y="109" width="34" height="34" rx="10" fill="#f7f8dd" />
        <path
          d="M47 117L40 126L47 135M56 117L63 126L56 135"
          stroke="#596432"
          strokeWidth="1.5"
        />
        <path d="M54 116L49 136" stroke="#596432" />
        <text x="35" y="171" fontSize="16" fill="#343c23" letterSpacing="-.5">
          Your tools.
        </text>
        <text x="35" y="190" fontSize="16" fill="#343c23" letterSpacing="-.5">
          Real actions.
        </text>
        <path d="M35 215H135" stroke="#acb471" />
        <path d="M127 208L135 215L127 222" stroke="#606e38" />
      </g>
      <g transform="rotate(5 248 124)" filter="url(#tools-shadow)">
        <rect x="144" y="43" width="216" height="91" rx="13" fill="#fffef8" />
        <rect x="160" y="61" width="31" height="36" rx="6" fill="#e8ecf5" />
        <path
          d="M168 71H183M168 78H183M168 85H178"
          stroke="#5d72b2"
          strokeWidth="2"
        />
        <text x="204" y="77" fontSize="13" fill="#30352b">
          Create a document
        </text>
        <text x="204" y="95" fontSize="9" fill="#75796c">
          With the context that matters.
        </text>
        <path d="M161 117H341" stroke="#e2e5da" />
      </g>
      <g transform="rotate(-3 274 182)" filter="url(#tools-shadow)">
        <rect x="173" y="148" width="215" height="97" rx="13" fill="#d47551" />
        <circle cx="203" cy="178" r="14" fill="#f5d4b0" />
        <path d="M196 178L201 183L210 173" stroke="#925232" strokeWidth="2" />
        <text x="229" y="175" fontSize="12" fill="#fff7e9">
          Make it happen.
        </text>
        <text x="229" y="192" fontSize="9" fill="#f9ddc7">
          Your workflow, connected.
        </text>
        <path d="M193 218H369" stroke="#f4be9d" />
        <circle cx="358" cy="218" r="5" fill="#f6e8c7" />
      </g>
    </svg>
  );
}

export function InterfaceArtwork() {
  return (
    <svg viewBox="0 0 400 240" fill="none" aria-hidden="true">
      <defs>
        <linearGradient
          id="interface-orbit"
          x1="223"
          y1="101"
          x2="347"
          y2="222"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#e4e9ab" />
          <stop offset=".5" stopColor="#e5966e" />
          <stop offset="1" stopColor="#c8603d" />
        </linearGradient>
        <filter
          id="interface-shadow"
          x="-30%"
          y="-30%"
          width="160%"
          height="175%"
        >
          <feDropShadow
            dx="0"
            dy="5"
            stdDeviation="5"
            floodColor="#22251d"
            floodOpacity=".12"
          />
        </filter>
      </defs>
      <g transform="rotate(-7 205 148)" filter="url(#interface-shadow)">
        <rect x="47" y="40" width="313" height="212" rx="12" fill="#fcfcf5" />
        <path d="M47 65H360" stroke="#dfe1d7" />
        <circle cx="62" cy="53" r="2.5" fill="#bdc1b2" />
        <circle cx="72" cy="53" r="2.5" fill="#bdc1b2" />
        <circle cx="82" cy="53" r="2.5" fill="#bdc1b2" />
        <text x="185" y="56" fontSize="7" fill="#777b6c">
          your-product.app
        </text>
        <text
          x="68"
          y="92"
          fontSize="12"
          fontWeight="600"
          fill="#343b2d"
          letterSpacing="-.4"
        >
          your space.
        </text>
        <path d="M297 84H339M306 90H339" stroke="#a9af9d" />
        <rect x="64" y="110" width="279" height="125" rx="5" fill="#e7ebd8" />
        <text x="79" y="141" fontSize="23" fill="#343c2c" letterSpacing="-1">
          Make room
        </text>
        <text x="79" y="167" fontSize="23" fill="#343c2c" letterSpacing="-1">
          for what’s next.
        </text>
        <path d="M80 192H138M80 199H129" stroke="#848d75" strokeWidth="2" />
        <rect x="79" y="213" width="44" height="9" rx="3" fill="#3b4533" />
        <ellipse
          cx="299"
          cy="167"
          rx="35"
          ry="53"
          transform="rotate(29 299 167)"
          stroke="url(#interface-orbit)"
          strokeWidth="13"
        />
      </g>
      <g transform="rotate(5 294 193)" filter="url(#interface-shadow)">
        <rect x="237" y="175" width="145" height="57" rx="11" fill="#fdfcf7" />
        <rect x="248" y="188" width="28" height="30" rx="8" fill="#d9e2a2" />
        <path
          d="M254 208V199Q254 193 260 199V208M260 199Q260 193 266 199V208H271"
          stroke="#4e5d35"
          strokeWidth="2"
        />
        <text x="286" y="198" fontSize="9" fill="#414936">
          Your assistant
        </text>
        <path d="M286 209H366" stroke="#b4bba9" strokeWidth="2" />
      </g>
    </svg>
  );
}

export function MindwireMark({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 120 95"
      fill="none"
      aria-hidden="true"
    >
      <path
        d="M10 82V36C10 4 53 4 53 36V82M53 36C53 4 96 4 96 36V82H113"
        stroke="currentColor"
        strokeWidth="17"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function OrbitArtwork() {
  return (
    <svg viewBox="0 0 220 220" fill="none" aria-hidden="true">
      {[0, 30, 60, 90, 120, 150].map((angle) => (
        <ellipse
          key={angle}
          cx="110"
          cy="110"
          rx="40"
          ry="96"
          transform={`rotate(${angle} 110 110)`}
          stroke="currentColor"
          strokeWidth=".8"
        />
      ))}
      <circle cx="110" cy="110" r="28" stroke="currentColor" strokeWidth=".8" />
      <circle cx="110" cy="110" r="5" fill="currentColor" />
    </svg>
  );
}
