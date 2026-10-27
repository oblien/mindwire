import { cn } from "@/lib/utils";

/** The same graphic wordmark as the public Mindwire site. */
export function BrandMark({
  size = 22,
  className,
  wordClassName,
}: {
  size?: number;
  className?: string;
  wordClassName?: string;
}) {
  return (
    <svg
      className={cn("shrink-0 select-none", className, wordClassName)}
      width={size * 7.8125}
      height={size}
      viewBox="0 0 250 32"
      role="img"
      aria-label="Mindwire"
      style={{ fontFamily: "var(--font-graphic)", fill: "currentColor", fontSize: 28 }}
    >
      <text x="0" y="27" textLength="250" lengthAdjust="spacingAndGlyphs">
        MINDWIRE
      </text>
    </svg>
  );
}
