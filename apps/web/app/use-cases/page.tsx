import type { Metadata } from "next";
import PageHero from "@/components/landing/PageHero";
import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "lucide-react";
import UseCaseExplorer from "@/components/landing/UseCaseExplorer";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/use-cases.module.css";

export const metadata: Metadata = {
  title: "Mindwire examples — What could your product do?",
  description:
    "Explore ideas for agent-powered products, with starter prompts, useful context, and concrete results.",
  alternates: { canonical: "/use-cases/" },
};

export default function UseCasesPage() {
  return (
    <div className={base.landing}>
      <PageHero
        eyebrow="Examples"
        id="use-cases-title"
        title={
          <>
            A request.
            <br />A real result.
          </>
        }
        description="Build tools, create content, work with data, and carry out tasks. Explore what an agent could do inside your product, with prompts you can make your own."
      >
        <Link href="#use-case-library" className={base.primaryButton}>
          Explore the examples <ArrowDown size={16} />
        </Link>
        <Link href="/sdk" className={base.secondaryButton}>
          Build it into your product <ArrowUpRight size={16} />
        </Link>
      </PageHero>
      <UseCaseExplorer />
      <section
        className={styles.makeItYours}
        aria-labelledby="make-it-yours-title"
      >
        <div className={styles.makeItYoursIntro}>
          <img
            src="/images/framework/canvas-motif.svg"
            alt=""
            width={105}
            height={29}
          />
          <h2 id="make-it-yours-title">
            The best use case?
            <br />
            <em>Probably yours.</em>
          </h2>
          <p>
            These are starting points. Useful results start with the right
            files, tools, and a clear request.
          </p>
          <Link href="/docs/guides/quickstart" className={base.primaryButton}>
            Start building
            <ArrowUpRight size={16} />
          </Link>
        </div>
        <ol className={styles.startSteps}>
          <li>
            <span>01</span>
            <div>
              <h3>Give it a job.</h3>
              <p>
                Choose a purpose and describe what a good result looks like. A
                short, specific request goes a long way.
              </p>
            </div>
          </li>
          <li>
            <span>02</span>
            <div>
              <h3>Bring your context.</h3>
              <p>
                Share the files, notes and preferences that matter. Add the
                tools you want it to use in its workspace.
              </p>
            </div>
          </li>
          <li>
            <span>03</span>
            <div>
              <h3>Keep shaping it.</h3>
              <p>
                Review what it makes. Ask for a change, try another approach, or
                turn a useful result into a tool you can return to.
              </p>
            </div>
          </li>
        </ol>
      </section>
    </div>
  );
}
