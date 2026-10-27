"use client";

import { useLayoutEffect, useRef, useState, type HTMLAttributes } from "react";
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
};

/** Connect measured DOM surfaces with the reference's rounded routing geometry. */
export default function SurfaceRoutes({
  kind,
  as: Component = "div",
  routeColor,
  children,
  className = "",
  ...props
}: Props) {
  const root = useRef<HTMLElement>(null);
  const [geometry, setGeometry] = useState<Geometry | null>(null);
  const definition = routes[kind] as Definition;
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
      setGeometry(
        computeSprueGeometry(
          layout,
          definition,
          (query: string) =>
            media.get(query)?.matches ?? matchMedia(query).matches,
          devicePixelRatio || 1,
        ),
      );
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
  }, [definition]);
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
          className={styles.surfaceRoutes}
          style={{
            left: geometry.corner[0],
            top: geometry.corner[1],
            zIndex: geometry.overlay ? 2 : -1,
            color:
              routeColor ?? (definition.tone === "dark" ? "#d9d9d9" : "#fff"),
          }}
          width={geometry.W}
          height={geometry.H}
          viewBox={`0 0 ${geometry.W} ${geometry.H}`}
        >
          {geometry.strokes.map((path, index) => (
            <path
              key={`line-${index}`}
              d={path}
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
              strokeMiterlimit={10}
              strokeDasharray={geometry.dashes[index]}
            />
          ))}
          {geometry.fills.map((path, index) => (
            <path key={`join-${index}`} d={path} fill="currentColor" />
          ))}
        </svg>
      )}
      {children}
    </Component>
  );
}
