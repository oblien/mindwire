import { ArrowUpRight, Check, Info } from "@/components/site/InterfaceIcon";
import { type ComputerPricing, usd } from "@/lib/computer-pricing";
import { cloudDashboardUrl } from "@/lib/product";
import styles from "./cloud-plans.module.css";

const descriptions: Record<string, string> = {
  essential: "For smaller projects and everyday agent tasks.",
  pro: "More capacity for active projects and regular builds.",
  max: "Extra headroom for larger builds and demanding work.",
};
const included = [
  "Hardware-isolated virtual machine",
  "Persistent files and projects",
  "Access from your Mac or phone",
  "Bring your supported agent accounts",
];

export default function CloudPlans({ pricing }: { pricing: ComputerPricing }) {
  return (
    <section
      className={styles.pricing}
      aria-labelledby="cloud-plans-title"
      id="cloud-plans"
    >
      <div className={styles.heading}>
        <div>
          <h2 id="cloud-plans-title">A home that stays online.</h2>
          <p>One isolated computer for your files, tools, and projects.</p>
        </div>
        <span className={styles.interval}>Monthly · USD</span>
      </div>

      <div className={styles.plans}>
        {pricing.plans.map((plan) => (
          <article
            className={styles.plan}
            data-featured={plan.id === "pro"}
            key={plan.id}
          >
            <h3>{plan.name}</h3>
            <p className={styles.description}>
              {descriptions[plan.id] ??
                "An online workspace for your runtime and projects."}
            </p>
            <div className={styles.price}>
              <strong>{usd(plan.monthlyCents)}</strong>
              <span>/ month</span>
            </div>
            <dl className={styles.specs}>
              <div>
                <dt>Shared vCPU</dt>
                <dd>{plan.vcpus}</dd>
              </div>
              <div>
                <dt>Memory</dt>
                <dd>{plan.memoryMb / 1024} GB</dd>
              </div>
              <div>
                <dt>Storage</dt>
                <dd>{plan.diskGb} GB</dd>
              </div>
            </dl>
            <a
              className={styles.choose}
              href={`${cloudDashboardUrl}?plan=${encodeURIComponent(plan.id)}`}
            >
              {pricing.available ? `Choose ${plan.name}` : "View in Console"}
              <ArrowUpRight size={16} />
            </a>
          </article>
        ))}
      </div>

      <div className={styles.included}>
        <h3>Every plan includes</h3>
        <ul>
          {included.map((benefit) => (
            <li key={benefit}>
              <Check size={16} />
              {benefit}
            </li>
          ))}
        </ul>
      </div>
      <div className={styles.notes}>
        <p className={styles.usage}>
          Per computer. AI usage billed separately.
        </p>
        {!pricing.available && (
          <p className={styles.availability}>
            <Info size={16} />
            <span>
              {pricing.source === "catalog"
                ? "These are our published plans. Live checkout availability couldn’t be confirmed."
                : "Cloud checkout is not available right now."}{" "}
              <a href={cloudDashboardUrl}>
                Check availability in your account.
              </a>
            </span>
          </p>
        )}
        <p>
          CPU, memory and included storage are covered for the paid month. Model
          usage, transfer, retained storage and backups follow separate terms
          shown in checkout. Existing subscriptions keep their saved terms.
        </p>
      </div>
    </section>
  );
}
