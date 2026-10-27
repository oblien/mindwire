import type { ReactNode } from "react";
import styles from "./interior.module.css";

export default function PageHero({
  eyebrow,
  title,
  description,
  children,
  aside,
  id,
}: {
  eyebrow: string;
  title: ReactNode;
  description: string;
  children?: ReactNode;
  aside?: ReactNode;
  id?: string;
}) {
  return (
    <section
      className={styles.hero}
      data-split={Boolean(aside)}
      aria-labelledby={id}
    >
      <div className={styles.heroCopy}>
        <span className={styles.eyebrow}>{eyebrow}</span>
        <h1 id={id}>{title}</h1>
        <p>{description}</p>
        {children && <div className={styles.heroActions}>{children}</div>}
      </div>
      {aside && <div className={styles.heroAside}>{aside}</div>}
      <div className={styles.heroFoot} aria-hidden="true">
        <img
          src="/images/framework/canvas-motif.svg"
          alt=""
          width={105}
          height={29}
        />
        <span>MINDWIRE / {eyebrow.toUpperCase()}</span>
      </div>
    </section>
  );
}
