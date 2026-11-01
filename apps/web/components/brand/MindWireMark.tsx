import type { SVGProps } from "react";
import mark from "@/lib/brand-mark.json";

/** Two open terminals and the wire between them. One mark at every size. */
export default function MindWireMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      width={32}
      height={32}
      viewBox={mark.viewBox}
      fill="currentColor"
      aria-hidden="true"
      focusable="false"
      data-brand-mark="mindwire"
      {...props}
    >
      <path d={mark.path} />
    </svg>
  );
}
