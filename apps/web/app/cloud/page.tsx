import type { Metadata } from "next";
import PageHero from "@/components/landing/PageHero";
import Link from "next/link";
import {
  ArrowUpRight,
  CalendarDays,
  Check,
  Cloud,
  Laptop,
  Info,
} from "lucide-react";
import CloudCosts from "@/components/landing/CloudCosts";
import ConnectedQuestions from "@/components/landing/ConnectedQuestions";
import CloudInfrastructure from "@/components/landing/CloudInfrastructure";
import { getComputerPricing, usd } from "@/lib/computer-pricing";
import { cloudDashboardUrl } from "@/lib/product";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Cloud & Pricing — Mindwire",
  description:
    "Personal Cloud Computers for your projects in Mindwire. Compare monthly plans and pick up your work anywhere. Building for customers? Explore scalable business infrastructure on Oblien.",
  alternates: { canonical: "/cloud/" },
};
export const revalidate = 60;

const descriptions: Record<string, string> = {
  essential: "For smaller projects and everyday agent tasks.",
  pro: "More capacity for active projects and regular builds.",
  max: "Extra headroom for larger builds and demanding work.",
};

export default async function CloudPage() {
  const pricing = await getComputerPricing();
  return (
    <div className={base.landing}>
      <PageHero
        eyebrow="Cloud for personal use"
        id="cloud-title"
        title={
          <>
            Your work.
            <br />
            Always with you.
          </>
        }
        description="Mindwire Cloud is for personal use. Give your projects an online computer, keep your files together, and pick up the work from your Mac or phone. One monthly plan per computer, powered by Oblien."
      >
        <Link href="#cloud-plans" className={base.primaryButton}>
          Explore personal plans <ArrowUpRight size={16} />
        </Link>
        <a href="https://oblien.com/" className={base.secondaryButton}>
          Cloud for business <ArrowUpRight size={16} />
        </a>
      </PageHero>
      <div className={styles.pageWrap}>
        <section
          className={styles.freePlan}
          aria-labelledby="own-computer-title"
        >
          <Laptop size={44} strokeWidth={1.4} />
          <div>
            <h2 id="own-computer-title">Your own computer works, too.</h2>
            <p>
              Use Mindwire with your Mac or a server you already own.
              Connect your devices and work with your supported agent accounts.
            </p>
          </div>
          <div className={styles.freePrice}>
            <strong>$0</strong>
            <span>With your own computer</span>
          </div>
          <Link href="/app#download" className={base.primaryButton}>
            Get the app <ArrowUpRight size={16} />
          </Link>
        </section>
        <section aria-labelledby="cloud-plans-title" id="cloud-plans">
          <div className={styles.cloudHeading}>
            <div>
              <h2 id="cloud-plans-title">A home that stays online.</h2>
              <p>
                Personal plans. One isolated computer for your files, tools, and projects.
              </p>
            </div>
            <span className={styles.billingInterval}>
              <CalendarDays size={15} />
              Monthly · USD
            </span>
          </div>
          <div className={styles.planGrid}>
            {pricing.plans.map((plan, i) => (
              <article
                className={styles.planCard}
                data-tone={
                  plan.id === "pro" ? "blue" : i === 2 ? "peach" : "mint"
                }
                key={plan.id}
              >
                <div className={styles.planName}>
                  <h3>{plan.name}</h3>
                  <Cloud size={34} strokeWidth={1.4} />
                </div>
                <p className={styles.planDescription}>
                  {descriptions[plan.id] ??
                    "An online workspace for your runtime and projects."}
                </p>
                <div className={styles.planPrice}>
                  <strong>{usd(plan.monthlyCents)}</strong>
                  <span>/ month</span>
                </div>
                <p className={styles.planPriceNote}>
                  Per computer. AI usage billed separately.
                </p>
                <div className={styles.planSpecs}>
                  <div>
                    <strong>{plan.vcpus}</strong>
                    <small>Shared vCPU</small>
                  </div>
                  <div>
                    <strong>{plan.memoryMb / 1024} GB</strong>
                    <small>Memory</small>
                  </div>
                  <div>
                    <strong>{plan.diskGb} GB</strong>
                    <small>Storage</small>
                  </div>
                </div>
                <ul className={styles.planBenefits}>
                  <li>
                    <Check size={15} />
                    Hardware-isolated virtual machine
                  </li>
                  <li>
                    <Check size={15} />
                    Persistent files and projects
                  </li>
                  <li>
                    <Check size={15} />
                    Access from your Mac or phone
                  </li>
                  <li>
                    <Check size={15} />
                    Bring your supported agent accounts
                  </li>
                </ul>
                <a
                  className={
                    plan.id === "pro"
                      ? base.primaryButton
                      : base.secondaryButton
                  }
                  href={`${cloudDashboardUrl}?plan=${encodeURIComponent(plan.id)}`}
                >
                  {pricing.available
                    ? `Choose ${plan.name}`
                    : "View in Console"}
                  <ArrowUpRight size={16} />
                </a>
              </article>
            ))}
          </div>
          {!pricing.available && (
            <div className={styles.availabilityNote}>
              <Info size={16} />
              <span>
                {pricing.source === "catalog"
                  ? "These are our published plans. Live checkout availability couldn’t be confirmed."
                  : "Cloud checkout is not available right now."}{" "}
                <a href={cloudDashboardUrl}>
                  Check availability in your account.
                </a>
              </span>
            </div>
          )}
          <p className={styles.pricingNote}>
            CPU, memory and included storage are covered for the paid month.
            Model usage, transfer, retained storage and backups follow separate
            terms shown in checkout. Existing subscriptions keep their saved
            terms.
          </p>
        </section>
      </div>
      <CloudInfrastructure />
      <CloudCosts />
      <ConnectedQuestions
        id="cloud-questions"
        eyebrow="Good to know"
        title={
          <>
            Less guesswork.
            <br />
            More getting on.
          </>
        }
        description="A personal computer in the cloud, with clear monthly costs. Your agent account stays separate."
        items={[
          {
            question: "Who is Mindwire Cloud for?",
            answer: <>Mindwire Cloud is for personal use: your projects, your files, and work you manage through the Mindwire app. For business products, customer workspaces, or infrastructure at scale, use the Mindwire SDK with <a href="https://oblien.com/">Oblien</a>.</>,
          },
          {
            question: "What runs underneath Mindwire Cloud?",
            answer:
              "Oblien provides the virtual machines, storage, and networking. Mindwire brings that infrastructure into your personal workspace. Each Cloud Computer gets its own VM and the resource allowance shown in its plan.",
          },
          {
            question: "How is my agent’s workspace isolated?",
            answer:
              "Oblien uses hardware virtualization to separate the machines. Each Linux VM has its own kernel, filesystem, and network, with the resource limits shown in your plan.",
          },
          {
            question: "Do I need Cloud to use Mindwire?",
            answer:
              "No. Run Mindwire on your own Mac, Windows or Linux computer, or connect a server. Cloud is an optional home for work that needs to stay online when your personal computer is off.",
          },
          {
            question: "Does a plan include Codex or Claude usage?",
            answer:
              "No. Connect a supported account or API key for your agent. Your provider’s subscription, rate limits and usage charges remain separate from the computer subscription.",
          },
          {
            question: "Can I have more than one personal computer?",
            answer:
              "Yes. Keep different personal projects on separate Cloud Computers, subject to available capacity. Each computer has its own monthly subscription and resource limits. For a business fleet or workspaces for your customers, go directly to Oblien.",
          },
          {
            question: "Can I stop the renewal?",
            answer:
              "Yes. Manage the subscription in your Mindwire account or the app. Canceling renewal keeps the paid period intact. It does not delete the computer or its files; retained storage follows the terms of your purchase.",
          },
          {
            question: "Can I bring my existing cloud account?",
            answer:
              "Use the connection flow in your Mindwire account to link a supported existing account. Linking keeps existing computers, files and paid terms; it does not replace an existing subscription.",
          },
        ]}
      />
      <section
        className={styles.finalInvite}
        aria-labelledby="cloud-invite-title"
      >
        <h2 id="cloud-invite-title">A computer for your next idea.</h2>
        <p>
          Choose a personal plan, connect from Mindwire, and keep your projects moving.
        </p>
        <a href={cloudDashboardUrl} className={base.primaryButton}>
          Open Cloud Console <ArrowUpRight size={18} />
        </a>
        <span>Powered by Oblien. Connected through Mindwire.</span>
      </section>
    </div>
  );
}
