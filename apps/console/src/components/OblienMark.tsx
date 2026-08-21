import { cn } from "@/lib/utils";

// The real Oblien brand mark — the rounded-square ring glyph from the oblien.com / landing-site asset
// (`apps/web/public/oblien.svg`), inlined and recolored to `currentColor` so it inherits the app's
// black-and-white ink (white on the dark console, black on light). The source art carries heavy padding
// inside its 0–400 viewBox, so we crop the viewBox to the glyph's bounds to make it fill its box. The
// wordmark is set in Gellix (the brand typeface, loaded in index.css), lowercase, to match oblien.com.
export function OblienMark({
  className,
  showWord = true,
  size = 24,
}: {
  className?: string;
  showWord?: boolean;
  size?: number;
}) {
  return (
    <span className={cn("inline-flex items-center gap-2 select-none", className)}>
      <svg
        role="img"
        aria-label="Oblien"
        viewBox="84 84 232 232"
        width={size}
        height={size}
        className="shrink-0"
      >
        <g transform="translate(0,400) scale(0.1,-0.1)" fill="currentColor" stroke="none">
          <path
            d="M1270 3124 c-194 -51 -345 -204 -395 -399 -22 -88 -22 -1362 0 -1450
50 -197 203 -350 400 -400 88 -22 1362 -22 1450 0 197 50 350 203 400 400 13
50 15 163 15 725 0 562 -2 675 -15 725 -50 197 -203 350 -400 400 -86 22
-1371 21 -1455 -1z m1474 -218 c32 -16 76 -46 96 -66 20 -20 50 -64 66 -96
l29 -59 0 -685 0 -685 -26 -56 c-34 -72 -90 -128 -165 -165 l-59 -29 -685 0
-685 0 -59 29 c-75 37 -131 93 -165 165 l-26 56 -3 664 c-3 731 -4 720 59 814
36 55 102 104 174 129 46 16 104 17 720 15 l670 -2 59 -29z"
          />
        </g>
      </svg>
      {showWord && (
        <span className="text-[15px] font-semibold lowercase tracking-tight">oblien</span>
      )}
    </span>
  );
}
