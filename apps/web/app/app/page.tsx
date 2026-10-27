import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "lucide-react";
import AppWorkspacePreview from "@/components/landing/AppWorkspacePreview";
import AppContinuity from "@/components/landing/AppContinuity";
import ConnectedQuestions from "@/components/landing/ConnectedQuestions";
import SurfaceRoutes from "@/components/landing/SurfaceRoutes";
import WindowShape from "@/components/landing/WindowShape";
import HeroTrail from "@/components/landing/HeroTrail";
import AppDownloads from "@/components/landing/AppDownloads";
import styles from "@/components/landing/app-framework.module.css";

export const metadata: Metadata = {
  title: "Mindwire App — Your work, with you everywhere",
  description:
    "Your agents, files, and projects in one connected workspace. Create with General mode, work with code in Developer mode, and pick up from Mac, iPhone, or iPad.",
  alternates: { canonical: "/app/" },
};

export default function AppPage() {
  return (
    <div className={styles.page}>
      <SurfaceRoutes
        as="section"
        kind="appHero"
        className={styles.hero}
        aria-labelledby="app-title"
      >
        <div className={styles.heroWindow} data-sprue="app-hero">
          <WindowShape kind="hero" />
          <HeroTrail />
          <div className={styles.heroIntro}>
            <span>Mindwire App</span>
            <h1 id="app-title">
              YOUR WORK.
              <br />
              WITH YOU.
            </h1>
            <p>
              Your agents, files, and ideas in one workspace. Make something on
              your Mac. Pick it up on your phone. Keep going.
            </p>
            <div className={styles.actions}>
              <Link href="#download" className={styles.primary}>
                Get the app
                <ArrowUpRight size={18} />
              </Link>
              <Link href="#workspace" className={styles.secondary}>
                Take a look inside
                <ArrowDown size={18} />
              </Link>
            </div>
          </div>
          <div className={styles.heroFoot}>
            <img
              src="/images/framework/canvas-motif.svg"
              alt=""
              width={105}
              height={29}
            />
            <span>Mac, iPhone, and iPad.</span>
          </div>
        </div>
        <div className={styles.bridge} data-sprue="app-bridge">
          A place to put your ideas to work
        </div>
      </SurfaceRoutes>

      <section
        className={styles.workspaceSection}
        id="workspace"
        aria-labelledby="workspace-title"
      >
        <div className={styles.sectionHeading}>
          <h2 id="workspace-title">
            Start in chat.
            <br />
            Make it real.
          </h2>
          <p>
            Create a tool, work through an idea, or get into the code. Switch
            between General and Developer views as the work changes.
          </p>
        </div>
        <AppWorkspacePreview />
      </section>

      <AppContinuity />

      <AppDownloads />

      <ConnectedQuestions
        id="app-questions"
        eyebrow="Before you begin"
        title={
          <>
            Make yourself
            <br />
            at home.
          </>
        }
        description="Choose a computer, connect an agent, and start with something you need done."
        items={[
          {
            question: "Which mode should I use?",
            answer:
              "General mode keeps conversations, files, and finished results up front. Developer mode exposes projects, code changes, Git, and the terminal. Switch views as the work changes.",
          },
          {
            question: "Does the work run on my phone?",
            answer:
              "The work runs on a connected computer: your Mac, a server, or an optional Cloud Computer. Your phone lets you follow progress and interact with the workspace. The host needs to stay running while tasks execute.",
          },
          {
            question: "Can I use my existing agent account?",
            answer:
              "Connect an account or API key supported by your selected agent. Available authentication methods, provider terms, limits, and usage charges still apply.",
          },
          {
            question: "Where can I get the app?",
            answer: (
              <>
                Choose a device in the{" "}
                <Link href="#download">download section</Link> for current Mac
                and iOS availability, then follow the{" "}
                <Link href="/get-started">app setup guide</Link> to connect your
                computer and agent.
              </>
            ),
          },
        ]}
      />
    </div>
  );
}
