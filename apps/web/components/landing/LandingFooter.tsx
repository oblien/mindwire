import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import OblienLogo from "../OblienLogo";
import { consoleUrl } from "@/lib/console-url";
import { githubUrl } from "@/lib/product";
import base from "./landing.module.css";
import styles from "./product.module.css";

const columns = [
  { title: "Build with Mindwire", links: [["SDK & runtime", "/sdk"], ["Quickstart", "/docs/guides/quickstart"], ["Documentation", "/docs"], ["Sandboxing", "/sandboxing"], ["Source code", githubUrl]] },
  { title: "Run and use", links: [["Mindwire Cloud", "/cloud"], ["Pricing", "/pricing"], ["Cloud Console", consoleUrl], ["Mindwire App", "/app"], ["Download the app", "/download"]] },
  { title: "Explore", links: [["App examples", "/use-cases"], ["Developer mode", "/developers"], ["App setup", "/get-started"], ["Changelog", "/changelog"], ["Get help", githubUrl + "/issues"]] },
];

export default function LandingFooter() {
  return <div className={base.landing}><footer className={styles.siteFooter}>
    <div className={styles.footerTop}>
      <div className={styles.footerIdentity}><Link href="/" className={base.wordmark}><OblienLogo size={28} /> Mindwire</Link><p>Your product.<br />Powered by agents.</p></div>
      {columns.map(column => <nav key={column.title} aria-label={column.title}><h3>{column.title}</h3>{column.links.map(([label, href]) => <Link href={href} key={label}>{label}{href.startsWith("https:") && <ArrowUpRight size={13} />}</Link>)}</nav>)}
    </div>
    <div className={styles.footerBottom}><span>© {new Date().getFullYear()} Oblien LLC</span><span>Open-source runtime and SDKs.</span><a href={githubUrl + "/blob/main/LICENSING.md"}>Component licenses <ArrowUpRight size={13} /></a></div>
  </footer></div>;
}
