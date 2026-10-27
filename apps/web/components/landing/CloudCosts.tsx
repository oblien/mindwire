import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import SurfaceRoutes from "./SurfaceRoutes";
import styles from "./connected-sections.module.css";

const costs = [
  {
    id: "runtime",
    label: "Mindwire",
    title: "The agent layer. Open source.",
    detail:
      "The SDK and runtime have no Mindwire usage fee. Bring your tools, connect an agent, and build on your own infrastructure.",
    icon: "api-icon.svg",
    link: "/sdk",
    action: "Explore the SDK",
  },
  {
    id: "computer",
    label: "Your computer",
    title: "Your hardware. Or Cloud.",
    detail:
      "Run on a computer or server you own, or choose a monthly Cloud Computer. Hosting pays for the place where your agent works.",
    icon: "website-icon.svg",
    link: "/cloud#cloud-plans",
    action: "Compare Cloud plans",
  },
  {
    id: "agent",
    label: "Your agent",
    title: "Your account. Your usage.",
    detail:
      "Connect a supported provider subscription or API key. Your provider’s limits, availability, and usage charges apply separately.",
    icon: "agents-icon.svg",
    link: "/docs/guides/authentication",
    action: "Connect your account",
  },
];

export default function CloudCosts() {
  return (
    <SurfaceRoutes
      as="section"
      kind="cloudCosts"
      className={styles.costs}
      aria-labelledby="costs-title"
    >
      <div className={styles.costHeading} data-sprue="cost-heading">
        <h2 id="costs-title">A clear place for every cost.</h2>
        <p>Runtime, hosting, and AI usage. Three separate pieces.</p>
      </div>
      <div className={styles.costGrid}>
        {costs.map((cost) => (
          <article
            key={cost.id}
            className={styles.costCard}
            data-sprue={`cost-${cost.id}`}
          >
            <div className={styles.costLabel}>
              <span>{cost.label}</span>
              <img
                src={`/images/framework/${cost.icon}`}
                alt=""
                width={42}
                height={42}
                loading="lazy"
              />
            </div>
            <h3>{cost.title}</h3>
            <p>{cost.detail}</p>
            <Link href={cost.link}>
              {cost.action}
              <ArrowUpRight size={17} />
            </Link>
          </article>
        ))}
      </div>
    </SurfaceRoutes>
  );
}
