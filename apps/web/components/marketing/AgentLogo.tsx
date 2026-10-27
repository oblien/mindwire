import type { CSSProperties } from "react";
import Image from "next/image";
import type { ProductAgent } from "@/lib/product";
import styles from "./AgentLogo.module.css";

export default function AgentLogo({
  agent,
  size = 24,
  className = "",
  surface = "auto",
}: {
  agent: ProductAgent | "antigravity";
  size?: number;
  className?: string;
  surface?: "auto" | "light" | "dark";
}) {
  if (agent === "claude" || agent === "antigravity") {
    return (
      <Image
        src={
          agent === "claude"
            ? "/logos/claude-color.svg"
            : "/logos/antigravity.svg"
        }
        alt=""
        aria-hidden="true"
        width={size}
        height={size}
        className={`shrink-0 ${className}`}
      />
    );
  }

  if (agent === "opencode") {
    // Preserve the two-tone SVG artwork supplied at https://opencode.ai/brand.
    return (
      <span
        aria-hidden="true"
        className={`${styles.artwork} ${className}`}
        data-surface={surface}
        style={{ width: size, height: size }}
      >
        <Image
          src="/logos/opencode-light.svg"
          alt=""
          width={size}
          height={size}
          className={styles.light}
        />
        <Image
          src="/logos/opencode-dark.svg"
          alt=""
          width={size}
          height={size}
          className={styles.dark}
        />
      </span>
    );
  }

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
