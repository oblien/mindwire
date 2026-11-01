import type { Metadata } from "next";
import PageHero from "@/components/landing/PageHero";
import Link from "next/link";
import { ArrowUpRight, Laptop } from "@/components/site/InterfaceIcon";
import CloudCosts from "@/components/landing/CloudCosts";
import CloudPlans from "@/components/landing/CloudPlans";
import ConnectedQuestions from "@/components/landing/ConnectedQuestions";
import CloudInfrastructure from "@/components/landing/CloudInfrastructure";
import { getComputerPricing } from "@/lib/computer-pricing";
import { cloudDashboardUrl } from "@/lib/product";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/product.module.css";

export const metadata: Metadata = {
  title: "Cloud & Pricing — MindWire",
  description:
    "Personal Cloud Computers for your projects in MindWire. Compare monthly plans and pick up your work anywhere. Building for customers? Explore scalable business infrastructure on Oblien.",
  alternates: { canonical: "/cloud/" },
};
export const revalidate = 60;

export default async function CloudPage() {
  const pricing = await getComputerPricing();
  return (
    <div className={base.landing}>
      <PageHero
        id="cloud-title"
        title={
          <>
            Your work.
            <br />
            Always with you.
          </>
        }
        description="MindWire Cloud is for personal use. Give your projects an online computer, keep your files together, and pick up the work from your Mac or phone. One monthly plan per computer, powered by Oblien."
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
              Use MindWire with your Mac or a server you already own. Connect
              your devices and work with your supported agent accounts.
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
        <CloudPlans pricing={pricing} />
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
            question: "Who is MindWire Cloud for?",
            answer: (
              <>
                MindWire Cloud is for personal use: your projects, your files,
                and work you manage through the MindWire app. For business
                products, customer workspaces, or infrastructure at scale, use
                the MindWire SDK with <a href="https://oblien.com/">Oblien</a>.
              </>
            ),
          },
          {
            question: "What runs underneath MindWire Cloud?",
            answer:
              "Oblien provides the virtual machines, storage, and networking. MindWire brings that infrastructure into your personal workspace. Each Cloud Computer gets its own VM and the resource allowance shown in its plan.",
          },
          {
            question: "How is my agent’s workspace isolated?",
            answer:
              "Oblien uses hardware virtualization to separate the machines. Each Linux VM has its own kernel, filesystem, and network, with the resource limits shown in your plan.",
          },
          {
            question: "Do I need Cloud to use MindWire?",
            answer:
              "No. Run MindWire on your own Mac, Windows or Linux computer, or connect a server. Cloud is an optional home for work that needs to stay online when your personal computer is off.",
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
              "Yes. Manage the subscription in your MindWire account or the app. Canceling renewal keeps the paid period intact. It does not delete the computer or its files; retained storage follows the terms of your purchase.",
          },
          {
            question: "Can I bring my existing cloud account?",
            answer:
              "Use the connection flow in your MindWire account to link a supported existing account. Linking keeps existing computers, files and paid terms; it does not replace an existing subscription.",
          },
        ]}
      />
      <section
        className={styles.finalInvite}
        aria-labelledby="cloud-invite-title"
      >
        <h2 id="cloud-invite-title">A computer for your next idea.</h2>
        <p>
          Choose a personal plan, connect from MindWire, and keep your projects
          moving.
        </p>
        <a href={cloudDashboardUrl} className={base.primaryButton}>
          Open Cloud Console <ArrowUpRight size={18} />
        </a>
        <span>Powered by Oblien. Connected through MindWire.</span>
      </section>
    </div>
  );
}
