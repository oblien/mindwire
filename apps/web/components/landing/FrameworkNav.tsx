"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowRight, Menu, X } from "lucide-react";
import { consoleUrl } from "@/lib/console-url";
import ThemeToggle from "../ThemeToggle";
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

const links = [
  { href: "/sdk", label: "SDK" },
  { href: "/cloud", label: "Cloud" },
  { href: "/app", label: "App" },
  { href: "/use-cases", label: "Examples" },
  { href: "/docs", label: "Docs" },
];

export default function FrameworkNav({
  interior = false,
}: {
  interior?: boolean;
}) {
  const [notice, setNotice] = useState(true);
  const [docked, setDocked] = useState(false);
  const [open, setOpen] = useState(false);
  const navRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const pathname = usePathname()?.replace(/\/$/, "") || "/";
  const current = (href: string) =>
    pathname === href ||
    pathname.startsWith(href + "/") ||
    (href === "/app" &&
      ["/get-started", "/developers"].includes(pathname));
  useEffect(() => {
    const scroll = () => {
      const mobile = window.innerWidth < 768;
      setDocked(
        window.scrollY > (mobile ? 16 : 40) + (notice ? (mobile ? 48 : 40) : 0),
      );
    };
    scroll();
    window.addEventListener("scroll", scroll, { passive: true });
    window.addEventListener("resize", scroll);
    return () => {
      window.removeEventListener("scroll", scroll);
      window.removeEventListener("resize", scroll);
    };
  }, [notice]);
  useEffect(() => {
    if (!open) return;
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        toggleRef.current?.focus();
      }
    };
    const outside = (event: PointerEvent) => {
      if (!navRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const resize = () => {
      if (window.innerWidth >= 768) setOpen(false);
    };
    window.addEventListener("keydown", escape);
    window.addEventListener("pointerdown", outside);
    window.addEventListener("resize", resize);
    return () => {
      window.removeEventListener("keydown", escape);
      window.removeEventListener("pointerdown", outside);
      window.removeEventListener("resize", resize);
    };
  }, [open]);
  return (
    <header
      className={styles.header}
      data-interior={interior}
      data-announcement={notice}
    >
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
      <div
        ref={navRef}
        className={styles.nav}
        data-docked={docked}
        data-open={open}
      >
        <Link
          href="/"
          className={styles.navBrand}
          aria-label="Mindwire home"
          onClick={() => setOpen(false)}
        >
          <FrameworkWordmark />
        </Link>
        <nav className={styles.navLinks} aria-label="Main navigation">
          {links.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              aria-current={current(link.href) ? "page" : undefined}
            >
              {link.label}
            </Link>
          ))}
        </nav>
        <div className={styles.navActions}>
          <ThemeToggle />
          <a href={consoleUrl} className={styles.signIn}>
            Sign in
          </a>
          <button
            ref={toggleRef}
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
              Cloud & pricing <ArrowRight size={18} />
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
            <a href={consoleUrl}>
              Sign in <ArrowRight size={18} />
            </a>
          </nav>
        )}
      </div>
    </header>
  );
}
