"use client";
import { useState } from "react";
import { useCases } from "@/lib/use-cases";
import UseCaseCard from "./UseCaseCard";
import styles from "./use-cases.module.css";

export default function UseCaseExplorer() {
  const [platform, setPlatform] = useState("All products");
  const cases = useCases.filter(
    (item) => platform === "All products" || item.platform === platform,
  );
  return (
    <section
      id="use-case-library"
      className={styles.library}
      aria-labelledby="library-title"
    >
      <div className={styles.libraryHeading}>
        <div>
          <span>Built into the product.</span>
          <h2 id="library-title">What could yours become?</h2>
        </div>
        <p>
          Six product blueprints. Explore the customer experience, try the
          result, and see how the integration fits together.
        </p>
      </div>
      <div className={styles.filters}>
        <div role="group" aria-label="Filter examples by platform">
          {["All products", "Desktop", "Cloud"].map((label) => (
            <button
              key={label}
              type="button"
              aria-pressed={platform === label}
              onClick={() => setPlatform(label)}
            >
              {label}
            </button>
          ))}
        </div>
        <span role="status">{cases.length} product examples</span>
      </div>
      <div className={styles.caseGrid}>
        {cases.map((item) => (
          <UseCaseCard key={item.slug} item={item} />
        ))}
      </div>
      <p className={styles.sampleNote}>
        Illustrative products with sample data. Each walkthrough shows a product
        you could build with Mindwire.
      </p>
    </section>
  );
}
