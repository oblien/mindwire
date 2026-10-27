import Link from "next/link";
import type { UseCase } from "@/lib/use-cases";
import ProductThumbnail from "../examples/ProductThumbnail";
import styles from "./use-cases.module.css";

export default function UseCaseCard({ item }: { item: UseCase }) {
  return (
    <Link href={`/use-cases/${item.slug}`} className={styles.caseCard}>
      <ProductThumbnail item={item} />
      <div className={styles.cardCopy}>
        <div className={styles.cardMeta}>
          <span>{item.category}</span>
          <span>{item.platform}</span>
        </div>
        <h3>{item.title}</h3>
        <p>{item.summary}</p>
        <span className={styles.cardLink}>
          Inside the product <span aria-hidden="true">↗</span>
        </span>
      </div>
    </Link>
  );
}
