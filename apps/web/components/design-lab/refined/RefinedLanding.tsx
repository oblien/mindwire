import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import { productAgents } from "@/lib/product";
import SiteNav from "@/components/site/SiteNav";
import SiteFooter from "@/components/site/SiteFooter";
import HomeSections from "@/components/site/HomeSections";
import BuildFlow from "./BuildFlow";
import IdeaDiagram from "./IdeaDiagram";
import HeroArtwork from "./HeroArtwork";
import type { HeroVariant } from "./hero-surfaces";
import OwnershipStory from "./OwnershipStory";
import AnimatedLinework from "./AnimatedLinework";
import { MindwireMark, OrbitArtwork } from "./Artwork";
import styles from "./refined.module.css";

function StartLink({
  children = "Start building",
}: {
  children?: React.ReactNode;
}) {
  return (
    <Link href="/docs/guides/quickstart" className={styles.primary}>
      <span>{children}</span>
      <ArrowUpRight size={19} />
    </Link>
  );
}

export default function RefinedLanding({
  heroVariant = "glass",
}: {
  heroVariant?: HeroVariant;
}) {
  return (
    <div className={styles.page} id="top">
      <div className={styles.heroShell} data-hero-kind={heroVariant}>
        <HeroArtwork variant={heroVariant} />
        <SiteNav embedded />

        <section className={styles.hero} aria-labelledby="hero-title">
          <div className={styles.heroCopy}>
            <h1 id="hero-title">
              Your product.
              <br />
              <span>Powered by agents.</span>
            </h1>
            <p>
              The best agents already exist.
              <br />
              Give them your tools, your knowledge, and a place in your product.
            </p>
            <div className={styles.heroActions}>
              <StartLink />
              <a href="#concept" className={styles.textLink}>
                See how it works <ArrowDown size={17} />
              </a>
            </div>
          </div>
          <div className={styles.heroBottom}>
            <a href="#concept" aria-label="Explore the idea">
              <ArrowDown size={23} />
            </a>
          </div>
        </section>
      </div>

      <div className={styles.agentRail}>
        <p>
          Great agents.
          <br />
          <span>One framework.</span>
        </p>
        <div>
          {productAgents.map((agent) => (
            <Link href={agent.href} key={agent.id}>
              <AgentLogo agent={agent.id} size={28} surface="light" />
              <span>{agent.name}</span>
            </Link>
          ))}
        </div>
        <Link href="/docs/concepts/architecture" className={styles.railLink}>
          Meet Mindwire <ArrowUpRight size={18} />
        </Link>
      </div>

      <section className={styles.manifesto} aria-labelledby="manifesto-title">
        <div className={styles.manifestoSymbol} aria-hidden="true">
          <OrbitArtwork />
        </div>
        <h2 id="manifesto-title">
          <span>
            They can think, plan,
            <br className={styles.desktopBreak} /> and get things done.
          </span>
          <br />
          You give them a world
          <br className={styles.desktopBreak} /> worth working in.
        </h2>
        <p>
          Mindwire connects capable agents to everything that makes your product
          yours. The knowledge. The actions. The experience.
        </p>
        <svg
          className={styles.manifestoWire}
          viewBox="0 0 600 130"
          preserveAspectRatio="none"
          aria-hidden="true"
        >
          <path d="M0 1H220Q280 1 280 61V69Q280 129 340 129H600" />
        </svg>
      </section>

      <div className={styles.system}>
        <section
          className={styles.idea}
          id="concept"
          aria-labelledby="idea-title"
        >
          <div className={styles.ideaHeading}>
            <h2 id="idea-title">
              Everything connects.
              <br />
              <span>Everything stays yours.</span>
            </h2>
            <p>
              Connect what your product knows and what it can do. Let the agent
              work inside the experience you’ve already built.
            </p>
          </div>
          <IdeaDiagram />
        </section>

        <section
          className={styles.showcase}
          id="inside"
          aria-labelledby="showcase-title"
        >
          <div className={styles.systemJoint} aria-hidden="true">
            <MindwireMark />
          </div>
          <OwnershipStory />
        </section>
      </div>

      <BuildFlow />
      <HomeSections />

      <section className={styles.closing} aria-labelledby="closing-title">
        <div className={styles.closingArt} aria-hidden="true">
          <AnimatedLinework variant="weave" />
        </div>
        <div className={styles.closingCopy}>
          <h2 id="closing-title">
            Now, make
            <br />
            it yours.
          </h2>
          <p>
            Great intelligence deserves
            <br />a great place to work.
          </p>
          <StartLink />
        </div>
      </section>

      <SiteFooter />
    </div>
  );
}
