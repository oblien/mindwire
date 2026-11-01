import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "@/components/site/InterfaceIcon";
import PageHero from "@/components/landing/PageHero";
import UseCaseExplorer from "@/components/landing/UseCaseExplorer";
import ConnectionPatterns from "@/components/examples/ConnectionPatterns";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/use-cases.module.css";

export const metadata: Metadata = {
  title: "MindWire examples — Your product, powered by agents",
  description:
    "Explore business applications with agents built in. Discover existing desktop accounts, connect a cloud workspace, and let users create and act inside your product.",
  alternates: { canonical: "/use-cases/" },
};

export default function UseCasesPage() {
  return (
    <div className={base.landing}>
      <PageHero
        id="use-cases-title"
        title={
          <>
            Your product.
            <br />
            Powered by agents.
          </>
        }
        description="A research app that writes the report. A builder that creates the website. A help desk that fixes the problem. Put the best agents to work inside the product your customers already use."
      >
        <Link href="#use-case-library" className={base.primaryButton}>
          Explore the products <ArrowDown size={16} />
        </Link>
        <Link href="/sdk" className={base.secondaryButton}>
          Build with MindWire <ArrowUpRight size={16} />
        </Link>
      </PageHero>
      <ConnectionPatterns />
      <UseCaseExplorer />
      <section className={styles.closing}>
        <h2>
          Your interface.
          <br />
          Their agent. Real work.
        </h2>
        <div>
          <p>
            Your application starts the conversation, supplies the context, and
            decides what the agent can do. The result lives where your customers
            work.
          </p>
          <Link href="/docs/guides/quickstart">
            Start your integration <ArrowUpRight size={16} />
          </Link>
        </div>
      </section>
    </div>
  );
}
