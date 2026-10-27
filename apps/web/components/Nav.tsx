"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useSearchContext } from "fumadocs-ui/contexts/search";
import { ArrowUpRight, Search } from "lucide-react";
import OblienLogo from "./OblienLogo";
import ThemeToggle from "./ThemeToggle";
import LandingNav from "./landing/LandingNav";
import { consoleUrl } from "@/lib/console-url";
import styles from "./docs-nav.module.css";

export default function Nav() {
  const pathname = usePathname();
  const { setOpenSearch } = useSearchContext();
  if (pathname === "/") return null;
  if (!pathname?.startsWith("/docs")) return <LandingNav />;

  return <header className={styles.header}>
    <div className={styles.inner}>
      <Link href="/" className={styles.brand} aria-label="Mindwire home"><OblienLogo size={28} />Mindwire</Link>
      <Link href="/docs" className={styles.docsLabel}>Documentation</Link>
      <button type="button" className={styles.search} onClick={() => setOpenSearch(true)} aria-label="Search documentation"><Search size={16} /><span>Find something…</span><kbd>⌘ K</kbd></button>
      <nav aria-label="Documentation navigation" className={styles.links}><Link href="/sdk">SDK & runtime</Link><Link href="/app">Mindwire App</Link></nav>
      <div className={styles.actions}><ThemeToggle /><a href={consoleUrl} className={styles.account}>Your account<ArrowUpRight size={15} /></a></div>
    </div>
  </header>;
}
