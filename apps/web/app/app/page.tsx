import type { Metadata } from "next";
import Link from "next/link";
import { ArrowDown, ArrowUpRight } from "lucide-react";
import AppWorkspacePreview from "@/components/landing/AppWorkspacePreview";
import ConnectedQuestions from "@/components/landing/ConnectedQuestions";
import SurfaceRoutes from "@/components/landing/SurfaceRoutes";
import PageHero from "@/components/landing/PageHero";
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
      <SurfaceRoutes kind="appHero" className={styles.hero}>
        <PageHero
          className={styles.heroWindow}
          connector="app-hero"
          eyebrow="Mindwire App"
          id="app-title"
          title={
            <>
              Your work.
              <br />
              With you.
            </>
          }
          description="Your projects, files, and ideas in one workspace. Make something on your Mac. Pick it up on your phone. Keep going."
          artwork={<HeroTrail />}
          caption="Mac, iPhone, and iPad."
        >
          <Link href="#download" className={styles.primary}>
            Get the app <ArrowUpRight size={16} />
          </Link>
          <Link href="#workspace" className={styles.secondary}>
            Take a look inside <ArrowDown size={16} />
          </Link>
        </PageHero>
        <div className={styles.bridge} data-sprue="app-bridge">
          A place to put your ideas to work
        </div>
      </SurfaceRoutes>

      <section
        className={styles.workspaceSection}
        id="workspace"
        aria-labelledby="workspace-title"
      >
        <div className={`${styles.sectionHeading} ${styles.workspaceHeading}`}>
          <h2 id="workspace-title">
            Your computer does the work.
            <br />
            Pick it up from anywhere.
          </h2>
          <div>
            <p>
              General puts chat and finished work up front. Developer brings
              your agents, code, Git, and terminal together. Stay with the same
              task from your computer or your phone.
            </p>
            <Link href="#download">
              Get Mindwire <ArrowUpRight size={16} />
            </Link>
          </div>
        </div>
        <AppWorkspacePreview />
      </section>

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
