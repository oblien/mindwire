import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { githubUrl } from "@/lib/product";
import { consoleUrl } from "@/lib/console-url";
import { MindwireMark } from "@/components/design-lab/refined/Artwork";
import styles from "./site.module.css";

const groups = [
  {
    title: "Build",
    links: [
      ["SDK & runtime", "/sdk"],
      ["Documentation", "/docs"],
      ["Product examples", "/use-cases"],
      ["Sandboxing", "/sandboxing"],
    ],
  },
  {
    title: "Work",
    links: [
      ["Mindwire App", "/app"],
      ["Cloud & pricing", "/cloud"],
      ["App setup", "/get-started"],
      ["Developer mode", "/developers"],
    ],
  },
  {
    title: "Connect",
    links: [
      ["GitHub", githubUrl],
      ["Changelog", "/changelog"],
      ["Support", githubUrl + "/issues"],
      ["Console", consoleUrl],
    ],
  },
];

export default function SiteFooter() {
  return (
    <footer className={styles.footer}>
      <div className={styles.footerTop}>
        <div className={styles.identity}>
          <Link href="/" className={styles.wordmark} aria-label="Mindwire home">
            <MindwireMark />
            <span>mindwire</span>
          </Link>
          <p>
            The agent layer for your product.
            <br />
            Open source. Made to be made yours.
          </p>
          <svg viewBox="0 0 190 58" fill="none" aria-hidden="true">
            <path
              d="M1 29H42Q53 29 53 18V13Q53 2 64 2H98Q109 2 109 13V45Q109 56 120 56H157Q168 56 168 45V40Q168 29 179 29H189"
              stroke="currentColor"
              strokeWidth="1"
            />
            <path
              d="M26 29H158"
              stroke="currentColor"
              strokeWidth="1"
              strokeDasharray="2 6"
              opacity=".4"
            />
            <circle
              cx="81"
              cy="29"
              r="8"
              fill="#d9c9e3"
              stroke="currentColor"
            />
            <circle
              cx="142"
              cy="29"
              r="5"
              fill="#dce5cc"
              stroke="currentColor"
            />
          </svg>
        </div>
        <nav className={styles.footerNav} aria-label="Footer navigation">
          {groups.map((group) => (
            <div key={group.title}>
              <h2>{group.title}</h2>
              {group.links.map(([label, href]) => (
                <Link key={href} href={href}>
                  {label}
                  {href.startsWith("https:") && <ArrowUpRight size={12} />}
                </Link>
              ))}
            </div>
          ))}
        </nav>
      </div>
      <div className={styles.footerBottom}>
        <span>© {new Date().getFullYear()} Mindwire</span>
        <a href={githubUrl + "/blob/main/LICENSING.md"}>
          Open-source licenses <ArrowUpRight size={12} />
        </a>
        <a href="https://oblien.com/">
          Made by Oblien <ArrowUpRight size={12} />
        </a>
      </div>
    </footer>
  );
}
