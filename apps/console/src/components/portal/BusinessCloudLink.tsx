import { ArrowUpRight } from "lucide-react";
import styles from "./portal.module.css";

export function BusinessCloudLink() {
  return (
    <aside className={styles.businessLink} aria-label="Cloud for business">
      <div>
        <strong>Building for your customers?</strong>
        <p>Mindwire Cloud is for personal use. For business products and infrastructure that scales, build with Mindwire and Oblien.</p>
      </div>
      <a href="https://oblien.com/" target="_blank" rel="noreferrer">
        Explore Oblien <ArrowUpRight size={17} />
      </a>
    </aside>
  );
}
