"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowRight, Menu, X } from "lucide-react";
import { consoleUrl } from "@/lib/console-url";
import styles from "./framework-home.module.css";

export function FrameworkWordmark() {
  return (
    <svg
      className={styles.wordmark}
      viewBox="0 0 250 32"
      aria-label="Mindwire"
      role="img"
    >
      <text x="0" y="27" textLength="250" lengthAdjust="spacingAndGlyphs">
        MINDWIRE
      </text>
    </svg>
  );
}

export default function FrameworkNav() {
  const [notice, setNotice] = useState(true);
  const [docked, setDocked] = useState(false);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const scroll = () => {
      const mobile = window.innerWidth < 768;
      setDocked(
        window.scrollY > (mobile ? 16 : 40) + (notice ? (mobile ? 48 : 40) : 0),
      );
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    scroll();
    window.addEventListener("scroll", scroll, { passive: true });
    window.addEventListener("keydown", escape);
    return () => {
      window.removeEventListener("scroll", scroll);
      window.removeEventListener("keydown", escape);
    };
  }, [notice]);
  return (
    <header className={styles.header}>
      {notice && (
        <div className={styles.notice}>
          <span className={styles.noticeLabel}>[OPEN SOURCE] //</span>
          <a href="https://github.com/oblien/mindwire">
            The agent layer for your product.{" "}
            <span>
              Explore Mindwire <ArrowRight size={14} />
            </span>
          </a>
          <button
            type="button"
            onClick={() => setNotice(false)}
            aria-label="Dismiss announcement"
          >
            <X size={16} />
          </button>
        </div>
      )}
      <div className={styles.nav} data-docked={docked} data-open={open}>
        <Link
          href="/"
          className={styles.navBrand}
          aria-label="Mindwire home"
          onClick={() => setOpen(false)}
        >
          <FrameworkWordmark />
        </Link>
        <nav className={styles.navLinks} aria-label="Main navigation">
          <Link href="/sdk">SDK</Link>
          <Link href="/cloud">Cloud</Link>
          <Link href="/app">App</Link>
          <Link href="/docs">Docs</Link>
          <a href="/use-cases">Examples</a>
        </nav>
        <div className={styles.navActions}>
          <a href={consoleUrl} className={styles.signIn}>
            Sign in
          </a>
          <button
            className={styles.menuButton}
            type="button"
            aria-label={open ? "Close navigation" : "Open navigation"}
            aria-expanded={open}
            aria-controls="framework-menu"
            onClick={() => setOpen((value) => !value)}
          >
            {open ? <X size={20} /> : <Menu size={20} />}
          </button>
        </div>
        {open && (
          <nav
            id="framework-menu"
            className={styles.mobileMenu}
            aria-label="Mobile navigation"
            onClick={() => setOpen(false)}
          >
            <Link href="/sdk">
              SDK & runtime <ArrowRight size={18} />
            </Link>
            <Link href="/cloud">
              Cloud <ArrowRight size={18} />
            </Link>
            <Link href="/app">
              Mindwire App <ArrowRight size={18} />
            </Link>
            <Link href="/docs">
              Documentation <ArrowRight size={18} />
            </Link>
            <a href="/use-cases">
              Examples <ArrowRight size={18} />
            </a>
            <Link href="/pricing">
              Pricing <ArrowRight size={18} />
            </Link>
          </nav>
        )}
      </div>
    </header>
  );
}
