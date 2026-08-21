// A dependency-free, monochrome area+line sparkline for one numeric series, drawn in the console's ink
// palette: `currentColor` over the surface at low opacity, sharp-cornered, no chart library (see
// WireDiagram for the same convention). The SVG uses a fixed viewBox with `preserveAspectRatio="none"`
// so one path definition stretches fluidly to the container width, and `vector-effect` keeps the stroke
// a crisp 1.5px at any scale. Values map left→right, oldest→newest. Give the parent a `text-ink`/
// `text-foreground` color and the chart inherits it.
//
// Auto-scales to the series peak unless `max` is given — right for CPU% (which can exceed 100 on
// multiple cores) and for memory (arbitrary magnitude), so a small signal is still legible.

// viewBox units — arbitrary; the SVG scales to its box. Chosen wide so x-positions have room.
const VW = 100;
const VH = 32;

export function MiniAreaChart({
  values,
  max,
  height = 52,
  className,
}: {
  values: number[];
  /** Fixed top of the scale; omit to auto-scale to the series peak. */
  max?: number;
  height?: number;
  className?: string;
}) {
  const n = values.length;
  const peak = max ?? values.reduce((m, v) => (v > m ? v : m), 0);
  const top = peak > 0 ? peak : 1; // avoid /0; a flat-zero series then draws along the baseline

  // x spreads points across the full width; y is inverted (SVG origin is top-left). A single point is
  // drawn pinned to the right edge so the first frame shows something rather than collapsing to nothing.
  const xy = (i: number, v: number): [number, number] => {
    const x = n <= 1 ? VW : (i / (n - 1)) * VW;
    const y = VH - (Math.min(v, top) / top) * VH;
    return [x, y];
  };
  const pts = values.map((v, i) => xy(i, v));
  const line = pts.map(([x, y]) => `${x.toFixed(2)},${y.toFixed(2)}`).join(" ");
  const area = pts.length
    ? `M0,${VH} ` + pts.map(([x, y]) => `L${x.toFixed(2)},${y.toFixed(2)}`).join(" ") + ` L${VW},${VH} Z`
    : "";

  return (
    <svg
      viewBox={`0 0 ${VW} ${VH}`}
      preserveAspectRatio="none"
      style={{ height, width: "100%", display: "block" }}
      className={className}
      aria-hidden
    >
      {/* baseline */}
      <line
        x1="0"
        y1={VH}
        x2={VW}
        y2={VH}
        stroke="currentColor"
        strokeOpacity="0.14"
        strokeWidth="1"
        vectorEffect="non-scaling-stroke"
      />
      {area && <path d={area} fill="currentColor" fillOpacity="0.08" />}
      {pts.length > 1 && (
        <polyline
          points={line}
          fill="none"
          stroke="currentColor"
          strokeOpacity="0.8"
          strokeWidth="1.5"
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
      )}
    </svg>
  );
}
