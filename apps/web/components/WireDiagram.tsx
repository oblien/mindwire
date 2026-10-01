import { productAgents } from "@/lib/product";
import styles from "./WireDiagram.module.css";

// Keep the original SVG wire animation and brand artwork. The product catalog
// determines which agents appear, so the diagram only shows supported agents.
const agents = productAgents.map((agent, index) => {
  const cx = 44 + (index * 376) / (productAgents.length - 1);
  const colorLogo = agent.id === "claude" || agent.id === "codex";
  return {
    ...agent,
    cx,
    logo: `/logos/${agent.id}${colorLogo ? "-color" : ""}.svg`,
    wire: `M${cx} 56 C ${cx} 130 232 130 232 214`,
  };
});
const appWire = "M232 268 L232 366";

export default function WireDiagram() {
  return (
    <svg
      viewBox="0 0 464 424"
      role="img"
      aria-label={`${productAgents.map((agent) => agent.name).join(", ")} connect through Mindwire to your iPhone`}
      className={styles.diagram}
    >
      <g
        stroke="currentColor"
        strokeOpacity="0.2"
        strokeWidth="1.5"
        fill="none"
      >
        {agents.map((agent) => (
          <path key={agent.id} d={agent.wire} />
        ))}
        <path d={appWire} />
      </g>

      {/* junction dots */}
      <g fill="currentColor" fillOpacity="0.35">
        <circle cx="232" cy="214" r="2.5" />
        <circle cx="232" cy="268" r="2.5" />
      </g>

      {/* Native SVG motion needs no client bundle. Reduced motion keeps the
          static wires and labels, hiding only the travelling pulses. */}
      <g className={styles.pulses} fill="currentColor">
        {agents.map((agent, index) => (
          <circle key={agent.id} r="3.5" opacity="0">
            <animateMotion
              path={agent.wire}
              dur="2.8s"
              begin={`${index * 0.42}s`}
              repeatCount="indefinite"
            />
            <animate
              attributeName="opacity"
              dur="2.8s"
              begin={`${index * 0.42}s`}
              repeatCount="indefinite"
              values="0;1;1;0"
              keyTimes="0;0.15;0.85;1"
            />
          </circle>
        ))}
        <circle r="3.5" opacity="0">
          <animateMotion
            path={appWire}
            dur="1.5s"
            begin="0.2s"
            repeatCount="indefinite"
          />
          <animate
            attributeName="opacity"
            dur="1.5s"
            begin="0.2s"
            repeatCount="indefinite"
            values="0;1;1;0"
            keyTimes="0;0.2;0.8;1"
          />
        </circle>
      </g>

      {agents.map((agent) => (
        <g key={agent.id}>
          <rect
            x={agent.cx - 14}
            y="2"
            width="28"
            height="28"
            fill="#ffffff"
            stroke="rgba(0,0,0,0.12)"
            strokeWidth="1"
          />
          <image
            href={agent.logo}
            x={agent.cx - 9}
            y="7"
            width="18"
            height="18"
          />
          <text
            x={agent.cx}
            y="46"
            textAnchor="middle"
            fontSize="12"
            fill="currentColor"
            className="font-mono"
          >
            {agent.name}
          </text>
        </g>
      ))}

      <g>
        <rect
          x="157"
          y="214"
          width="150"
          height="54"
          fill="currentColor"
          fillOpacity="0.06"
          stroke="currentColor"
          strokeOpacity="0.65"
        />
        <text
          x="232"
          y="247"
          textAnchor="middle"
          fontSize="18"
          fontWeight="600"
          fill="currentColor"
          className="font-sans"
        >
          Mindwire
        </text>
      </g>

      <g>
        <rect
          x="162"
          y="366"
          width="140"
          height="44"
          fill="none"
          stroke="currentColor"
          strokeOpacity="0.5"
        />
        <text
          x="232"
          y="393"
          textAnchor="middle"
          fontSize="16"
          fill="currentColor"
          className="font-sans"
        >
          Your iPhone
        </text>
      </g>
    </svg>
  );
}
