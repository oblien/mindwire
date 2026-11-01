import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "@/components/site/InterfaceIcon";
import AgentLogo from "@/components/marketing/AgentLogo";
import { heroAgents, heroHeadline, heroDescription } from "@/lib/hero";
import SiteNav from "@/components/site/SiteNav";
import SiteFooter from "@/components/site/SiteFooter";
import HomeSections from "@/components/site/HomeSections";
import ConversationHandoff from "@/components/site/ConversationHandoff";
import ProductPossibilities from "@/components/site/ProductPossibilities";
import BuildFlow from "./BuildFlow";
import BackgroundWork from "./BackgroundWork";
import IdeaDiagram from "./IdeaDiagram";
import HeroArtwork from "./HeroArtwork";
import type { HeroVariant } from "./hero-surfaces";
import OwnershipStory from "./OwnershipStory";
import { OrbitArtwork } from "./Artwork";
import MindWireMark from "@/components/brand/MindWireMark";
import styles from "./refined.module.css";

function StartLink({
  children = "Start building",
}: {
  children?: React.ReactNode;
}) {
  return (
    <Link href="/docs/guides/quickstart" className={styles.primary}>
      <span>{children}</span>
      <span className={styles.primaryIcon}>
        <ArrowUpRight size={19} />
      </span>
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
            <ul
              className={styles.heroAgents}
              aria-label="Agents you can build with"
            >
              {heroAgents.map((agent) => (
                <li key={agent.id}>
                  <Link
                    href={agent.href}
                    aria-label={agent.name}
                    title={agent.name}
                  >
                    <AgentLogo agent={agent.id} size={25} surface="light" />
                  </Link>
                </li>
              ))}
            </ul>
            <h1 id="hero-title">
              {heroHeadline[0]}
              <br />
              <span>{heroHeadline[1]}</span>
            </h1>
            <p>
              {heroDescription[0]}
              <br />
              {heroDescription[1]}
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
          MindWire connects capable agents to everything that makes your product
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

        <div className={styles.showcase} id="inside">
          <div className={styles.systemJoint} aria-hidden="true">
            <MindWireMark />
          </div>
          <OwnershipStory />
        </div>
      </div>

      <BackgroundWork />
      <ConversationHandoff />
      <ProductPossibilities />
      <BuildFlow />
      <HomeSections />

      <SiteFooter embedded />
    </div>
  );
}
