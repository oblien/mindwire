"use client";

import { useEffect, useId, useMemo, useRef } from "react";
import { lineworkArt, type LineworkVariant } from "./linework-art";
import styles from "./animated-linework.module.css";

export default function AnimatedLinework({
  variant,
  label,
}: {
  variant: LineworkVariant;
  label?: string;
}) {
  const artwork = useRef<SVGSVGElement>(null);
  const hatchId = "linework-" + useId().replace(/:/g, "");
  const markup = useMemo(
    () =>
      lineworkArt[variant].markup
        .replaceAll('id="hatch"', `id="${hatchId}"`)
        .replaceAll("url(#hatch)", `url(#${hatchId})`),
    [variant, hatchId],
  );

  useEffect(() => {
    const element = artwork.current;
    if (!element) return;
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let visible = false;
    const sync = () => {
      element.dataset.running = String(
        visible && !document.hidden && !reducedMotion.matches,
      );
    };
    const observer = new IntersectionObserver(
      ([entry]) => {
        visible = entry.isIntersecting;
        sync();
      },
      { threshold: 0.05 },
    );
    observer.observe(element);
    reducedMotion.addEventListener("change", sync);
    document.addEventListener("visibilitychange", sync);
    sync();
    return () => {
      observer.disconnect();
      reducedMotion.removeEventListener("change", sync);
      document.removeEventListener("visibilitychange", sync);
    };
  }, [variant]);

  return (
    <svg
      ref={artwork}
      className={styles.art}
      viewBox="0 0 400 400"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.45"
      strokeLinecap="round"
      strokeLinejoin="round"
      data-linework={variant}
      data-running="false"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      focusable="false"
      // Geometry is a static, locally authored SVG string, never user input.
      dangerouslySetInnerHTML={{ __html: markup }}
    />
  );
}
