/**
 * Rainbrand's public Sprue geometry, semantically renamed without algorithm changes.
 * Source: module 436429, https://www.rainbrand.com/_next/static/immutable/chunks/1yht79f8qhdxw.js
 * Units are CSS pixels. Input points and window boxes are already device-pixel snapped.
 * Preserves exact 0.75px anti-seam overlaps, 2px stroke, 20px joints, and SVG number formatting.
 */
let SPRUE_DEFAULTS = {
    stroke: 2,
    radius: 20,
    padding: 6,
    innerRadius: 16,
  },
  point = (e, t) => ({
    x: e,
    y: t,
  }),
  add = (e, t) => point(e.x + t.x, e.y + t.y),
  subtract = (e, t) => point(e.x - t.x, e.y - t.y),
  scale = (e, t) => point(e.x * t, e.y * t),
  dot = (e, t) => e.x * t.x + e.y * t.y,
  cross = (e, t) => e.x * t.y - e.y * t.x,
  length = (e) => Math.hypot(e.x, e.y),
  normalize = (e) => scale(e, 1 / length(e)),
  perpendicular = (e) => point(-e.y, e.x),
  formatPoint = (e) => `${+e.x.toFixed(3)} ${+e.y.toFixed(3)}`;
function intersectLines(originA, directionA, originB, directionB) {
  let distance =
    cross(subtract(originB, originA), directionB) /
    cross(directionA, directionB);
  return add(originA, scale(directionA, distance));
}
function roundedCornerFill(
  corner,
  firstRay,
  secondRay,
  firstOutsideNormal,
  secondOutsideNormal,
  radius,
) {
  let angle = Math.acos(Math.max(-1, Math.min(1, dot(firstRay, secondRay)))),
    tangentDistance = radius / Math.tan(angle / 2),
    tangentA = add(corner, scale(firstRay, tangentDistance)),
    tangentB = add(corner, scale(secondRay, tangentDistance)),
    center = add(
      corner,
      scale(normalize(add(firstRay, secondRay)), radius / Math.sin(angle / 2)),
    ),
    sweep = +(
      cross(subtract(tangentA, center), subtract(tangentB, center)) > 0
    ),
    coveredTangentA = add(tangentA, scale(firstOutsideNormal, 0.75)),
    coveredTangentB = add(tangentB, scale(secondOutsideNormal, 0.75)),
    coveredCorner = add(
      corner,
      scale(add(firstOutsideNormal, secondOutsideNormal), 0.75),
    );
  return `M ${formatPoint(coveredTangentA)} L ${formatPoint(tangentA)} A ${radius} ${radius} 0 0 ${sweep} ${formatPoint(tangentB)} L ${formatPoint(coveredTangentB)} L ${formatPoint(coveredCorner)} Z`;
}
let clamp = (e, t, r) => Math.min(Math.max(e, t), r),
  windowRadius = (e) => Math.min(SPRUE_DEFAULTS.radius, e.w / 2, e.h / 2);
function clampToWindowCore(rect, position) {
  let radius = windowRadius(rect);
  return point(
    clamp(position.x, rect.x + radius, rect.x + rect.w - radius),
    clamp(position.y, rect.y + radius, rect.y + rect.h - radius),
  );
}
function rayMarchWindow(rect, origin, direction, outset = 0) {
  let distance = 0;
  for (let c = 0; c < 32; c++) {
    var s;
    let c =
      length(
        subtract(
          (s = add(origin, scale(direction, distance))),
          clampToWindowCore(rect, s),
        ),
      ) -
      windowRadius(rect) -
      outset;
    if (c < 0.01) return distance;
    distance += c;
  }
  return null;
}
function attachLineToWindow(
  rect,
  [endpointX, endpointY],
  [directionX, directionY],
  radius = SPRUE_DEFAULTS.radius,
) {
  let endpoint = point(endpointX, endpointY),
    direction = normalize(point(directionX, directionY)),
    halfStroke = SPRUE_DEFAULTS.stroke / 2,
    endInset =
      Math.max(
        ...[1, -1].map(
          (t) =>
            rayMarchWindow(
              rect,
              add(endpoint, scale(perpendicular(direction), t * halfStroke)),
              direction,
            ) ?? 0,
        ),
      ) + 2,
    fills =
      radius > 0
        ? [1, -1].flatMap(
            (r) =>
              (function (e, r, i, u, h, p) {
                let d = scale(perpendicular(i), u),
                  v = add(r, scale(d, SPRUE_DEFAULTS.stroke / 2)),
                  y = add(add(v, scale(d, p)), scale(i, -4 * p)),
                  b = rayMarchWindow(e, y, i, p);
                if (null === b) return null;
                let w = add(y, scale(i, b)),
                  E = add(w, scale(d, -p)),
                  M = clampToWindowCore(e, w),
                  T = normalize(subtract(w, M)),
                  R = add(M, scale(T, windowRadius(e))),
                  $ = +(cross(subtract(E, w), subtract(R, w)) > 0),
                  A = add(E, scale(d, -0.75)),
                  P = add(R, scale(T, -0.75)),
                  S = add(add(v, scale(i, h)), scale(d, -0.75));
                return `M ${formatPoint(A)} L ${formatPoint(E)} A ${p} ${p} 0 0 ${$} ${formatPoint(R)} L ${formatPoint(P)} L ${formatPoint(S)} Z`;
              })(rect, endpoint, direction, r, endInset, radius) ?? [],
          )
        : [],
    end = add(endpoint, scale(direction, endInset));
  return {
    fills: fills,
    end: [end.x, end.y],
  };
}
function stepShoulderFill(x, y, horizontalSign, verticalSign) {
  let radius = SPRUE_DEFAULTS.radius;
  return `M ${x - 0.75 * horizontalSign} ${y - 0.75 * verticalSign} L ${x + horizontalSign * radius} ${y - 0.75 * verticalSign} L ${x + horizontalSign * radius} ${y} A ${radius} ${radius} 0 0 ${horizontalSign * verticalSign > 0 ? 0 : 1} ${x} ${y + verticalSign * radius} L ${x - 0.75 * horizontalSign} ${y + verticalSign * radius} Z`;
}
export { SPRUE_DEFAULTS };
export function buildSprueGeometry(scene) {
  let cornerRadius = SPRUE_DEFAULTS.radius,
    halfStroke = SPRUE_DEFAULTS.stroke / 2,
    fills = [],
    strokes = [],
    windows = scene.windows.map((e) => ({
      ...e,
      hidden: [],
    }));
  for (let t of scene.stepped ?? []) {
    let e = t.map((e) => ({
      ...e,
      hidden: [],
    }));
    for (let t = 1; t < e.length; t++) {
      let r = e[t - 1],
        n = e[t],
        a = Math.max(r.x, n.x),
        o = Math.min(r.x + r.w, n.x + n.w);
      Math.abs(r.y + r.h - n.y) > 0.75 ||
        a >= o ||
        (r.hidden.push({
          side: "bottom",
          lo: a,
          hi: o,
        }),
        n.hidden.push({
          side: "top",
          lo: a,
          hi: o,
        }),
        fills.push(
          `M ${a} ${n.y - cornerRadius} H ${o} V ${n.y + cornerRadius} H ${a} Z`,
        ),
        r.x !== n.x &&
          fills.push(stepShoulderFill(a, n.y, -1, r.x < n.x ? 1 : -1)),
        r.x + r.w !== n.x + n.w &&
          fills.push(
            stepShoulderFill(o, n.y, 1, r.x + r.w > n.x + n.w ? 1 : -1),
          ));
    }
    windows.push(...e);
  }
  let paths = scene.lines
    .filter((e) => e.points.length >= 2)
    .map((e) => {
      let t = e.points.map(([e, t]) => point(e, t));
      return {
        P: t,
        runs: (function (e) {
          let t = [];
          for (let r = 1; r < e.length; r++) {
            let n = e[r - 1],
              o = e[r];
            if (1e-6 > length(subtract(o, n))) continue;
            let c = t.at(-1),
              f = normalize(subtract(o, n));
            if (c) {
              let e = normalize(subtract(c[1], c[0]));
              if (1e-6 > Math.abs(cross(e, f)) && dot(e, f) > 0) {
                c[1] = o;
                continue;
              }
            }
            t.push([n, o]);
          }
          return t;
        })(t),
        sharp: new Set(e.sharp ?? []),
      };
    });
  for (let e of paths) {
    let t = e.P.length - 1;
    for (let [f, x] of [
      [0, normalize(subtract(e.P[0], e.P[1]))],
      [t, normalize(subtract(e.P[t], e.P[t - 1]))],
    ]) {
      let t = e.P[f],
        y = (function (e) {
          for (let t of windows) {
            let r = e.x > t.x + 0.75 && e.x < t.x + t.w - 0.75,
              n = e.y > t.y + 0.75 && e.y < t.y + t.h - 0.75,
              a = (e, r) =>
                t.hidden.some(
                  (t) => t.side === e && r >= t.lo - 0.75 && r <= t.hi + 0.75,
                );
            if (
              (r && 0.75 > Math.abs(e.y - t.y) && !a("top", e.x)) ||
              (r && 0.75 > Math.abs(e.y - (t.y + t.h)) && !a("bottom", e.x)) ||
              (n &&
                (0.75 > Math.abs(e.x - t.x) ||
                  0.75 > Math.abs(e.x - (t.x + t.w))))
            )
              return t;
          }
          return null;
        })(t);
      if (y) {
        let n = attachLineToWindow(y, [t.x, t.y], [x.x, x.y]);
        (fills.push(...n.fills), (e.P[f] = point(...n.end)));
        continue;
      }
      let E = (function (e, t, r) {
        for (let c of paths)
          if (c !== r)
            for (let [r, f] of c.runs) {
              let c = subtract(f, r),
                h = normalize(c);
              if (1e-6 > Math.abs(cross(t, h))) continue;
              let p = dot(subtract(e, r), c) / dot(c, c);
              if (
                !(p <= 0) &&
                !(p >= 1) &&
                0.75 > length(subtract(e, add(r, scale(c, p))))
              )
                return {
                  e: h,
                };
            }
        return null;
      })(t, x, e);
      if (E) {
        let e =
          0 > dot(perpendicular(E.e), x)
            ? perpendicular(E.e)
            : scale(perpendicular(E.e), -1);
        fills.push(
          ...(function (e, t, r, a, l) {
            let u = perpendicular(t),
              s = add(e, scale(a, l));
            return [1, -1].map((l) => {
              let c = intersectLines(add(e, scale(u, l * halfStroke)), t, s, r),
                f = dot(r, scale(u, l)) > 0 ? r : scale(r, -1);
              return roundedCornerFill(
                c,
                scale(t, -1),
                f,
                scale(u, -l),
                scale(a, -1),
                cornerRadius,
              );
            });
          })(t, x, E.e, e, halfStroke),
        );
      }
    }
    for (let r of e.sharp) {
      if (r <= 0 || r >= t) continue;
      let i = normalize(subtract(e.P[r], e.P[r - 1])),
        u = normalize(subtract(e.P[r + 1], e.P[r])),
        f = Math.sign(cross(i, u)),
        x = scale(perpendicular(i), f),
        v = scale(perpendicular(u), f),
        y = intersectLines(
          add(e.P[r - 1], scale(x, halfStroke)),
          i,
          add(e.P[r], scale(v, halfStroke)),
          u,
        );
      fills.push(
        roundedCornerFill(
          y,
          scale(i, -1),
          u,
          scale(x, -1),
          scale(v, -1),
          cornerRadius,
        ),
      );
    }
    strokes.push(
      (function (e) {
        let t = e.P,
          r = [`M ${formatPoint(t[0])}`];
        for (let c = 1; c < t.length - 1; c++) {
          let h = normalize(subtract(t[c], t[c - 1])),
            p = normalize(subtract(t[c + 1], t[c])),
            m = Math.acos(Math.max(-1, Math.min(1, dot(h, p))));
          if (e.sharp.has(c) || m < 0.001) {
            r.push(`L ${formatPoint(t[c])}`);
            continue;
          }
          let g = Math.min(
              cornerRadius * Math.tan(m / 2),
              Math.min(
                length(subtract(t[c], t[c - 1])),
                length(subtract(t[c + 1], t[c])),
              ) / 2,
            ),
            x = g / Math.tan(m / 2),
            v = +(cross(h, p) > 0);
          (r.push(`L ${formatPoint(subtract(t[c], scale(h, g)))}`),
            r.push(
              `A ${x} ${x} 0 0 ${v} ${formatPoint(add(t[c], scale(p, g)))}`,
            ));
        }
        return (r.push(`L ${formatPoint(t[t.length - 1])}`), r.join(" "));
      })(e),
    );
  }
  return {
    fills: fills,
    strokes: strokes,
  };
}
export function roundedPolylineLength(coordinates, sharpIndices = []) {
  let points = coordinates.map(([e, t]) => point(e, t)),
    totalLength = 0;
  for (let e = 1; e < points.length; e++)
    totalLength += length(subtract(points[e], points[e - 1]));
  for (let e = 1; e < points.length - 1; e++) {
    let r = Math.acos(
      Math.max(
        -1,
        Math.min(
          1,
          dot(
            normalize(subtract(points[e], points[e - 1])),
            normalize(subtract(points[e + 1], points[e])),
          ),
        ),
      ),
    );
    if (sharpIndices.includes(e) || r < 0.001) continue;
    let c =
        Math.min(
          length(subtract(points[e], points[e - 1])),
          length(subtract(points[e + 1], points[e])),
        ) / 2,
      f = Math.min(SPRUE_DEFAULTS.radius * Math.tan(r / 2), c);
    totalLength += (f / Math.tan(r / 2)) * r - 2 * f;
  }
  return totalLength;
}
export function centeredDashPattern(pathLength, gap) {
  let clampedGap = Math.min(Math.max(gap, 0), pathLength);
  return `${(pathLength - clampedGap) / 2} ${clampedGap} ${pathLength}`;
}
export function steppedWindowClipPath(parts) {
  if (
    !parts.length ||
    parts.some(
      (e) =>
        ![e.x, e.y, e.w, e.h].every(Number.isFinite) || e.w <= 0 || e.h <= 0,
    )
  )
    return null;
  for (let t = 1; t < parts.length; t++) {
    let r = parts[t - 1],
      n = parts[t];
    if (
      Math.abs(r.y + r.h - n.y) > 0.75 ||
      Math.max(r.x, n.x) >= Math.min(r.x + r.w, n.x + n.w)
    )
      return null;
  }
  let first = parts[0],
    last = parts[parts.length - 1],
    outline = [point(first.x, first.y), point(first.x + first.w, first.y)];
  for (let t = 1; t < parts.length; t++) {
    let n = parts[t - 1],
      a = parts[t];
    n.x + n.w !== a.x + a.w &&
      outline.push(point(n.x + n.w, a.y), point(a.x + a.w, a.y));
  }
  outline.push(
    point(last.x + last.w, last.y + last.h),
    point(last.x, last.y + last.h),
  );
  for (let t = parts.length - 1; t > 0; t--) {
    let n = parts[t - 1],
      a = parts[t];
    n.x !== a.x && outline.push(point(a.x, a.y), point(n.x, a.y));
  }
  let corners = outline.filter((e, t) => {
    let r = outline[(t + outline.length - 1) % outline.length],
      n = outline[(t + 1) % outline.length];
    return Math.abs(cross(subtract(e, r), subtract(n, e))) > 0.75;
  });
  if (corners.length < 4) return null;
  let radius =
      4 === corners.length
        ? windowRadius({
            ...first,
            h: last.y + last.h - first.y,
          })
        : SPRUE_DEFAULTS.radius,
    commands = [];
  for (let e = 0; e < corners.length; e++) {
    let t = corners[e],
      r = subtract(t, corners[(e + corners.length - 1) % corners.length]),
      i = subtract(corners[(e + 1) % corners.length], t);
    if (Math.min(length(r), length(i)) < 2 * radius - 0.75) return null;
    let c = normalize(r),
      h = normalize(i),
      m = subtract(t, scale(c, radius)),
      x = add(t, scale(h, radius));
    (commands.push(`${e ? "L" : "M"} ${formatPoint(m)}`),
      commands.push(
        `A ${radius} ${radius} 0 0 ${+(cross(c, h) > 0)} ${formatPoint(x)}`,
      ));
  }
  return `${commands.join(" ")} Z`;
}
export { attachLineToWindow };
