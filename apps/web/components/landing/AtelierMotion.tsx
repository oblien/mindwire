"use client";

import { useEffect, useRef, type ReactNode } from "react";

/** Subtle, frame-coalesced parallax; disabled for reduced motion and touch. */
export default function AtelierMotion({ children, className }: { children: ReactNode; className: string }) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const preference = matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0, x = 0, y = 0;
    const paint = () => {
      frame = 0;
      element.style.setProperty("--pointer-x", `${x}px`);
      element.style.setProperty("--pointer-y", `${y}px`);
    };
    const reset = () => { x = y = 0; cancelAnimationFrame(frame); paint(); };
    const move = (event: PointerEvent) => {
      if (preference.matches || event.pointerType === "touch") return;
      const rect = element.getBoundingClientRect();
      x = ((event.clientX - rect.left) / rect.width - .5) * 22;
      y = ((event.clientY - rect.top) / rect.height - .5) * 18;
      if (!frame) frame = requestAnimationFrame(paint);
    };
    element.addEventListener("pointermove", move, { passive: true });
    element.addEventListener("pointerleave", reset);
    preference.addEventListener("change", reset);
    return () => {
      cancelAnimationFrame(frame);
      element.removeEventListener("pointermove", move);
      element.removeEventListener("pointerleave", reset);
      preference.removeEventListener("change", reset);
    };
  }, []);
  return <div ref={root} className={className}>{children}</div>;
}
