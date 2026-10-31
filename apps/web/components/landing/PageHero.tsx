import type { ReactNode } from "react";
import SiteIllustration, {
  type SiteIllustrationKind,
} from "../site/SiteIllustration";
import styles from "./interior.module.css";

const illustrations: Record<string, SiteIllustrationKind> = {
  "cloud-title": "cloud",
  "app-title": "devices",
  "use-cases-title": "product",
  "setup-title": "devices",
  "get-started-title": "devices",
  "developers-title": "code",
  "dev-title": "code",
  "changelog-title": "context",
};

export default function PageHero({
  eyebrow,
  title,
  description,
  children,
  aside,
  id,
  artwork,
  caption,
  connector,
  className,
}: {
  eyebrow: string;
  title: ReactNode;
  description: string;
  children?: ReactNode;
  aside?: ReactNode;
  id?: string;
  artwork?: ReactNode;
  caption?: string;
  connector?: string;
  className?: string;
}) {
  return (
    <section
      className={[styles.hero, className].filter(Boolean).join(" ")}
      data-split={Boolean(aside)}
      data-illustrated={!aside}
      data-sprue={connector}
      aria-labelledby={id}
    >
      {!aside && (
        <div className={styles.artwork} aria-hidden="true">
          {artwork ?? (
            <SiteIllustration kind={illustrations[id ?? ""] ?? "identity"} />
          )}
        </div>
      )}
      <div className={styles.heroCopy}>
        <span className={styles.eyebrow}>{eyebrow}</span>
        <h1 id={id}>{title}</h1>
        <p>{description}</p>
        {children && <div className={styles.heroActions}>{children}</div>}
      </div>
      {aside && <div className={styles.heroAside}>{aside}</div>}
      <div className={styles.heroFoot} aria-hidden="true">
        <span>Mindwire</span>
        <span>{caption ?? eyebrow}</span>
      </div>
    </section>
  );
}
