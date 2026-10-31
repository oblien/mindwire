import { cn } from "@/lib/utils";

/** The same naturally spaced wordmark as the public Mindwire site. */
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
    <span
      className={cn("shrink-0 select-none", className, wordClassName)}
      aria-label="Mindwire"
      style={{
        display: "inline-block",
        fontFamily: "var(--font-display)",
        fontSize: size * 1.45,
        fontWeight: 600,
        letterSpacing: "-0.065em",
        lineHeight: 1,
      }}
    >
      mindwire
    </span>
  );
}
