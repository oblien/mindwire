import Link from "next/link";
import { ArrowUpRight } from "@/components/site/InterfaceIcon";
import Questions from "@/components/landing/Questions";
import SiteIllustration from "./SiteIllustration";
import styles from "./home-sections.module.css";

export default function HomeSections() {
  return (
    <>
      <nav className={styles.products} aria-label="More from MindWire">
        <Link
          href="/app"
          className={styles.product}
          aria-label="Explore the MindWire app"
        >
          <div className={styles.productCopy}>
            <h3>
              Take MindWire
              <br />
              for a spin.
            </h3>
            <p>
              Your agents, files, and projects. Together in a workspace that
              goes with you.
            </p>
            <span className={styles.productAction}>
              Explore the app <ArrowUpRight size={18} />
            </span>
          </div>
          <div className={styles.productArt}>
            <SiteIllustration kind="devices" />
          </div>
        </Link>
        <Link
          href="/cloud"
          className={`${styles.product} ${styles.cloud}`}
          aria-label="Explore MindWire Cloud"
        >
          <div className={styles.productCopy}>
            <h3>
              Let your projects
              <br />
              keep going.
            </h3>
            <p>
              An optional cloud computer for the work that carries on when you
              step away.
            </p>
            <span className={styles.productAction}>
              Explore MindWire Cloud <ArrowUpRight size={18} />
            </span>
          </div>
          <div className={styles.productArt}>
            <SiteIllustration kind="cloud" />
          </div>
        </Link>
      </nav>

      <section className={styles.faq} aria-labelledby="home-questions-title">
        <div>
          <h2 id="home-questions-title">Questions, answered.</h2>
          <p>Start with the idea. Find the details in the docs.</p>
          <Link href="/docs">
            Read the documentation <ArrowUpRight size={17} />
          </Link>
        </div>
        <Questions
          items={[
            {
              question: "What does MindWire add to my product?",
              answer:
                "MindWire connects established agent harnesses to your product through one SDK and runtime. You supply the interface, context, and tools. The agent brings its planning and execution.",
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
              question: "Do I need MindWire Cloud?",
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
