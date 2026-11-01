import type { ReactNode } from "react";
import styles from "./interior.module.css";

type HeroMaterial = "precision" | "aperture" | "porcelain";
type HeroComposition = "open" | "panorama" | "split" | "editorial";

const materials: Record<HeroMaterial, string> = {
  precision: "sdk-precision-glass-v1",
  aperture: "app-satin-aperture-v1",
  porcelain: "cloud-porcelain-light-v1",
};

const designs: Record<
  string,
  { material: HeroMaterial; composition: HeroComposition }
> = {
  "sdk-title": { material: "precision", composition: "split" },
  "app-title": { material: "aperture", composition: "open" },
  "cloud-title": { material: "porcelain", composition: "panorama" },
  "developers-title": { material: "precision", composition: "open" },
  "setup-title": { material: "aperture", composition: "editorial" },
  "use-cases-title": { material: "aperture", composition: "panorama" },
  "sandbox-title": { material: "porcelain", composition: "open" },
  "changelog-title": { material: "precision", composition: "editorial" },
  "example-title": { material: "aperture", composition: "editorial" },
};

export default function PageHero({
  title,
  description,
  children,
  aside,
  id,
  artwork,
  connector,
  className,
}: {
  title: ReactNode;
  description: string;
  children?: ReactNode;
  aside?: ReactNode;
  id?: string;
  artwork?: ReactNode;
  connector?: string;
  className?: string;
}) {
  const design = designs[id ?? ""] ?? designs["app-title"];
  return (
    <section
      className={[styles.hero, className].filter(Boolean).join(" ")}
      data-page-hero
      data-material={design.material}
      data-composition={aside ? "split" : design.composition}
      data-sprue={connector}
      aria-labelledby={id}
    >
      <div className={styles.material} aria-hidden="true">
        <img
          src={`/images/design-lab/materials/${materials[design.material]}.webp`}
          alt=""
          width={1774}
          height={887}
          fetchPriority="high"
        />
      </div>
      <div className={styles.heroCopy}>
        <h1 id={id}>{title}</h1>
        <div className={styles.heroDetails}>
          <p>{description}</p>
          {children && <div className={styles.heroActions}>{children}</div>}
        </div>
      </div>
      {aside && <div className={styles.heroAside}>{aside}</div>}
      {artwork && (
        <div className={styles.artwork} aria-hidden="true">
          {artwork}
        </div>
      )}
    </section>
  );
}
