"use client";

import { useEffect, useRef, useState } from "react";
import styles from "./framework-home.module.css";

/** React owns the canvas; the renderer owns only its GPU resources and listeners. */
export default function HeroTrail() {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [moving, setMoving] = useState(false);
  useEffect(() => {
    const element = canvas.current,
      host = element?.parentElement;
    if (!element || !host) return;
    let alive = true,
      timer: ReturnType<typeof setTimeout> | undefined;
    let renderer: { destroy: () => void } | null = null;
    const motion = matchMedia("(prefers-reduced-motion: reduce)");
    const moved = () => {
      setMoving(true);
      clearTimeout(timer);
      timer = setTimeout(() => setMoving(false), 1500);
    };
    const initialize = async () => {
      if (motion.matches) {
        renderer?.destroy();
        renderer = null;
        clearTimeout(timer);
        setMoving(false);
        return;
      }
      try {
        const { createCursorTrail } = await import("./effects/cursor-trail");
        if (alive && !motion.matches && !renderer)
          renderer = createCursorTrail(element, host, moved);
      } catch {
        // Keep the seed illustration visible when WebGL is unavailable.
      }
    };
    const lost = (event: Event) => {
      event.preventDefault();
      renderer?.destroy();
      renderer = null;
      clearTimeout(timer);
      setMoving(false);
    };
    void initialize();
    motion.addEventListener("change", initialize);
    element.addEventListener("webglcontextlost", lost);
    element.addEventListener("webglcontextrestored", initialize);
    return () => {
      alive = false;
      clearTimeout(timer);
      renderer?.destroy();
      motion.removeEventListener("change", initialize);
      element.removeEventListener("webglcontextlost", lost);
      element.removeEventListener("webglcontextrestored", initialize);
    };
  }, []);
  return (
    <>
      <canvas ref={canvas} className={styles.trailCanvas} aria-hidden="true" />
      <img
        className={styles.cursorTrail}
        data-moving={moving}
        src="/images/framework/cursor-trail.svg"
        alt=""
        width={290}
        height={322}
        fetchPriority="high"
      />
    </>
  );
}
