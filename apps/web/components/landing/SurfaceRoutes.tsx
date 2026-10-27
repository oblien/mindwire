"use client";

import { useId, useLayoutEffect, useRef, useState, type HTMLAttributes } from "react";
import {
  measureSprueLayout,
  computeSprueGeometry,
  applySprueClips,
} from "./geometry/sprue-layout";
import * as routes from "./geometry/sprue-routes";
import styles from "./framework-home.module.css";

type Geometry = {
  W: number;
  H: number;
  corner: number[];
  strokes: string[];
  fills: string[];
  dashes: (string | undefined)[];
  overlay: boolean;
  artworkSpan?: { start: number; end: number };
};
type Definition = {
  routes: { media?: string }[];
  mobileRoutes?: { media?: string }[];
  tone?: string;
  overlay?: boolean | string;
};
type Props = HTMLAttributes<HTMLElement> & {
  kind: keyof typeof routes;
  as?: "section" | "div" | "footer";
  routeColor?: string;
  /** Keep the line visible over artwork, then blend into its connected surface. */
  artwork?: { surface?: string; dock: string; overlayOnly?: boolean };
};

/** Connect measured DOM surfaces with the reference's rounded routing geometry. */
export default function SurfaceRoutes({
  kind,
  as: Component = "div",
  routeColor,
  artwork,
  children,
  className = "",
  ...props
}: Props) {
  const root = useRef<HTMLElement>(null);
  const gradientId = useId();
  const [geometry, setGeometry] = useState<Geometry | null>(null);
  const definition = routes[kind] as Definition;
  const { surface, dock, overlayOnly } = artwork ?? {};
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    let alive = true;
    const queries = new Set([
      "(width < 768px)",
      ...[...definition.routes, ...(definition.mobileRoutes ?? [])].flatMap(
        (route) => (route.media ? [route.media] : []),
      ),
      ...(typeof definition.overlay === "string" ? [definition.overlay] : []),
    ]);
    const media = new Map(
      [...queries].map((query) => [query, matchMedia(query)]),
    );
    const measure = () => {
      if (!alive) return;
      const layout = measureSprueLayout(element);
      if (!layout.W || !layout.H) return;
      applySprueClips(layout.clips);
      const next: Geometry = computeSprueGeometry(
        layout,
        definition,
        (query: string) =>
          media.get(query)?.matches ?? matchMedia(query).matches,
        devicePixelRatio || 1,
      );
      const dockBox = dock ? layout.wins.get(dock)?.box : undefined;
      const surfaceBox = surface ? layout.wins.get(surface)?.box : undefined;
      if (dockBox && (!overlayOnly || next.overlay)) {
        const start = surfaceBox?.y ?? dockBox.y - 96;
        if (dockBox.y > start) {
          next.artworkSpan = { start, end: dockBox.y };
        }
      }
      setGeometry(next);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    element
      .querySelectorAll<HTMLElement>("[data-sprue], [data-sprue-clip]")
      .forEach((target) => {
        observer.observe(target);
        if (target.hasAttribute("data-sprue-stepped"))
          [...target.children].forEach((part) => observer.observe(part));
      });
    media.forEach((query) => query.addEventListener("change", measure));
    window.addEventListener("resize", measure);
    void document.fonts.ready.then(measure);
    measure();
    return () => {
      alive = false;
      observer.disconnect();
      media.forEach((query) => query.removeEventListener("change", measure));
      window.removeEventListener("resize", measure);
    };
  }, [definition, surface, dock, overlayOnly]);
  const paint = geometry?.artworkSpan ? `url(#${gradientId})` : "currentColor";
  return (
    <Component
      ref={(element) => {
        root.current = element;
      }}
      {...props}
      className={`${styles.routeSurface} ${className}`}
    >
      {geometry && (
        <svg
          aria-hidden="true"
          data-surface-route={kind}
          className={styles.surfaceRoutes}
          style={{
            left: geometry.corner[0],
            top: geometry.corner[1],
            zIndex: geometry.overlay ? 2 : -1,
            color:
              routeColor ??
              (definition.tone === "dark"
                ? "var(--mw-diagram-surface)"
                : "var(--mw-surface)"),
          }}
          width={geometry.W}
          height={geometry.H}
          viewBox={`0 0 ${geometry.W} ${geometry.H}`}
        >
          {geometry.artworkSpan && (
            <defs>
              <linearGradient
                id={gradientId}
                gradientUnits="userSpaceOnUse"
                x1={0}
                x2={0}
                y1={geometry.artworkSpan.start}
                y2={geometry.artworkSpan.end}
              >
                <stop offset={0} stopColor={surface ? "currentColor" : "#fff"} />
                <stop
                  offset={Math.min(
                    0.15,
                    12 / (geometry.artworkSpan.end - geometry.artworkSpan.start),
                  )}
                  stopColor="#fff"
                />
                <stop
                  offset={Math.max(
                    0.5,
                    1 - 40 / (geometry.artworkSpan.end - geometry.artworkSpan.start),
                  )}
                  stopColor="#fff"
                />
                <stop offset={1} stopColor="currentColor" />
              </linearGradient>
            </defs>
          )}
          {geometry.strokes.map((path, index) => (
            <path
              key={`line-${index}`}
              d={path}
              fill="none"
              stroke={paint}
              strokeWidth={2}
              strokeMiterlimit={10}
              strokeDasharray={geometry.dashes[index]}
            />
          ))}
          {geometry.fills.map((path, index) => (
            <path key={`join-${index}`} d={path} fill={paint} />
          ))}
        </svg>
      )}
      {children}
    </Component>
  );
}
