import type { Metadata } from "next";
import Link from "next/link";
import {
  ArrowRight,
  ArrowUpRight,
  Cloud,
  Laptop,
  Server,
  Smartphone,
} from "lucide-react";
import InstallCommand from "@/components/marketing/InstallCommand";
import Corners from "@/components/Corners";
import styles from "@/components/marketing/marketing.module.css";
import { cloudDashboardUrl, iosApp } from "@/lib/product";

export const metadata: Metadata = {
  title: "Get started — Mindwire",
  description:
    "Set up Mindwire on your iPhone and connect your computer with a QR code. Bring a server over SSH or choose an Oblien cloud workspace.",
  alternates: { canonical: "/get-started/" },
};

export default function GetStarted() {
  return (
    <div className={styles.marketing}>
      <section className={`${styles.wrap} ${styles.pageHero}`}>
        <span className={styles.kicker}>Take your agents with you</span>
        <h1 className={styles.pageTitle}>
          Your next workspace
          <br />
          <span>is already on your desk.</span>
        </h1>
        <p>
          Mindwire on your iPhone. Your agents on your computer. A secure
          connection between the two.
        </p>
      </section>
      <div className={styles.wrap}>
        <div className={styles.onboardingGrid}>
          <section
            id="iphone"
            className={styles.onboardingCard}
            aria-labelledby="iphone-title"
          >
            <Corners />
            <Smartphone size={32} strokeWidth={1.5} />
            <h2 id="iphone-title">Meet your iPhone app.</h2>
            <p>
              Chat with your agents, review changes, and reach your workspaces
              from wherever you are.
            </p>
            {iosApp ? (
              <a href={iosApp.href} className={styles.primaryButton}>
                {iosApp.label}
                <ArrowUpRight size={16} />
              </a>
            ) : (
              <span className={styles.soonBadge}>
                iPhone download coming soon
              </span>
            )}
            <p>
              Already have Mindwire? Open{" "}
              <strong>Workspaces → Add computer</strong> to scan the code from
              your computer.
            </p>
            <Link href="/#features" className={styles.textButton}>
              Explore the app <ArrowRight size={15} />
            </Link>
          </section>
          <section
            id="computer"
            className={styles.onboardingCard}
            aria-labelledby="computer-title"
          >
            <Corners />
            <Laptop size={32} strokeWidth={1.5} />
            <h2 id="computer-title">Connect your computer.</h2>
            <p>
              On Mac, Windows, or Linux, install Node.js 18 or later and run
              these commands.
            </p>
            <InstallCommand />
            <ol className={styles.onboardingSteps}>
              <li>
                <span>1</span>
                <div>
                  <strong>Scan the QR.</strong> Open Add computer in the iPhone
                  app, then scan the code from your terminal.
                </div>
              </li>
              <li>
                <span>2</span>
                <div>
                  <strong>Approve your phone.</strong> Confirm its device key on
                  your computer. That phone can now access files and run
                  commands as your computer account.
                </div>
              </li>
              <li>
                <span>3</span>
                <div>
                  <strong>Pick up a project.</strong> Choose a folder, select an
                  agent, and sign in with the authentication it supports.
                </div>
              </li>
            </ol>
            <p>
              The default connection works across networks without a Mindwire or
              Oblien login. Keep your computer awake, online, and Mindwire
              running.
            </p>
            <Link
              href="/docs/guides/personal-computers"
              className={styles.textButton}
            >
              Connection and startup guide <ArrowUpRight size={15} />
            </Link>
          </section>
        </div>
        <div className={styles.onboardingAlternatives}>
          <section
            id="server"
            className={styles.onboardingCard}
            aria-labelledby="server-title"
          >
            <Corners />
            <Server size={27} strokeWidth={1.5} />
            <h2 id="server-title">Have a server?</h2>
            <p>
              Choose <strong>Add server</strong> in the app and enter your SSH
              connection details. Use the host directly, or set up a Docker
              workspace on a supported server. Your projects use the same files,
              Git, agent, and terminal tools.
            </p>
            <Link
              href="/docs/guides/destinations"
              className={styles.textButton}
            >
              Explore workspace destinations <ArrowUpRight size={15} />
            </Link>
          </section>
          <section
            className={styles.onboardingCard}
            aria-labelledby="cloud-setup-title"
          >
            <Corners />
            <Cloud size={27} strokeWidth={1.5} />
            <h2 id="cloud-setup-title">Prefer a cloud workspace?</h2>
            <p>
              Choose <strong>Oblien Cloud</strong> in the app. Sign in to your
              Oblien account, create a workspace, and give your agent an online
              home. You can manage its resources and lifecycle from your phone.
            </p>
            <div className={styles.actions}>
              <Link href="/pricing" className={styles.textButton}>
                Workspace pricing <ArrowRight size={15} />
              </Link>
              <a href={cloudDashboardUrl} className={styles.textButton}>
                Open Oblien <ArrowUpRight size={15} />{" "}
              </a>
            </div>
          </section>
        </div>
        <p className={styles.onboardingHelp}>
          Using your own VPN, Cloudflare, ngrok, or a self-hosted directory?
          <br />
          <Link href="/docs/guides/personal-computers#choose-a-connection">
            Choose the connection that works for you.
          </Link>
        </p>
      </div>
    </div>
  );
}
