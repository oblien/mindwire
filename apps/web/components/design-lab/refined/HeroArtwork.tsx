"use client";

import { useEffect, useRef, useState, type CSSProperties } from "react";
import { Pause, Play } from "lucide-react";
import { createHeroMaterial } from "./hero-material";
import { heroSurfaces, type HeroVariant } from "./hero-surfaces";
import styles from "./hero-artwork.module.css";

export default function HeroArtwork({
  variant = "glass",
}: {
  variant?: HeroVariant;
}) {
  const host = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const renderer = useRef<ReturnType<typeof createHeroMaterial>>(null);
  const [paused, setPaused] = useState(false);
  const surface = heroSurfaces[variant];

  useEffect(() => {
    if (!canvas.current || !host.current) return;
    const material = createHeroMaterial(canvas.current, host.current, variant);
    renderer.current = material;
    return () => {
      material?.dispose();
      renderer.current = null;
    };
  }, [variant]);

  useEffect(() => {
    renderer.current?.setPaused(paused);
  }, [paused, variant]);

  return (
    <div
      ref={host}
      className={styles.artwork}
      data-hero-material
      data-hero-variant={variant}
      style={
        {
          "--hero-surface": `url("${surface.image}")`,
          "--hero-strength": surface.strength,
          "--hero-position": `${surface.anchor * 100}%`,
        } as CSSProperties
      }
    >
      <div className={styles.fallback} aria-hidden="true" />
      <canvas ref={canvas} className={styles.canvas} aria-hidden="true" />
      <button
        className={styles.motion}
        type="button"
        aria-label={
          paused ? "Resume background animation" : "Pause background animation"
        }
        title={
          paused ? "Resume background animation" : "Pause background animation"
        }
        onClick={() => setPaused((value) => !value)}
      >
        {paused ? <Play size={13} /> : <Pause size={13} />}
      </button>
    </div>
  );
}
