import type { UseCase } from "@/lib/use-cases";
import styles from "./product-thumbnail.module.css";

/** Small product surfaces, kept as HTML so the examples remain easy to customize. */
export default function ProductThumbnail({ item }: { item: UseCase }) {
  return (
    <div className={styles.thumbnail} data-kind={item.kind} aria-hidden="true">
      <div className={styles.top}>
        <strong>{item.brand.toLowerCase()}</strong>
        <span>
          {item.kind === "builder" ? "Site preview" : "Your workspace"}
        </span>
      </div>
      {item.kind === "research" && (
        <div className={styles.report}>
          <span>Research / Onboarding</span>
          <strong>
            What’s getting
            <br />
            in the way?
          </strong>
          <div>
            <span>Finding 01</span>
            <p>Teams need a clearer first step.</p>
            <em>3 interviews · View evidence ↗</em>
          </div>
        </div>
      )}
      {item.kind === "builder" && (
        <div className={styles.website}>
          <img
            src="/images/launch-demo/fieldwork-retreat.webp"
            alt=""
            loading="lazy"
          />
          <div>
            <span>Fieldwork / A coastal retreat</span>
            <strong>
              Room to
              <br />
              <i>think again.</i>
            </strong>
          </div>
        </div>
      )}
      {item.kind === "support" && (
        <div className={styles.support}>
          <span>Case #1048 / Export delivery</span>
          <strong>
            Found the cause.
            <br />
            Prepared the fix.
          </strong>
          <div>
            <span>Export file</span>
            <b>Ready</b>
          </div>
          <div>
            <span>Delivery address</span>
            <b>Verified</b>
          </div>
          <p>Review reply & resolve ↗</p>
        </div>
      )}
      {item.kind === "analytics" && (
        <div className={styles.analytics}>
          <div>
            <span>Revenue this quarter</span>
            <strong>$108,000</strong>
          </div>
          <div className={styles.bars}>
            {[45, 30, 20, 56, 31, 22, 68, 35, 25].map((height, index) => (
              <i key={index} style={{ height: `${height}%` }} />
            ))}
          </div>
          <footer>
            <span>January</span>
            <span>February</span>
            <span>March</span>
          </footer>
        </div>
      )}
      {item.kind === "learning" && (
        <div className={styles.learning}>
          <span>First chords / Lesson 01</span>
          <strong>
            A little theory.
            <br />
            <i>A lot of music.</i>
          </strong>
          <div className={styles.piano}>
            {["C", "D", "E", "F", "G", "A", "B", "C"].map((key, index) => (
              <span key={index}>{key}</span>
            ))}
          </div>
        </div>
      )}
      {item.kind === "commerce" && (
        <div className={styles.commerce}>
          <span>Linen collection / Review changes</span>
          <strong>
            A collection,
            <br />
            ready to go.
          </strong>
          {[
            "Everyday linen shirt",
            "Relaxed linen trousers",
            "Linen overshirt",
          ].map((title) => (
            <div key={title}>
              <span>{title}</span>
              <b>↗</b>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
