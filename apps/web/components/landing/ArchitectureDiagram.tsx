import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import styles from "./architecture-diagram.module.css";

const asset = (name: string) => `/images/framework/${name}.svg`;

const inputs = [
  {
    key: "files",
    anchor: "hiw-files",
    label: "IN // KNOWLEDGE",
    title: "Your files, knowledge & goals",
    art: "files",
    icon: "files-icon",
  },
  {
    key: "website",
    anchor: "hiw-web",
    label: "IN // YOUR PRODUCT",
    title: "Your interface. Your brand.",
    art: "website",
    icon: "website-icon",
  },
  {
    key: "workspace",
    anchor: "hiw-figma",
    label: "IN // TOOLS & CONTEXT",
    title: "Everything your agent needs",
    art: "workspace",
    icon: "workspace-icon",
  },
] as const;

function DiagramInputs() {
  return (
    <div className={styles.sources} data-sprue="hiw-sources">
      {inputs.map((input) => (
        <div
          key={input.key}
          data-sprue={input.anchor}
          className={`${styles.card} ${styles[input.key]}`}
        >
          <img
            className={styles.art}
            src={asset(input.art)}
            alt=""
            width={366}
            height={216}
            loading="lazy"
          />
          <div className={styles.cardHeading}>
            <span className={styles.label}>{input.label}</span>
            <h3>{input.title}</h3>
            {input.key === "files" && (
              <span className={styles.reading}>[READING FILES]</span>
            )}
          </div>
          <img
            className={styles.icon}
            src={asset(input.icon)}
            alt=""
            width={40}
            height={40}
            loading="lazy"
          />
        </div>
      ))}
    </div>
  );
}

function DiagramHub() {
  return (
    <div className={styles.hub}>
      <img
        className={styles.hubShape}
        src={asset("hub")}
        alt=""
        width={239}
        height={239}
      />
      <span className={styles.hubCaption}>
        Their intelligence. Your product.
      </span>
      <span
        className={styles.hubTop}
        data-sprue="hiw-hub-top"
        aria-hidden="true"
      />
      <span className={styles.hubBody} data-sprue="hiw-rb" aria-hidden="true" />
      <span
        className={styles.hubFoot}
        data-sprue="hiw-hub-foot"
        aria-hidden="true"
      />
      <img
        className={styles.hubTicks}
        src={asset("hub-ticks")}
        alt=""
        width={83}
        height={7}
      />
      <span className={styles.hubMark} aria-hidden="true">
        m.
      </span>
      <span className={styles.hubLabel}>MINDWIRE</span>
    </div>
  );
}

function DiagramOutputs() {
  return (
    <>
      <Link
        href="/docs/reference/agents/codex"
        data-sprue="hiw-agents"
        className={`${styles.card} ${styles.agents} ${styles.outputLink}`}
      >
        <img
          className={styles.art}
          src={asset("agents")}
          alt="Claude, Cursor, Codex, ChatGPT, Gemini, and more"
          width={366}
          height={153}
          loading="lazy"
        />
        <div className={styles.cardHeading}>
          <span className={styles.label}>OUT // YOUR AGENT</span>
          <h3>
            The best brains, made yours <ArrowUpRight size={13} />
          </h3>
        </div>
        <img
          className={styles.icon}
          src={asset("agents-icon")}
          alt=""
          width={40}
          height={40}
          loading="lazy"
        />
      </Link>
      <div className={styles.product} data-sprue="hiw-product">
        <Link
          href="/sdk"
          data-sprue="hiw-api"
          className={`${styles.card} ${styles.api} ${styles.outputLink}`}
        >
          <img
            className={styles.art}
            src={asset("api")}
            alt=""
            width={366}
            height={216}
            loading="lazy"
          />
          <div className={styles.cardHeading}>
            <span className={styles.label}>OUT // SDK & API</span>
            <h3>
              Embed it into your product <ArrowUpRight size={13} />
            </h3>
          </div>
          <img
            className={styles.icon}
            src={asset("api-icon")}
            alt=""
            width={41}
            height={37}
            loading="lazy"
          />
          <div className={styles.terminal} aria-hidden="true">
            <span className={styles.terminalStatus}>mindwire.ts</span>
            <pre>
              <code>{`const run = await mw.turn({\n  chatId: conversation.id,\n  message: user.message,\n});`}</code>
            </pre>
          </div>
        </Link>
        <p className={styles.workflow}>
          Keep your product, tools, and brand.
          <br />
          Mindwire brings the agent layer.
        </p>
      </div>
    </>
  );
}

export default function ArchitectureDiagram() {
  return (
    <section
      id="how-it-works"
      className={styles.frame}
      aria-labelledby="architecture-title"
    >
      <WindowShape kind="diagram" color="var(--mw-inverse)" />
      <SurfaceRoutes kind="diagram" className={styles.composition}>
        <div className={styles.heading}>
          <span className={styles.eyebrow}>HOW IT WORKS</span>
          <h2 id="architecture-title">
            Your product,
            <br />
            powered by agents.
          </h2>
          <p>
            Mindwire connects the best agent harnesses to your tools, knowledge,
            and interface. Your users ask. Your product delivers.
          </p>
        </div>
        <DiagramInputs />
        <DiagramHub />
        <DiagramOutputs />
      </SurfaceRoutes>
    </section>
  );
}
