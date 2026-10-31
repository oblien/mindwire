import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import ProductStudio from "@/components/landing/ProductStudio";
import styles from "./ownership-story.module.css";

const foundations = [
  ["Streaming & tool activity", "/docs/guides/streaming"],
  ["Skills & MCP", "/docs/guides/mcp"],
  ["Memory & instructions", "/docs/guides/prompts-and-memory"],
  ["Approvals & live steering", "/docs/guides/interaction"],
  ["Voice, files & images", "/docs/guides/attachments"],
  ["Slack, Teams, Discord & Telegram", "/docs/guides/integrations"],
];

export default function OwnershipStory() {
  return (
    <div className={styles.story}>
      <ProductStudio embedded />
      <div className={styles.foundations}>
        <p>
          The full agent. <br />
          <strong>Behind every bot.</strong>
        </p>
        <nav aria-label="Explore the agent capabilities">
          {foundations.map(([label, href]) => (
            <Link key={href} href={href}>
              {label}
              <ArrowUpRight size={14} />
            </Link>
          ))}
        </nav>
      </div>
    </div>
  );
}
