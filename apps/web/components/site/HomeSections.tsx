import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import Questions from "@/components/landing/Questions";
import SiteIllustration from "./SiteIllustration";
import styles from "./home-sections.module.css";

export default function HomeSections() {
  return (
    <>
      <nav className={styles.products} aria-label="More from Mindwire">
        <Link href="/app" className={styles.product}>
          <div className={styles.productArt}>
            <SiteIllustration kind="devices" />
          </div>
          <div>
            <h3>Try the Mindwire app.</h3>
            <p>
              See what the framework can do. Agents, files and projects, across
              your devices.
            </p>
          </div>
          <ArrowUpRight size={21} />
        </Link>
        <Link href="/cloud" className={styles.product}>
          <div className={styles.productArt}>
            <SiteIllustration kind="cloud" />
          </div>
          <div>
            <h3>Give your work a home.</h3>
            <p>
              An optional cloud computer for projects that keep going when your
              laptop closes.
            </p>
          </div>
          <ArrowUpRight size={21} />
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
