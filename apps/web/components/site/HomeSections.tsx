import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { useCases } from "@/lib/use-cases";
import UseCaseCard from "@/components/landing/UseCaseCard";
import Questions from "@/components/landing/Questions";
import SiteIllustration from "./SiteIllustration";
import styles from "./home-sections.module.css";

const featured = ["desktop-research", "cloud-site-builder", "learning-studio"]
  .map((slug) => useCases.find((item) => item.slug === slug))
  .filter((item): item is (typeof useCases)[number] => Boolean(item));

export default function HomeSections() {
  return (
    <>
      <section
        className={styles.examples}
        id="possibilities"
        aria-labelledby="possibilities-title"
      >
        <div className={styles.heading}>
          <h2 id="possibilities-title">
            What will <br />
            yours do?
          </h2>
          <div>
            <p>
              A research workspace. A website builder. A whole learning
              experience. Start with what your users want to make possible.
            </p>
            <Link href="/use-cases">
              Explore the product examples <ArrowUpRight size={18} />
            </Link>
          </div>
        </div>
        <div className={styles.examplesGrid}>
          {featured.map((item) => (
            <UseCaseCard key={item.slug} item={item} />
          ))}
        </div>
        <p className={styles.sampleNote}>
          Illustrative products. Real integration patterns you can build on.
        </p>
      </section>

      <section className={styles.products} aria-labelledby="products-title">
        <div className={styles.heading}>
          <h2 id="products-title">
            One framework. <br />
            More ways to work.
          </h2>
          <p>
            The same agent layer, ready for your own projects. A workspace to
            work in. A computer that stays with you.
          </p>
        </div>
        <div className={styles.productGrid}>
          <Link href="/app" className={styles.product}>
            <div className={styles.productArt}>
              <SiteIllustration kind="devices" />
            </div>
            <span>Mindwire App</span>
            <h3>
              Your work.
              <br />
              With you.
            </h3>
            <p>
              Agents, files, and projects in one workspace. Pick up from your
              Mac, iPhone, or iPad.
            </p>
            <div className={styles.productLink}>
              Meet the app <ArrowUpRight size={19} />
            </div>
          </Link>
          <Link href="/cloud" className={styles.product}>
            <div className={styles.productArt}>
              <SiteIllustration kind="cloud" />
            </div>
            <span>Mindwire Cloud</span>
            <h3>
              A place to
              <br />
              keep going.
            </h3>
            <p>
              A personal computer for your projects, online even when your own
              computer is off.
            </p>
            <div className={styles.productLink}>
              Explore Cloud & pricing <ArrowUpRight size={19} />
            </div>
          </Link>
        </div>
      </section>

      <section className={styles.faq} aria-labelledby="home-questions-title">
        <div>
          <h2 id="home-questions-title">
            A few good
            <br />
            questions.
          </h2>
          <p>Start with the idea. Find the details in the docs.</p>
          <Link href="/docs">
            Read the documentation <ArrowUpRight size={17} />
          </Link>
        </div>
        <Questions
          items={[
            {
              question: "What does Mindwire add to my product?",
              answer:
                "Mindwire connects established agent harnesses to your product through one SDK and runtime. You supply the interface, context, and tools. The agent brings its planning and execution.",
            },
            {
              question: "Can I keep my own interface and brand?",
              answer: (
                <>
                  Yes. Stream the agent’s work into your existing interface,
                  connect your own tools, and use your own voice and identity.{" "}
                  <Link href="/sdk">See how the SDK fits.</Link>
                </>
              ),
            },
            {
              question: "Where does the agent run?",
              answer: (
                <>
                  Run locally, on your server, or in an isolated remote
                  environment. Your deployment decides where files and tools
                  live.{" "}
                  <Link href="/docs/guides/destinations">
                    Explore runtime destinations.
                  </Link>
                </>
              ),
            },
            {
              question: "Do I need Mindwire Cloud?",
              answer: (
                <>
                  Cloud is optional. Use your own infrastructure for your
                  product, or a computer you already own for personal work. The
                  SDK is open source.{" "}
                  <Link href="/cloud">Compare the options.</Link>
                </>
              ),
            },
            {
              question: "Which agents can I use?",
              answer: (
                <>
                  Connect Codex, Claude Code, Grok Build, or opencode.
                  Authentication and capabilities depend on the selected agent;
                  model usage stays with its provider.{" "}
                  <Link href="/docs/guides/agents">Meet the agents.</Link>
                </>
              ),
            },
          ]}
        />
      </section>
    </>
  );
}
