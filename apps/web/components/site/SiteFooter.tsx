import Link from "next/link";
import { ArrowUpRight } from "@/components/site/InterfaceIcon";
import { githubUrl } from "@/lib/product";
import { consoleUrl } from "@/lib/console-url";
import MindWireMark from "@/components/brand/MindWireMark";
import OblienLogo from "@/components/OblienLogo";
import styles from "./site-footer.module.css";

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
      ["MindWire App", "/app"],
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

export default function SiteFooter({
  embedded = false,
}: {
  embedded?: boolean;
}) {
  return (
    <footer className={styles.footer} data-embedded={embedded}>
      <div className={styles.footerTop}>
        <div className={styles.identity}>
          <p className={styles.closing}>
            Made to be
            <br />
            made yours.
          </p>
          <p>
            The open-source framework for
            <br />
            products powered by agents.
          </p>
          <Link href={githubUrl}>
            Explore the source <ArrowUpRight size={16} />
          </Link>
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
      <Link href="/" className={styles.signature} aria-label="MindWire home">
        <MindWireMark />
        <span>MindWire</span>
      </Link>
      <div className={styles.footerBottom}>
        <span>© {new Date().getFullYear()} MindWire</span>
        <a href={githubUrl + "/blob/main/LICENSING.md"}>
          Open-source licenses <ArrowUpRight size={12} />
        </a>
        <a href="https://oblien.com/">
          <span>Made by</span>
          <OblienLogo size={18} />
          <span>Oblien</span>
          <ArrowUpRight size={12} />
        </a>
      </div>
    </footer>
  );
}
