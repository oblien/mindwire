import Link from "next/link";
import {
  ArrowDownToLine,
  ArrowUpRight,
  Laptop,
  Smartphone,
} from "lucide-react";
import { githubUrl, iosApp, macApp } from "@/lib/product";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import HeatformArt from "./HeatformArt";
import InstallBlock from "./InstallBlock";
import app from "./app-framework.module.css";
import styles from "./app-downloads.module.css";

export default function AppDownloads() {
  return (
    <SurfaceRoutes
      as="section"
      kind="appDownloads"
      className={styles.downloads}
      id="download"
      aria-labelledby="download-title"
    >
      <div className={styles.headingRow}>
        <div className={styles.intro} data-sprue="download-heading">
          <WindowShape kind="connect" />
          <h2 id="download-title">GET MINDWIRE.</h2>
          <p>
            At your desk. On your phone.
            <br />
            The same workspace, wherever you are.
          </p>
          <Link href="/get-started" className={styles.textLink}>
            A little help getting started <ArrowUpRight size={18} />
          </Link>
        </div>
        <div className={styles.art} data-sprue="download-art">
          <WindowShape kind="diagram" />
          <HeatformArt variant="bloom" className={styles.texture} />
        </div>
      </div>

      <div className={styles.platforms}>
        <article
          className={styles.platform}
          id="mac"
          data-sprue="download-mac"
          aria-labelledby="mac-title"
        >
          <div className={styles.platformHeading}>
            <h3 id="mac-title">On your Mac.</h3>
            <Laptop size={34} strokeWidth={1.5} aria-hidden="true" />
          </div>
          <p>
            Room for the whole project. Chat, files, tools, and finished work in
            one native app.
          </p>
          <div className={styles.platformAction}>
            {macApp ? (
              <a href={macApp.href} className={app.primary}>
                {macApp.label} <ArrowDownToLine size={18} />
              </a>
            ) : (
              <span className={styles.availability}>Mac app coming soon</span>
            )}
            <span>macOS 26 or later</span>
          </div>
          <a href={`${githubUrl}/releases/latest`} className={styles.textLink}>
            Releases & updates <ArrowUpRight size={17} />
          </a>
        </article>

        <article
          className={styles.platform}
          id="iphone"
          data-sprue="download-ios"
          aria-labelledby="iphone-title"
        >
          <div className={styles.platformHeading}>
            <h3 id="iphone-title">In your pocket.</h3>
            <Smartphone size={32} strokeWidth={1.5} aria-hidden="true" />
          </div>
          <p>
            Start a conversation, follow the work, and review the next step from
            your iPhone or iPad.
          </p>
          <div className={styles.platformAction}>
            {iosApp ? (
              <a href={iosApp.href} className={app.primary}>
                {iosApp.label} <ArrowUpRight size={18} />
              </a>
            ) : (
              <span className={styles.availability}>iOS app coming soon</span>
            )}
            <span>iPhone & iPad · iOS 26 or later</span>
          </div>
          <Link href="/get-started#iphone" className={styles.textLink}>
            Connect your phone <ArrowUpRight size={17} />
          </Link>
        </article>
      </div>

      <div className={styles.runtime} id="cli" aria-labelledby="cli-title">
        <div>
          <h3 id="cli-title">More of a terminal person?</h3>
          <p>
            Install the runtime on your Mac, Windows, or Linux computer, then
            connect it to the app.
          </p>
          <Link href="/get-started" className={styles.textLink}>
            Follow the setup guide <ArrowUpRight size={17} />
          </Link>
        </div>
        <div className={styles.terminal}>
          <InstallBlock />
        </div>
      </div>
    </SurfaceRoutes>
  );
}
