import type { Metadata } from "next";
import Link from "next/link";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronRight,
  Cloud,
  Code2,
  Cpu,
  HardDrive,
  Laptop,
  MemoryStick,
  Sparkles,
} from "lucide-react";
import styles from "@/components/marketing/marketing.module.css";
import Corners from "@/components/Corners";
import { cloudPricingUrl, cloudRatesUrl, downloadUrl } from "@/lib/product";
import { getWorkspacePricing, resource, usd } from "@/lib/workspace-pricing";

export const metadata: Metadata = {
  title: "Workspace pricing — Mindwire",
  description:
    "Run the open-source Mindwire service on your own computer or server, or choose an Oblien cloud workspace. Compare current plans, resources, and usage rates.",
  alternates: { canonical: "/pricing/" },
};

export const revalidate = 3600;

const questions = [
  {
    question: "Are these plans for the iPhone app or for workspaces?",
    answer:
      "These are Oblien cloud account plans and resource allowances. They pay for the infrastructure where your agents run. Connecting your own computer or server uses the open-source Mindwire service without a runtime license fee.",
  },
  {
    question: "Is AI usage included?",
    answer:
      "No. Sign in to your agent with a supported subscription or supply your own API key. Your provider handles model charges and usage limits separately from workspace resources.",
  },
  {
    question: "Does one plan cover multiple workspaces?",
    answer:
      "Yes. Included credits and running resource pools are shared by workspaces in your Oblien account. Every workspace also has its own size limit. Creating a workspace does not grant another copy of the plan’s allowance.",
  },
  {
    question: "Can I switch from my computer to a cloud workspace?",
    answer:
      "Yes. On supported macOS and Linux workspaces, Mindwire can transfer a project and selected Codex or Claude history, then bring compatible changes back later. Finish active work first and authenticate the agent on the destination. The original copy is preserved.",
  },
  {
    question: "Do I need Oblien to connect my computer?",
    answer:
      "No. The default connection uses a free Cloudflare tunnel with encrypted address recovery. You can use your own VPN, tunnel provider, or self-hosted address directory. Paid third-party services follow their own pricing.",
  },
];

export default async function Pricing() {
  const pricing = await getWorkspacePricing();

  return (
    <div className={styles.marketing}>
      <section className={`${styles.wrap} ${styles.pageHero}`}>
        <span className={styles.kicker}>Workspace pricing</span>
        <h1 className={styles.pageTitle}>
          Your workspace.
          <br />
          <span>Your call.</span>
        </h1>
        <p>
          Bring your own machine, or give your agent one in the cloud. Choose
          what fits the way you work.
        </p>
      </section>

      <section className={styles.wrap} aria-labelledby="own-title">
        <div className={styles.ownPricing}>
          <Corners />
          <div>
            <span className={styles.ownPricingLabel}>
              <Laptop size={22} /> Your computer or server
            </span>
            <h2 id="own-title">
              $0 <span>Mindwire runtime fee</span>
            </h2>
            <p>
              Run the open-source service on hardware you already own. You cover
              your infrastructure and agent usage.
            </p>
            <Link href="/get-started#computer" className={styles.textButton}>
              Connect your own machine <ArrowRight size={16} />
            </Link>
          </div>
          <ul>
            <li>
              <Check size={16} /> Your existing projects and agent accounts
            </li>
            <li>
              <Check size={16} /> Chats, files, Git, and a native terminal
            </li>
            <li>
              <Check size={16} /> Computer pairing or server access over SSH
            </li>
            <li>
              <Check size={16} /> Open-source service, SDKs, and Console
            </li>
          </ul>
        </div>
      </section>

      <section
        className={`${styles.wrap} ${styles.cloudPricing}`}
        aria-labelledby="cloud-title"
      >
        <div className={styles.pricingIntro}>
          <div>
            <span className={styles.kicker}>
              <Cloud size={16} /> Cloud workspaces, powered by Oblien
            </span>
            <h2 id="cloud-title" className={styles.sectionTitle}>
              A place for your agents.
              <br />
              <span>Even when your laptop’s closed.</span>
            </h2>
            <p>
              Choose an Oblien account plan. Credits cover workspace usage;
              resources are shared across your running workspaces.
            </p>
          </div>
          <a href={cloudPricingUrl} className={styles.textButton}>
            All Oblien plans <ArrowUpRight size={15} />
          </a>
        </div>
        {pricing ? (
          <>
            <div className={styles.pricingGrid}>
              <Corners />
              {pricing.plans.map((plan) => (
                <article
                  key={plan.family}
                  className={`${styles.planCard} ${plan.family === "pro" ? styles.planFeatured : ""}`}
                >
                  <h3>{plan.name}</h3>
                  <div className={styles.planPrice}>
                    {usd(plan.price)}
                    <span>/ month</span>
                  </div>
                  <p className={styles.planCredits}>
                    {plan.credits_per_cycle.toLocaleString("en-US")} credits
                    included monthly
                  </p>
                  <div className={styles.planSpecs}>
                    <span>Shared running resource pool</span>
                    <div>
                      <Cpu size={15} />
                      {resource(plan.running_pool.cpus, "vCPU")}
                    </div>
                    <div>
                      <MemoryStick size={15} />
                      {resource(plan.running_pool.memory_mb, "GiB")} memory
                    </div>
                    <div>
                      <HardDrive size={15} />
                      {resource(plan.running_pool.disk_size_mb, "GiB")} disk
                    </div>
                  </div>
                  <a href={cloudPricingUrl} className={styles.secondaryButton}>
                    View on Oblien <ArrowUpRight size={14} />
                  </a>
                </article>
              ))}
            </div>
            <p className={styles.pricingNote}>
              Prices in USD, from Oblien’s current pricing service. These are
              account allowances, with usage deducted from credits. Image
              requirements, per-workspace limits, and available shared resources
              determine what you can run. Model usage is separate.
            </p>
            <details className={styles.pricingDetails}>
              <summary>
                Compare individual workspace limits <ChevronRight size={17} />
              </summary>
              <div
                className={styles.comparisonScroll}
                tabIndex={0}
                role="region"
                aria-label="Workspace limit comparison"
              >
                <table className={styles.comparisonTable}>
                  <caption>
                    Each workspace is limited by these ceilings and the
                    remaining resources in your account’s running pool.
                  </caption>
                  <thead>
                    <tr>
                      <th scope="col">Per workspace</th>
                      {pricing.plans.map((plan) => (
                        <th scope="col" key={plan.family}>
                          {plan.name}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    <tr>
                      <th scope="row">Maximum CPU</th>
                      {pricing.plans.map((plan) => (
                        <td key={plan.family}>
                          {resource(plan.max_workspace.cpus, "vCPU")}
                        </td>
                      ))}
                    </tr>
                    <tr>
                      <th scope="row">Maximum memory</th>
                      {pricing.plans.map((plan) => (
                        <td key={plan.family}>
                          {resource(plan.max_workspace.memory_mb, "GiB")}
                        </td>
                      ))}
                    </tr>
                    <tr>
                      <th scope="row">Maximum disk</th>
                      {pricing.plans.map((plan) => (
                        <td key={plan.family}>
                          {resource(plan.max_workspace.disk_size_mb, "GiB")}
                        </td>
                      ))}
                    </tr>
                  </tbody>
                </table>
              </div>
            </details>
          </>
        ) : (
          <div className={styles.pricingUnavailable} role="status">
            <h3>Current cloud prices are temporarily unavailable.</h3>
            <p>
              Check Oblien for the latest plans and resource rates. You can
              still connect your own computer or server.
            </p>
            <a href={cloudPricingUrl} className={styles.secondaryButton}>
              See prices on Oblien <ArrowUpRight size={15} />
            </a>
          </div>
        )}

        <div className={styles.costBreakdown}>
          <h2>Know what you’re paying for.</h2>
          <div className={styles.costGrid}>
            <article>
              <h3>
                <Code2 size={19} /> Mindwire service
              </h3>
              <p>
                The open-source runtime has no license fee. Run it on your own
                machine, server, or cloud workspace.
              </p>
            </article>
            <article>
              <h3>
                <Cloud size={19} /> Workspace resources
              </h3>
              <p>
                Oblien credits cover cloud resource usage. Your own hardware or
                hosting provider has its own costs.
              </p>
            </article>
            <article>
              <h3>
                <Sparkles size={19} /> Your coding agent
              </h3>
              <p>
                Use a supported agent subscription or API key. Model usage is
                billed by that provider, with its own limits.
              </p>
            </article>
          </div>
        </div>
        {pricing && (
          <div className={styles.rates}>
            <div className={styles.ratesHeader}>
              <h3>Current metered usage rates</h3>
              <a href={cloudRatesUrl} className={styles.textButton}>
                Open Oblien’s calculator <ArrowUpRight size={14} />
              </a>
            </div>
            <dl className={styles.rateGrid}>
              <div>
                <dt>CPU time</dt>
                <dd>
                  {usd(
                    (pricing.rates.cpu_per_min * 60) / pricing.creditsPerDollar,
                  )}{" "}
                  <span>/ vCPU-hour</span>
                </dd>
              </div>
              <div>
                <dt>Measured memory</dt>
                <dd>
                  {usd(
                    (pricing.rates.memory_per_gb_min * 60) /
                      pricing.creditsPerDollar,
                  )}{" "}
                  <span>/ GiB-hour</span>
                </dd>
              </div>
              <div>
                <dt>Network in + out</dt>
                <dd>
                  {usd(pricing.rates.network_per_gb / pricing.creditsPerDollar)}{" "}
                  <span>/ GB</span>
                </dd>
              </div>
            </dl>
            <p>
              Monthly resource discounts apply; actual charges depend on usage.{" "}
              {pricing.creditsPerDollar.toLocaleString("en-US")} credits = $1.
              Account limits and the active rate card are managed by Oblien. See
              its calculator for a full estimate.
            </p>
          </div>
        )}
      </section>

      <section
        className={`${styles.wrap} ${styles.section} ${styles.faqSection}`}
        aria-labelledby="pricing-questions"
      >
        <div>
          <span className={styles.kicker}>Simple choices</span>
          <h2 id="pricing-questions" className={styles.sectionTitle}>
            A little
            <br />
            <span>more detail.</span>
          </h2>
        </div>
        <div className={styles.faqList}>
          {questions.map((item) => (
            <details key={item.question}>
              <summary>
                {item.question}
                <ChevronRight size={18} />
              </summary>
              <p>{item.answer}</p>
            </details>
          ))}
        </div>
      </section>
      <section className={`${styles.wrap} ${styles.finalCta}`}>
        <div className={styles.ctaPanel}>
          <Corners />
          <span className={styles.kicker}>Your agents, your way</span>
          <h2>
            Start with the computer
            <br />
            you already have.
          </h2>
          <div className={styles.actions}>
            <Link href="/get-started" className={styles.primaryButton}>
              Get connected <ArrowRight size={16} />
            </Link>
            <Link href={downloadUrl} className={styles.textButton}>
              Download app <ArrowUpRight size={16} />
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}
