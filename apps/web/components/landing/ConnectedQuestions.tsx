import type { ComponentProps, ReactNode } from "react";
import Questions from "./Questions";
import SurfaceRoutes from "./SurfaceRoutes";
import styles from "./connected-sections.module.css";

export default function ConnectedQuestions({
  id,
  eyebrow,
  title,
  description,
  items,
}: {
  id: string;
  eyebrow: string;
  title: ReactNode;
  description?: string;
  items: ComponentProps<typeof Questions>["items"];
}) {
  return (
    <SurfaceRoutes
      as="section"
      kind="questions"
      className={styles.faq}
      aria-labelledby={id}
    >
      <div className={styles.faqIntro} data-sprue="questions-intro">
        <span>{eyebrow}</span>
        <h2 id={id}>{title}</h2>
        {description && <p>{description}</p>}
        <img
          src="/images/framework/canvas-motif.svg"
          alt=""
          width={105}
          height={29}
          loading="lazy"
        />
      </div>
      <div className={styles.faqBody} data-sprue="questions-list">
        <Questions items={items} />
      </div>
    </SurfaceRoutes>
  );
}
