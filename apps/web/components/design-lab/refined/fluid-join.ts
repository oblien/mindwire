type Point = { x: number; y: number };
export type Anchor = Point & { dx: number; dy: number };
export type Edge = "top" | "right" | "bottom" | "left";

// Widen the ends into the surfaces with a circular fillet. The narrow middle
// follows a cubic curve, so the joins stay fluid as the cards resize.
export function fluidJoin(from: Anchor, to: Anchor, flare = 24) {
  const reach = Math.hypot(to.x - from.x, to.y - from.y) * 0.48;
  const c1 = { x: from.x + from.dx * reach, y: from.y + from.dy * reach };
  const c2 = { x: to.x + to.dx * reach, y: to.y + to.dy * reach };
  const samples: (Point & { nx: number; ny: number; distance: number })[] = [];
  let distance = 0;

  for (let index = 0; index <= 120; index++) {
    const t = index / 120;
    const u = 1 - t;
    const x =
      u ** 3 * from.x +
      3 * u ** 2 * t * c1.x +
      3 * u * t ** 2 * c2.x +
      t ** 3 * to.x;
    const y =
      u ** 3 * from.y +
      3 * u ** 2 * t * c1.y +
      3 * u * t ** 2 * c2.y +
      t ** 3 * to.y;
    const dx =
      3 * u ** 2 * (c1.x - from.x) +
      6 * u * t * (c2.x - c1.x) +
      3 * t ** 2 * (to.x - c2.x);
    const dy =
      3 * u ** 2 * (c1.y - from.y) +
      6 * u * t * (c2.y - c1.y) +
      3 * t ** 2 * (to.y - c2.y);
    const length = Math.hypot(dx, dy) || 1;
    const previous = samples[index - 1];
    if (previous) distance += Math.hypot(x - previous.x, y - previous.y);
    samples.push({ x, y, nx: -dy / length, ny: dx / length, distance });
  }

  const radius = Math.min(flare, distance / 4);
  const widen = (along: number) => {
    const remaining = Math.max(0, radius - along);
    return radius - Math.sqrt(radius ** 2 - remaining ** 2);
  };
  const outline = (side: number) =>
    samples.map((point) => {
      const half =
        1.2 + widen(point.distance) + widen(distance - point.distance);
      return `${(point.x + point.nx * half * side).toFixed(2)} ${(point.y + point.ny * half * side).toFixed(2)}`;
    });
  return `M${outline(1).join("L")}L${outline(-1).reverse().join("L")}Z`;
}
