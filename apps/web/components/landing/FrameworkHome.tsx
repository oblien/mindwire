import Link from "next/link";
import { ArrowRight, ArrowUpRight } from "lucide-react";
import ArchitectureDiagram from "./ArchitectureDiagram";
import FrameworkNav from "./FrameworkNav";
import FrameworkFooter from "./FrameworkFooter";
import RuntimeConnect from "./RuntimeConnect";
import HeroTrail from "./HeroTrail";
import HeatformArt from "./HeatformArt";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import AgentConnect from "./AgentConnect";
import styles from "./framework-home.module.css";

const asset = (name: string) => "/images/framework/" + name;

function GraphicHeading({ first, second }: { first: string; second: string }) {
  const stacked = first === "YOUR PRODUCT.";
  return (
    <>
      <svg
        viewBox="0 0 800 112"
        preserveAspectRatio="none"
        aria-hidden="true"
        className={`${styles.graphicHeading} ${stacked ? styles.wideHeading : ""}`}
      >
        <text
          x="400"
          y="49"
          textAnchor="middle"
          textLength="800"
          lengthAdjust="spacingAndGlyphs"
        >
          {first}
        </text>
        <text
          x="400"
          y="105"
          textAnchor="middle"
          textLength="800"
          lengthAdjust="spacingAndGlyphs"
        >
          {second}
        </text>
      </svg>
      {stacked && (
        <svg
          viewBox="0 0 640 192"
          preserveAspectRatio="none"
          aria-hidden="true"
          className={styles.stackedHeading}
        >
          {["YOUR PRODUCT.", "POWERED BY", "AGENTS."].map((line, index) => (
            <text
              key={line}
              x={320}
              y={60 + index * 64}
              textAnchor="middle"
              textLength={640}
              lengthAdjust="spacingAndGlyphs"
            >
              {line}
            </text>
          ))}
        </svg>
      )}
    </>
  );
}

function Hero() {
  return (
    <SurfaceRoutes
      as="section"
      kind="hero"
      className={styles.hero}
      aria-labelledby="hero-title"
    >
      <div className={styles.heroWindow} data-sprue="hero">
        <WindowShape kind="hero" />
        <HeroTrail />
        <div className={styles.heroIntro}>
          <h1 id="hero-title">
            <span className={styles.srOnly}>
              Your product. Powered by agents.
            </span>
            <GraphicHeading first="YOUR PRODUCT." second="POWERED BY AGENTS." />
          </h1>
          <p>
            Mindwire brings the world’s best <span>agent harnesses</span>
            <br className={styles.desktopBreak} /> into{" "}
            <span>your product</span>. Without rebuilding the agent layer.
          </p>
          <div className={styles.heroActions}>
            <Link
              href="/docs/guides/quickstart"
              className={styles.primaryButton}
            >
              Start building <ArrowRight size={16} />
            </Link>
            <AgentConnect />
          </div>
        </div>
        <div className={styles.canvasFoot}>
          <img src={asset("canvas-motif.svg")} alt="" width={105} height={29} />
          <span>CANVAS: 01</span>
        </div>
        <img
          className={styles.heroCross}
          src={asset("cross.svg")}
          alt=""
          width={22}
          height={22}
        />
      </div>
    </SurfaceRoutes>
  );
}

function Manifesto() {
  return (
    <SurfaceRoutes
      as="section"
      kind="manifesto"
      className={styles.manifesto}
      aria-labelledby="manifesto-title"
    >
      <h2
        id="manifesto-title"
        data-sprue="manifesto-tab"
        className={styles.pill}
      >
        THE IDEA
      </h2>
      <p>
        <span>
          The best agents already know how to plan, create, and get things done.
        </span>{" "}
        Mindwire makes them part of your product: your tools, your knowledge,
        your brand. All the intelligence. None of the reinvention.
      </p>
    </SurfaceRoutes>
  );
}

function Features() {
  return (
    <SurfaceRoutes
      as="section"
      kind="features"
      id="features"
      className={styles.features}
      aria-labelledby="features-title"
    >
      <div className={styles.featureIntro} data-sprue="feat-header">
        <span className={styles.eyebrow}>MAKE IT YOURS</span>
        <h2 id="features-title">
          <span className={styles.srOnly}>Their brains. Your rules.</span>
          <GraphicHeading first="THEIR BRAINS." second="YOUR RULES." />
        </h2>
        <p>
          White-label leading agents. Give them the tools, context, and
          direction that make your product yours.
        </p>
        <Link href="/docs/guides/quickstart">
          Explore the framework <ArrowUpRight size={14} />
        </Link>
        <img
          src={asset("sphere.svg")}
          width={200}
          height={200}
          alt=""
          loading="lazy"
        />
      </div>
      <div className={styles.featureSteps}>
        <div className={styles.featureStep}>
          <div className={styles.featureArt} data-sprue="feat-brief-part">
            <HeatformArt variant="cobalt" />
          </div>
          <div>
            <h3>
              Your <mark className={styles.blueHighlight}>brand</mark>, your
              interface.
              <br />
              An agent that belongs in your product.
            </h3>
            <Link href="/use-cases">
              See the experience <ArrowUpRight size={14} />
            </Link>
          </div>
        </div>
        <div className={styles.featureStep}>
          <div className={styles.featureArt} data-sprue="feat-generate-part">
            <HeatformArt variant="lime" />
          </div>
          <div>
            <h3>
              Your <mark className={styles.limeHighlight}>tools</mark> and
              knowledge.
              <br />
              Real capabilities for your users.
            </h3>
            <Link href="/docs/guides/mcp">
              Connect your tools <ArrowUpRight size={14} />
            </Link>
          </div>
        </div>
        <div className={styles.featureStep}>
          <div className={styles.featureArt} data-sprue="feat-review-part">
            <HeatformArt variant="thermal" />
          </div>
          <div>
            <h3>
              Your <mark className={styles.redHighlight}>mindset</mark> and
              goals.
              <br />
              Give the intelligence a direction.
            </h3>
            <Link href="/docs/guides/quickstart">
              Shape the work <ArrowUpRight size={14} />
            </Link>
          </div>
        </div>
      </div>
    </SurfaceRoutes>
  );
}

function Connect() {
  return (
    <SurfaceRoutes
      as="section"
      kind="connect"
      id="connect"
      className={styles.connect}
      aria-labelledby="connect-title"
    >
      <div className={styles.connectIntro} data-sprue="connect-intro">
        <WindowShape kind="connect" />
        <h2 id="connect-title">Connect Mindwire.</h2>
        <p>
          The agent layer is ready.
          <br />
          Build the product only you can.
        </p>
        <div>
          <Link className={styles.primaryButton} href="/docs/guides/quickstart">
            Start building <ArrowUpRight size={14} />
          </Link>
          <Link href="/cloud">
            Run it in Cloud <ArrowUpRight size={14} />
          </Link>
        </div>
        <img
          src={asset("connect-motif.svg")}
          alt=""
          width={140}
          height={160}
          loading="lazy"
        />
      </div>
      <div className={styles.connectArt} data-sprue="connect-texture">
        <HeatformArt variant="bloom" />
      </div>
      <RuntimeConnect />
    </SurfaceRoutes>
  );
}

function Closing() {
  return (
    <SurfaceRoutes
      as="section"
      kind="closing"
      artwork={{ surface: "closing-cta", dock: "cta-dock", overlayOnly: true }}
      className={styles.closing}
      aria-labelledby="closing-title"
    >
      <div className={styles.closingArtwork} data-sprue="closing-cta">
        <WindowShape kind="closing" />
        <HeatformArt variant="closing" />
        <span
          className={styles.ctaDock}
          data-sprue="cta-dock"
          aria-hidden="true"
        />
      </div>
      <div className={styles.closingContent}>
        <h2 id="closing-title">
          <span className={styles.srOnly}>
            Your product. Powered by agents.
          </span>
          <GraphicHeading first="YOUR PRODUCT." second="POWERED BY AGENTS." />
        </h2>
        <p>
          The world’s best <span>agent harnesses</span>.<br />
          Your tools, your knowledge, <span>your product</span>.
        </p>
        <div className={styles.heroActions}>
          <Link href="/docs/guides/quickstart" className={styles.primaryButton}>
            Start building <ArrowRight size={15} />
          </Link>
          <a
            href="https://github.com/oblien/mindwire"
            className={styles.sourceButton}
          >
            Explore the source <ArrowUpRight size={15} />
          </a>
        </div>
      </div>
      <img
        className={styles.closingCross}
        src={asset("cross.svg")}
        alt=""
        width={22}
        height={22}
      />
    </SurfaceRoutes>
  );
}

export default function FrameworkHome() {
  return (
    <div className={styles.page}>
      <FrameworkNav />
      <Hero />
      <Manifesto />
      <div className={styles.systemWindow}>
        <ArchitectureDiagram />
        <Features />
      </div>
      <Connect />
      <Closing />
      <FrameworkFooter />
    </div>
  );
}
