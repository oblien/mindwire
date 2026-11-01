"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useSearchContext } from "fumadocs-ui/contexts/search";
import { ArrowUpRight, Search } from "@/components/site/InterfaceIcon";
import MindWireMark from "@/components/brand/MindWireMark";
import SiteNav from "./site/SiteNav";
import ThemeToggle from "./ThemeToggle";
import { consoleUrl } from "@/lib/console-url";
import styles from "./docs-nav.module.css";

export default function Nav() {
  const pathname = usePathname();
  const { setOpenSearch } = useSearchContext();
  if (pathname === "/" || pathname?.startsWith("/design-lab")) return null;
  if (!pathname?.startsWith("/docs")) return <SiteNav />;

  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <Link href="/" className={styles.brand} aria-label="MindWire home">
          <MindWireMark />
          <span>MindWire</span>
        </Link>
        <Link href="/docs" className={styles.docsLabel}>
          Documentation
        </Link>
        <button
          type="button"
          className={styles.search}
          onClick={() => setOpenSearch(true)}
          aria-label="Search documentation"
        >
          <Search size={16} />
          <span>Find something…</span>
          <kbd>⌘ K</kbd>
        </button>
        <nav aria-label="Documentation navigation" className={styles.links}>
          <Link href="/sdk">SDK & runtime</Link>
          <Link href="/app">MindWire App</Link>
        </nav>
        <div className={styles.actions}>
          <ThemeToggle />
          <a href={consoleUrl} className={styles.account}>
            Console
            <ArrowUpRight size={15} />
          </a>
        </div>
      </div>
    </header>
  );
}
