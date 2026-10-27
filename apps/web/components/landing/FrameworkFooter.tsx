import Link from "next/link";
import { FrameworkWordmark } from "./FrameworkNav";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import styles from "./framework-home.module.css";
const asset = (name: string) => "/images/framework/" + name;

export default function FrameworkFooter({
  interior = false,
}: {
  interior?: boolean;
}) {
  return (
    <SurfaceRoutes
      as="footer"
      kind="footer"
      artwork={interior ? undefined : { dock: "footer" }}
      className={styles.footerRoutes}
      data-interior={interior}
    >
      <div className={styles.footer}>
        <WindowShape kind="footer" />
        <span
          className={styles.footerAnchor}
          data-sprue="footer"
          aria-hidden="true"
        />
        <div className={styles.footerTop}>
          <Link href="/" aria-label="Mindwire home">
            <FrameworkWordmark />
          </Link>
          <img src={asset("footer-orbit.svg")} alt="" width={33} height={33} />
        </div>
        <div className={styles.footerBody}>
          <div className={styles.footerIdentity}>
            <p>
              THE AGENT LAYER FOR YOUR PRODUCT.
              <br />
              OPEN SOURCE. YOUR TOOLS. YOUR BRAND.
            </p>
            <img
              src={asset("globe.svg")}
              alt=""
              width={328}
              height={327}
              loading="lazy"
            />
          </div>
          <nav aria-label="Footer navigation">
            <div>
              <Link href="/sdk">SDK</Link>
              <Link href="/cloud">Cloud & pricing</Link>
              <Link href="/app">App</Link>
              <Link href="/docs">Docs</Link>
            </div>
            <div>
              <a href="/use-cases">Examples</a>
              <Link href="/#how-it-works">How it works</Link>
              <a href="https://github.com/oblien/mindwire">GitHub</a>
              <a href="https://github.com/oblien/mindwire/issues">Support</a>
            </div>
            <div>
              <Link href="/app#download">Downloads</Link>
              <Link href="/get-started">App setup</Link>
              <Link href="/sandboxing">Sandboxing</Link>
              <Link href="/changelog">Changelog</Link>
            </div>
          </nav>
          <img
            src={asset("footer-trail.svg")}
            alt=""
            width={227}
            height={54}
            loading="lazy"
          />
        </div>
        <div className={styles.footerBottom}>
          <span>© {new Date().getFullYear()} MINDWIRE</span>
          <div>
            <a href="https://github.com/oblien/mindwire/blob/main/LICENSING.md">
              Licenses
            </a>
            <a href="https://github.com/oblien/mindwire/issues">Support</a>
          </div>
        </div>
      </div>
    </SurfaceRoutes>
  );
}
