import type { CSSProperties } from "react";
import type { ProductAgent } from "@/lib/product";

export default function AgentLogo({
  agent,
  size = 24,
  className = "",
}: {
  agent: ProductAgent;
  size?: number;
  className?: string;
}) {
  return (
    <span
      aria-hidden="true"
      className={`logo-mask shrink-0 ${className}`}
      style={
        {
          "--logo": `url(/logos/${agent}.svg)`,
          width: size,
          height: size,
        } as CSSProperties
      }
    />
  );
}
