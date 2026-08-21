// A monochrome horizontal proportion bar: each segment's width is its share of the total, shaded by a
// descending ink opacity so the mix reads at a glance without color. Sharp-cornered, ink palette — the
// legend swatches reuse the exact same shades, so bar and legend always agree. Segments render in the
// order given (a fixed, learnable order beats sorting by magnitude for a recurring breakdown).
export interface BarSegment {
  label: string;
  value: number;
  /** Pre-formatted value for the legend (e.g. `compact(value)`); omit to show the percentage only. */
  display?: string;
}

// Most→least prominent, assigned in segment order. Six covers the token mix (input/output/cache×2/
// reasoning) with room to spare; a 7th+ segment falls back to the faintest shade.
const SHADES = [0.9, 0.62, 0.42, 0.28, 0.17, 0.1];
const shade = (i: number) => SHADES[i] ?? SHADES[SHADES.length - 1];

export function StackedBar({
  segments,
  legend = true,
  className,
}: {
  segments: BarSegment[];
  legend?: boolean;
  className?: string;
}) {
  const total = segments.reduce((s, x) => s + Math.max(0, x.value), 0);
  const pct = (v: number) => (total > 0 ? (Math.max(0, v) / total) * 100 : 0);

  return (
    <div className={className}>
      <div className="flex h-3 w-full overflow-hidden border border-border bg-ink/[0.03]">
        {total > 0 &&
          segments.map((s, i) => {
            const w = pct(s.value);
            if (w <= 0) return null;
            return (
              <div
                key={s.label}
                className="h-full bg-ink"
                style={{ width: `${w}%`, opacity: shade(i) }}
                title={`${s.label}: ${w.toFixed(1)}%`}
              />
            );
          })}
      </div>
      {legend && (
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
          {segments.map((s, i) => {
            if (s.value <= 0) return null;
            return (
              <span
                key={s.label}
                className="inline-flex items-center gap-1.5 text-[10px] text-muted-foreground"
              >
                <span className="size-2 shrink-0 bg-ink" style={{ opacity: shade(i) }} />
                <span className="text-foreground">{s.label}</span>
                {s.display !== undefined && <span className="tabular-nums">{s.display}</span>}
                <span className="tabular-nums text-muted-foreground">{pct(s.value).toFixed(0)}%</span>
              </span>
            );
          })}
        </div>
      )}
    </div>
  );
}
