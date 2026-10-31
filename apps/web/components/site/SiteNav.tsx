"use client";

import { useEffect, useId, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import Link from "next/link";
import { ArrowUpRight, Menu, X } from "lucide-react";
import { githubUrl } from "@/lib/product";
import { consoleUrl } from "@/lib/console-url";
import { MindwireMark } from "@/components/design-lab/refined/Artwork";
import styles from "./site.module.css";

const links = [
  { href: "/sdk", label: "SDK" },
  { href: "/cloud", label: "Cloud" },
  { href: "/app", label: "App" },
  { href: "/use-cases", label: "Examples" },
  { href: "/docs", label: "Documentation" },
];

export default function SiteNav({ embedded = false }: { embedded?: boolean }) {
  const [open, setOpen] = useState(false);
  const path = usePathname();
  const menuId = useId();
  const header = useRef<HTMLElement>(null);
  const toggle = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: PointerEvent) => {
      if (!header.current?.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        toggle.current?.focus();
      }
    };
    const resize = () => {
      if (innerWidth > 899) setOpen(false);
    };
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", escape);
    window.addEventListener("resize", resize);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", escape);
      window.removeEventListener("resize", resize);
    };
  }, [open]);
  const active = (href: string) =>
    path === href || path?.startsWith(href + "/");
  return (
    <header
      ref={header}
      className={styles.header}
      data-embedded={embedded}
      data-detached={path?.startsWith("/use-cases/") && path !== "/use-cases/"}
      data-open={open}
    >
      <Link
        href="/"
        className={styles.wordmark}
        aria-label="Mindwire home"
        onClick={() => setOpen(false)}
      >
        <MindwireMark />
        <span>mindwire</span>
      </Link>
      <nav className={styles.desktopNav} aria-label="Main navigation">
        {links.map((link) => (
          <Link
            key={link.href}
            href={link.href}
            aria-current={active(link.href) ? "page" : undefined}
          >
            {link.label}
            {link.href === "/docs" && <ArrowUpRight size={13} />}
          </Link>
        ))}
      </nav>
      <a className={styles.source} href={githubUrl}>
        Open source <ArrowUpRight size={16} />
      </a>
      <button
        ref={toggle}
        className={styles.menuToggle}
        type="button"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={open ? "Close navigation" : "Open navigation"}
        onClick={() => setOpen((value) => !value)}
      >
        {open ? <X size={22} /> : <Menu size={22} />}
      </button>
      {open && (
        <nav
          id={menuId}
          className={styles.mobileNav}
          aria-label="Mobile navigation"
          onClick={() => setOpen(false)}
        >
          <Link href={embedded ? "#concept" : "/#concept"}>
            The idea <ArrowUpRight size={17} />
          </Link>
          {links.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              aria-current={active(link.href) ? "page" : undefined}
            >
              {link.label}
              <ArrowUpRight size={17} />
            </Link>
          ))}
          <a href={consoleUrl}>
            Open Console <ArrowUpRight size={17} />
          </a>
          <a href={githubUrl}>
            Open source <ArrowUpRight size={17} />
          </a>
        </nav>
      )}
    </header>
  );
}
