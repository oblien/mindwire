"use client";

import { useEffect, useRef } from "react";
import type { Artwork } from "./effects/artwork";
import type { MotionRenderer } from "./effects/webgl";
import styles from "./framework-home.module.css";

export default function HeatformArt({
  variant,
  className = "",
}: {
  variant: Artwork;
  className?: string;
}) {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const element = canvas.current;
    if (!element) return;
    let renderer: MotionRenderer | null = null;
    let disposed = false,
      loading = false,
      visible = false,
      frame = 0,
      seconds = 0,
      previous = 0;
    const motion = matchMedia("(prefers-reduced-motion: reduce)");
    const stop = () => {
      cancelAnimationFrame(frame);
      frame = 0;
      previous = 0;
    };
    const draw = (now: number) => {
      frame = 0;
      if (
        !renderer ||
        disposed ||
        !visible ||
        motion.matches ||
        document.hidden
      )
        return;
      seconds += previous ? Math.min(0.1, (now - previous) / 1000) : 0;
      previous = now;
      renderer.draw(seconds);
      element.dataset.ready = "true";
      frame = requestAnimationFrame(draw);
    };
    const start = () => {
      if (renderer && visible && !motion.matches && !document.hidden && !frame)
        frame = requestAnimationFrame(draw);
    };
    const resize = () => {
      const dpr = Math.min(
        devicePixelRatio || 1,
        variant === "closing" ? 1 : 1.5,
      );
      renderer?.resize(element.clientWidth * dpr, element.clientHeight * dpr);
    };
    const initialize = async () => {
      if (loading || disposed || motion.matches || renderer) return;
      loading = true;
      try {
        const { createArtwork } = await import("./effects/artwork");
        if (disposed || motion.matches) return;
        const next = await createArtwork(element, variant);
        if (disposed) {
          next?.destroy();
          return;
        }
        renderer = next;
        resize();
        start();
      } catch {
        /* The reference poster remains visible if WebGL or the asset is unavailable. */
      } finally {
        loading = false;
      }
    };
    const intersection = new IntersectionObserver(
      ([entry]) => {
        visible = entry.isIntersecting;
        if (visible) {
          void initialize();
          start();
        } else stop();
      },
      { rootMargin: "200px" },
    );
    const observer = new ResizeObserver(resize);
    const updateMotion = () => {
      if (motion.matches) {
        stop();
        element.dataset.ready = "false";
      } else if (visible) {
        void initialize();
        start();
      }
    };
    const visibility = () => {
      if (document.hidden) stop();
      else start();
    };
    const lost = (event: Event) => {
      event.preventDefault();
      stop();
      element.dataset.ready = "false";
      renderer?.destroy();
      renderer = null;
    };
    const restored = () => {
      if (visible) void initialize();
    };
    intersection.observe(element);
    observer.observe(element);
    motion.addEventListener("change", updateMotion);
    document.addEventListener("visibilitychange", visibility);
    element.addEventListener("webglcontextlost", lost);
    element.addEventListener("webglcontextrestored", restored);
    return () => {
      disposed = true;
      stop();
      intersection.disconnect();
      observer.disconnect();
      motion.removeEventListener("change", updateMotion);
      document.removeEventListener("visibilitychange", visibility);
      element.removeEventListener("webglcontextlost", lost);
      element.removeEventListener("webglcontextrestored", restored);
      renderer?.destroy();
    };
  }, [variant]);
  return (
    <div className={`${styles.artwork} ${className}`}>
      <img
        src={`/images/framework/${variant}.webp`}
        alt=""
        width={variant === "closing" ? 1360 : 280}
        height={variant === "closing" ? 684 : 148}
        loading="lazy"
      />
      <canvas ref={canvas} aria-hidden="true" data-artwork={variant} />
    </div>
  );
}
