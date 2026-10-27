import { Code2 } from "lucide-react";
import AgentLogo from "../marketing/AgentLogo";
import SurfaceRoutes from "./SurfaceRoutes";
import styles from "./sdk-architecture.module.css";

const agents = [
  { id: "codex", name: "Codex" },
  { id: "claude", name: "Claude Code" },
  { id: "antigravity", name: "Antigravity" },
] as const;

export default function SDKArchitecture() {
  return (
    <SurfaceRoutes
      kind="sdkRuntime"
      routeColor="var(--mw-runtime-route)"
      className={styles.scene}
    >
      <div className={styles.product} data-sprue="sdk-product">
        <Code2 size={27} strokeWidth={1.75} aria-hidden="true" />
        <div>
          <strong>Your product</strong>
          <p>Your interface. Your users. Your ideas.</p>
        </div>
      </div>

      <div className={styles.runtime} data-sprue="sdk-runtime">
        <strong>Mindwire runtime</strong>
        <p>TypeScript · Go · REST + SSE</p>
        <div className={styles.capabilities}>
          <span>Conversations</span>
          <span>Tool events</span>
          <span>Approvals</span>
          <span>Optional profiles</span>
        </div>
      </div>

      <div className={styles.agents}>
        {agents.map((agent) => (
          <div
            key={agent.id}
            className={styles.agent}
            data-sprue={`sdk-${agent.id}`}
          >
            <AgentLogo agent={agent.id} size={28} />
            <span>{agent.name}</span>
          </div>
        ))}
      </div>

      <div className={styles.hosts}>
        <span>Local</span>
        <span>SSH server</span>
        <span>Docker</span>
        <span>Cloud</span>
      </div>
    </SurfaceRoutes>
  );
}
