/**
 * Exact DOM measuring and route expansion used by Rainbrand's Sprue React wrapper.
 * Extracted from public module 622087 in 1yht79f8qhdxw.js; semantic names added.
 * This file is framework-independent. The caller owns ResizeObserver/media listeners.
 */
import {
  buildSprueGeometry,
  steppedWindowClipPath,
  roundedPolylineLength,
  centeredDashPattern,
} from "./sprue-geometry.js";

export const BREAKPOINTS = {
  mobile: "(width < 768px)",
  tablet: "(768px <= width < 1280px)",
  desktop: "(width >= 1280px)",
  belowDesktop: "(width < 1280px)",
};

export const DIRECTIONS = {
  n: [0, -1],
  s: [0, 1],
  e: [1, 0],
  w: [-1, 0],
  ne: [1, -1],
  nw: [-1, -1],
  se: [1, 1],
  sw: [-1, 1],
};

export function snapToDevicePixel(value, dpr = window.devicePixelRatio || 1) {
  return Math.round(value * dpr) / dpr;
}

/**
 * Layout contract:
 * { W, H, corner:[x,y], wins:Map<string,{box:{x,y,w,h},parts?:Rect[]}>, clips }
 * All window boxes are relative to the rounded absolute origin of root.
 * `corner` compensates the SVG's absolute position for that origin rounding.
 * Stepped windows use direct child boxes in DOM order; zero-size boxes are skipped.
 */
export function measureSprueLayout(root, dpr = window.devicePixelRatio || 1) {
  const snap = (value) => snapToDevicePixel(value, dpr);
  const rootRect = root.getBoundingClientRect();
  const originX = snap(rootRect.left);
  const originY = snap(rootRect.top);

  function box(element) {
    const rect = element.getBoundingClientRect();
    const x = snap(rect.left);
    const y = snap(rect.top);
    return {
      x: x - originX,
      y: y - originY,
      w: snap(rect.right) - x,
      h: snap(rect.bottom) - y,
    };
  }

  const wins = new Map();
  for (const element of root.querySelectorAll("[data-sprue]")) {
    const rect = box(element);
    if (!rect.w || !rect.h) continue;
    const parts = element.hasAttribute("data-sprue-stepped")
      ? [...element.children].map(box).filter((part) => part.w && part.h)
      : undefined;
    wins.set(element.dataset.sprue, { box: rect, parts });
  }

  const { w: W, h: H } = box(root);
  const clips = [...root.querySelectorAll("[data-sprue-clip]")].map(
    (element) => {
      const rect = element.getBoundingClientRect();
      const target = wins.get(element.dataset.sprueClip);
      const parts = target?.parts?.length
        ? target.parts
        : target
          ? [target.box]
          : [];
      return {
        element,
        path: steppedWindowClipPath(
          parts.map((part) => ({
            ...part,
            x: part.x + originX - rect.left,
            y: part.y + originY - rect.top,
          })),
        ),
      };
    },
  );

  return {
    W,
    H,
    corner: [originX - rootRect.left, originY - rootRect.top],
    wins,
    clips,
  };
}

/** Anchor: {x,y} with numbers/percent strings, or {win,side,at?,offset?,part?}. */
export function resolveAnchor(anchor, layout, dpr = 1) {
  const snap = (value) => snapToDevicePixel(value, dpr);
  if ("x" in anchor) {
    const coordinate = (value, extent) =>
      typeof value === "number" ? value : (parseFloat(value) / 100) * extent;
    return [
      snap(coordinate(anchor.x, layout.W)),
      snap(coordinate(anchor.y, layout.H)),
    ];
  }

  const target = layout.wins.get(anchor.win);
  if (!target) return null;
  const parts = target.parts?.length ? target.parts : [target.box];
  const leadingSide = anchor.side === "top" || anchor.side === "left";
  const rect =
    parts[anchor.part ?? (leadingSide ? 0 : parts.length - 1)] ?? target.box;
  const at = anchor.at ?? 0.5;
  const offset = anchor.offset ?? 0;
  switch (anchor.side) {
    case "top":
      return [snap(rect.x + rect.w * at + offset), rect.y];
    case "bottom":
      return [snap(rect.x + rect.w * at + offset), rect.y + rect.h];
    case "left":
      return [rect.x, snap(rect.y + rect.h * at + offset)];
    case "right":
      return [rect.x + rect.w, snap(rect.y + rect.h * at + offset)];
  }
}

/**
 * Exact automatic elbow/diagonal choice for routes with a `to` anchor.
 * Top/bottom source anchors swap axes so the path leaves the window vertically.
 */
export function automaticRoute([fromX, fromY], [toX, toY], verticalFirst) {
  let result;
  const [a, b, c, d] = verticalFirst
    ? [fromY, fromX, toY, toX]
    : [fromX, fromY, toX, toY];
  const forwardDistance = c - a;
  const sidewaysDistance = d - b;

  if (Math.abs(sidewaysDistance) < 1) {
    result = [
      [a, b],
      [c, b],
    ];
  } else if (Math.abs(forwardDistance) >= Math.abs(sidewaysDistance) + 48) {
    const lead =
      ((Math.abs(forwardDistance) - Math.abs(sidewaysDistance)) / 2) *
      Math.sign(forwardDistance);
    result = [
      [a, b],
      [a + lead, b],
      [a + lead + Math.sign(forwardDistance) * Math.abs(sidewaysDistance), d],
      [c, d],
    ];
  } else {
    const middle = a + forwardDistance / 2;
    result = [
      [a, b],
      [middle, b],
      [middle, d],
      [c, d],
    ];
  }

  return verticalFirst ? result.map(([x, y]) => [y, x]) : result;
}

/**
 * A step is [direction,number|'edge'|{to:Anchor,axis?:'x'|'y'}].
 * Diagonal directions deliberately use ±1, not unit vectors. Anchor steps take
 * the absolute distance along the selected axis; direction determines its sign.
 * `edge` extends two pixels past the root to avoid section-boundary hairlines.
 */
export function resolveRoute(route, layout, dpr = 1) {
  const from = resolveAnchor(route.from, layout, dpr);
  if (!from) return null;

  if (route.to) {
    const to = resolveAnchor(route.to, layout, dpr);
    if (!to) return null;
    const sourceSide = "side" in route.from ? route.from.side : null;
    return automaticRoute(
      from,
      to,
      sourceSide === "top" || sourceSide === "bottom",
    );
  }

  const points = [from];
  for (const [directionName, step] of route.steps ?? []) {
    const [x, y] = points[points.length - 1];
    const [dx, dy] = DIRECTIONS[directionName];
    let distance;
    if (step === "edge") {
      distance = Math.min(
        dx < 0 ? x + 2 : dx > 0 ? layout.W + 2 - x : Infinity,
        dy < 0 ? y + 2 : dy > 0 ? layout.H + 2 - y : Infinity,
      );
      if (distance <= 0) continue;
    } else if (typeof step === "number") {
      distance = step;
    } else {
      const to = resolveAnchor(step.to, layout, dpr);
      if (!to) return null;
      const axis = step.axis ?? (dy ? "y" : "x");
      distance = Math.abs(axis === "y" ? to[1] - y : to[0] - x);
    }
    // Responsive surfaces can touch. A zero-length segment has no direction
    // and would produce invalid rounded joins while the layout is resizing.
    if (!Number.isFinite(distance) || distance <= 0) continue;
    points.push([x + dx * distance, y + dy * distance]);
  }
  return points.length >= 2 ? points : null;
}

/** Pure final expansion once DOM measurement and media matching are available. */
export function computeSprueGeometry(layout, definition, matches, dpr = 1) {
  const routes =
    matches(BREAKPOINTS.mobile) && definition.mobileRoutes
      ? definition.mobileRoutes
      : definition.routes;
  const windows = [];
  const stepped = [];
  for (const target of layout.wins.values()) {
    if (target.parts && target.parts.length > 1) stepped.push(target.parts);
    else windows.push(target.box);
  }

  const lines = routes.flatMap((route) => {
    if (route.media && !matches(route.media)) return [];
    const points = resolveRoute(route, layout, dpr);
    return points ? [{ points, sharp: route.sharp, gap: route.gap }] : [];
  });

  return {
    ...buildSprueGeometry({ windows, stepped, lines }),
    W: layout.W,
    H: layout.H,
    corner: layout.corner,
    overlay:
      typeof definition.overlay === "string"
        ? matches(definition.overlay)
        : (definition.overlay ?? false),
    dashes: lines.map((line) =>
      line.gap
        ? centeredDashPattern(
            roundedPolylineLength(line.points, line.sharp),
            line.gap,
          )
        : undefined,
    ),
  };
}

export function applySprueClips(clips) {
  for (const { element, path } of clips) {
    const value = path ? `path("${path}")` : "inset(50%)";
    if (element.style.getPropertyValue("--sprue-clip") !== value) {
      element.style.setProperty("--sprue-clip", value);
    }
    if (element.hasAttribute("data-sprue-clip-invalid") !== !path) {
      element.toggleAttribute("data-sprue-clip-invalid", !path);
    }
  }
}
