"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { steppedWindowClipPath } from "./geometry/sprue-geometry";
import styles from "./framework-home.module.css";

type Kind = "hero" | "diagram" | "connect" | "closing" | "footer";
type Shape = { width: number; height: number; path: string };

/** A single continuous silhouette, including the inward rounded corners. */
export default function WindowShape({
  kind,
  color = "var(--mw-surface)",
}: {
  kind: Kind;
  color?: string;
}) {
  const svg = useRef<SVGSVGElement>(null);
  const [shape, setShape] = useState<Shape | null>(null);
  useLayoutEffect(() => {
    const parent = svg.current?.parentElement;
    if (!parent) return;
    const measure = () => {
      const rect = parent.getBoundingClientRect(),
        dpr = devicePixelRatio || 1;
      const snap = (value: number) => Math.round(value * dpr) / dpr;
      const width = snap(rect.width),
        height = snap(rect.height);
      if (!width || !height) return;
      const mobile = innerWidth < 768;
      const inset =
        kind === "hero"
          ? innerWidth < 768
            ? 40
            : innerWidth < 1280
              ? 48
              : 67
          : kind === "footer"
            ? mobile
              ? 40
              : 68
            : kind === "connect"
              ? Math.min(95, width * 0.15)
              : 43;
      const part = (x: number, y: number, w: number, h: number) => ({
        x: snap(x),
        y: snap(y),
        w: snap(w),
        h: snap(h),
      });
      const strip =
        kind === "hero"
          ? 69
          : kind === "footer"
            ? 61
            : kind === "connect"
              ? 66
              : 43;
      const parts =
        (kind === "diagram" && innerWidth < 1024) ||
        (kind === "closing" && mobile)
          ? [part(0, 0, width, height)]
          : kind === "hero"
            ? [
                part(0, 0, width, height - strip),
                part(inset, height - strip, width - 2 * inset, strip),
              ]
            : kind === "footer"
              ? [
                  part(inset, 0, width - 2 * inset, strip),
                  part(0, strip, width, height - strip),
                ]
              : kind === "connect"
                ? [
                    part(0, 0, width - inset, strip),
                    part(0, strip, width, height - strip),
                  ]
                : kind === "closing"
                  ? [
                      part(inset, 0, width - 2 * inset, strip),
                      part(0, strip, width, height - strip),
                    ]
                  : [
                      part(inset, 0, width - 2 * inset, strip),
                      part(0, strip, width, height - 2 * strip),
                      part(inset, height - strip, width - 2 * inset, strip),
                    ];
      const path = steppedWindowClipPath(parts) ?? "";
      parent.style.setProperty(
        "--window-clip",
        path ? `path("${path}")` : "inset(0 round 20px)",
      );
      setShape({ width, height, path });
    };
    const observer = new ResizeObserver(measure);
    observer.observe(parent);
    window.addEventListener("resize", measure);
    measure();
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
      parent.style.removeProperty("--window-clip");
    };
  }, [kind]);
  return (
    <svg
      ref={svg}
      className={styles.windowShape}
      aria-hidden="true"
      width={shape?.width}
      height={shape?.height}
      viewBox={shape ? `0 0 ${shape.width} ${shape.height}` : undefined}
    >
      {shape?.path ? (
        <path d={shape.path} fill={color} />
      ) : (
        <rect width="100%" height="100%" rx="20" fill={color} />
      )}
    </svg>
  );
}
