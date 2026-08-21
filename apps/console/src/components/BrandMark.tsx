import { cn } from "@/lib/utils";
import { OblienMark } from "@/components/OblienMark";

// The product lockup: the Oblien parent-brand glyph next to the "MindWire" wordmark — the same treatment
// the marketing site uses (OblienLogo + "MindWire"). MindWire is built by Oblien, so the mark stays
// Oblien's ring glyph and the wordmark carries the product name. Monochrome ink, so it flips with theme.
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
    <span className={cn("inline-flex items-center gap-2 select-none", className)}>
      <OblienMark showWord={false} size={size} />
      <span className={cn("font-semibold tracking-tight", wordClassName)}>MindWire</span>
    </span>
  );
}
