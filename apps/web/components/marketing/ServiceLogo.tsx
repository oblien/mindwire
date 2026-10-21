import type { CSSProperties } from "react";

/** Consumer account branding; engine names remain in the developer experience. */
export default function ServiceLogo({ service, size = 24 }: {
  service: "chatgpt" | "claude";
  size?: number;
}) {
  return <span aria-hidden="true" className="logo-mask shrink-0" style={{
    "--logo": `url(/logos/${service}.svg)`, width: size, height: size,
  } as CSSProperties} />;
}
