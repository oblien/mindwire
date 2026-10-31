import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import AnimatedLinework from "./AnimatedLinework";
import styles from "./ownership-story.module.css";

const details = [
  {
    id: "identity",
    title: "Your identity.",
    description:
      "Your interface, your voice, your name. An agent that belongs in the experience your users already know.",
    alt: "Your typography, color palette, and branded interface, drawn as a flat design kit.",
    href: "/docs/guides/react",
    link: "Shape the experience",
  },
  {
    id: "context",
    title: "Your context.",
    description:
      "The files, knowledge, and instructions that make your product useful. Give the agent a world to work from.",
    alt: "Source documents connected to cited references and product knowledge.",
    href: "/docs/guides/prompts-and-memory",
    link: "Bring your knowledge",
  },
  {
    id: "actions",
    title: "Your actions.",
    description:
      "Connect what your product can do. Set the boundaries, and decide when the agent should ask for approval.",
    alt: "A tool call connected to an approval button and a completed result.",
    href: "/docs/guides/mcp",
    link: "Connect your tools",
  },
] as const;

export default function OwnershipStory() {
  return (
    <div className={styles.story}>
      <div className={styles.intro}>
        <h2 id="showcase-title">
          Unmistakably
          <br />
          yours.
        </h2>
        <p>
          The agent brings the capability.
          <br /> You give it an identity, a context,
          <br className={styles.wideBreak} /> and a way of working.
        </p>
        <Link href="/docs/guides/quickstart">
          Make it part of your product <ArrowUpRight size={18} />
        </Link>
      </div>
      <div className={styles.features}>
        {details.map((detail) => (
          <article className={styles.feature} key={detail.id}>
            <div className={styles.illustration} data-material={detail.id}>
              <AnimatedLinework variant={detail.id} label={detail.alt} />
            </div>
            <div className={styles.copy}>
              <h3>{detail.title}</h3>
              <p>{detail.description}</p>
              <Link href={detail.href}>
                {detail.link}
                <ArrowUpRight size={16} />
              </Link>
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}
