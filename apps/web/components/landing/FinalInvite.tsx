import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import base from "./landing.module.css";
import styles from "./product.module.css";

export default function FinalInvite({
  audience = "developers",
  illustrated = false,
}: {
  audience?: "developers" | "app";
  illustrated?: boolean;
}) {
  const app = audience === "app";
  return (
    <section className={styles.finalInvite} aria-labelledby="invite-title">
      {illustrated && (
        <img
          className={styles.inviteArt}
          src="/images/framework/canvas-motif.svg"
          alt=""
          width={105}
          height={29}
        />
      )}
      <h2 id="invite-title">
        {app
          ? "Start with something you need done."
          : "What could your product do next?"}
      </h2>
      <p>
        {app
          ? "Bring your files, connect an agent, and give it a task."
          : "Build your first agent feature with the Mindwire SDK."}
      </p>
      <Link
        href={app ? "/app#download" : "/docs/guides/quickstart"}
        className={base.primaryButton}
      >
        {app ? "Get the Mindwire app" : "Start building"}{" "}
        <ArrowUpRight size={18} />
      </Link>
      <span>
        {app
          ? "General and Developer modes. One connected workspace."
          : "Open-source runtime · TypeScript · Go · REST + SSE"}
      </span>
    </section>
  );
}
