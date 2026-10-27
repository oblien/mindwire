import type { Metadata } from "next";
import PageHero from "@/components/landing/PageHero";
import Link from "next/link";
import {
  ArrowDown,
  ArrowRight,
  ArrowUpRight,
  Check,
  Cloud,
  Code2,
  FileCode2,
  Folder,
  GitBranch,
  GitCommitHorizontal,
  Laptop,
  MessageSquare,
  ShieldCheck,
  Smartphone,
  Terminal,
} from "lucide-react";
import DeveloperWorkspace from "@/components/landing/DeveloperWorkspace";
import AgentLogo from "@/components/marketing/AgentLogo";
import Questions from "@/components/landing/Questions";
import base from "@/components/landing/landing.module.css";
import product from "@/components/landing/product.module.css";
import styles from "@/components/landing/developers.module.css";

export const metadata: Metadata = {
  title: "Mindwire App — Developer mode",
  description:
    "Build with Codex, Claude Code and OpenCode in a workspace made for developers. Explore projects, review Git changes, use terminals and keep working from your Mac or iPhone.",
  alternates: { canonical: "/developers/" },
};

const capabilities = [
  {
    icon: Folder,
    title: "A home for every project.",
    text: "Bring a repository or a local folder. Organize your projects and return to the files and conversations that belong together.",
  },
  {
    icon: GitBranch,
    title: "From diff to commit.",
    text: "Inspect changes, stage files and write a commit. Work with branches and repository history without losing the thread.",
  },
  {
    icon: Terminal,
    title: "A terminal within reach.",
    text: "Run your commands beside the conversation. Reconnect to the same shell when you return to a running workspace.",
  },
  {
    icon: MessageSquare,
    title: "Stay part of the process.",
    text: "Follow tool activity, answer questions and steer the next step. Supported approvals appear where the work happens.",
  },
  {
    icon: FileCode2,
    title: "Context that travels with you.",
    text: "Give your agent project instructions, reference material and reusable skills. Keep the useful details close to the task.",
  },
  {
    icon: Smartphone,
    title: "Away from the desk, in the loop.",
    text: "Pair your iPhone with your computer. Check progress, review files and continue the conversation on the same workspace.",
  },
];

export default function Developers() {
  return (
    <div className={base.landing}>
      <PageHero
        eyebrow="Mindwire App / Developer mode"
        id="developers-title"
        title={
          <>
            From the prompt.
            <br />
            To the commit.
          </>
        }
        description="Your agents, code, Git, and terminal in one workspace. Follow the work, review the changes, and pick up from any connected device."
      >
        <Link href="/app#download" className={base.primaryButton}>
          Get the app <ArrowUpRight size={16} />
        </Link>
        <Link href="#developer-workspace" className={base.secondaryButton}>
          Explore the workbench <ArrowDown size={16} />
        </Link>
      </PageHero>
      <div className={styles.previewWrap}>
        <DeveloperWorkspace />
      </div>

      <section
        className={styles.capabilities}
        aria-labelledby="capabilities-title"
      >
        <div className={styles.capabilityHeading}>
          <h2 id="capabilities-title">
            Everything between
            <br />
            <em>the prompt and the commit.</em>
          </h2>
          <p>
            Let your agent take the lead on the task.
            <br />
            Keep the tools to understand every step.
          </p>
        </div>
        <div className={styles.capabilityGrid}>
          {capabilities.map(({ icon: Icon, title, text }, i) => (
            <article key={title}>
              <span className={styles.capabilityIcon} data-tone={i % 3}>
                <Icon size={21} strokeWidth={1.6} />
              </span>
              <h3>{title}</h3>
              <p>{text}</p>
            </article>
          ))}
        </div>
      </section>

      <section
        className={`${product.section} ${styles.projectSection}`}
        aria-labelledby="project-title"
      >
        <div className={styles.projectScene}>
          <div className={styles.projectWindow}>
            <div className={styles.projectWindowTitle}>
              <Folder size={17} />
              <strong>Your projects</strong>
              <span>3</span>
            </div>
            <div className={styles.projectGroup}>PERSONAL</div>
            <div className={styles.projectRow}>
              <span className={styles.projectAvatar}>
                <Terminal size={22} />
              </span>
              <span>
                <strong>Reading corner</strong>
                <small>A little app for good books</small>
              </span>
              <span className={styles.projectBranch}>
                <GitBranch size={12} />
                weekly-goal
              </span>
            </div>
            <div className={styles.projectRow}>
              <span className={styles.projectAvatar} data-tone="mint">
                <Code2 size={22} />
              </span>
              <span>
                <strong>Field notes</strong>
                <small>Collect the things worth keeping</small>
              </span>
              <span className={styles.projectBranch}>
                <GitBranch size={12} />
                main
              </span>
            </div>
            <div className={styles.projectGroup}>WORK</div>
            <div className={styles.projectRow}>
              <span className={styles.projectAvatar} data-tone="peach">
                <FileCode2 size={22} />
              </span>
              <span>
                <strong>Studio website</strong>
                <small>One more idea, ready to build</small>
              </span>
              <span className={styles.projectBranch}>
                <GitBranch size={12} />
                main
              </span>
            </div>
            <div className={styles.projectWindowBottom}>
              <Laptop size={13} />
              My computer<span>Everything in its place.</span>
            </div>
          </div>
          <div className={styles.commitNote}>
            <GitCommitHorizontal size={18} />
            <span>
              <strong>Add a weekly reading goal</strong>
              <small>2 files changed · ready to review</small>
            </span>
            <Check size={16} />
          </div>
        </div>
        <div className={product.sectionCopy}>
          <span className={product.eyebrow}>
            Less finding your place. More making progress.
          </span>
          <h2 id="project-title">
            Pick a project.
            <br />
            <em>Get back into it.</em>
          </h2>
          <p>
            Keep side projects and daily work organized. Open the right
            workspace with its files, conversations and Git history already in
            view.
          </p>
          <ul className={product.checkList}>
            <li>
              <Check size={16} />
              Folders and project organization
            </li>
            <li>
              <Check size={16} />
              Branches, changes and commit history
            </li>
            <li>
              <Check size={16} />
              Local computers, servers and Cloud workspaces
            </li>
          </ul>
          <Link href="/get-started" className={product.textLink}>
            Set up your workspace
            <ArrowUpRight size={16} />
          </Link>
        </div>
      </section>

      <section className={styles.agentSection} aria-labelledby="agents-title">
        <div className={styles.agentCopy}>
          <span className={product.eyebrow}>Your workflow. Your choice.</span>
          <h2 id="agents-title">
            Keep your favorites.
            <br />
            <em>Give them a better home.</em>
          </h2>
          <p>
            Use Codex, Claude Code or OpenCode with the accounts you already
            have. Choose the agent for the work and keep a familiar workspace
            around it.
          </p>
          <div className={styles.agentNames}>
            <span>
              <AgentLogo agent="codex" size={23} />
              Codex
            </span>
            <span>
              <AgentLogo agent="claude" size={24} />
              Claude Code
            </span>
            <span>
              <AgentLogo agent="opencode" size={22} />
              OpenCode
            </span>
          </div>
          <Link href="/docs/guides/agents" className={product.textLink}>
            Explore supported agents
            <ArrowUpRight size={16} />
          </Link>
        </div>
        <div className={styles.controlNotes}>
          <div>
            <ShieldCheck size={22} />
            <h3>You’re part of the loop.</h3>
            <p>
              Follow progress and respond to supported questions and approvals.
              Set permissions to suit the workspace and the task.
            </p>
          </div>
          <div>
            <Cloud size={22} />
            <h3>Choose where the work lives.</h3>
            <p>
              Use a connected computer, a server or a Cloud Computer. Your phone
              is a window into that workspace, wherever you are.
            </p>
            <Link href="/cloud#cloud-plans" className={product.textLink}>
              Explore Cloud
              <ArrowUpRight size={14} />
            </Link>
          </div>
          <span>
            Agent features and authentication methods vary by provider. Your
            provider’s usage remains separate.
          </span>
        </div>
      </section>

      <section className={styles.sdkBridge} aria-labelledby="sdk-bridge-title">
        <div>
          <Code2 size={24} />
          <span>Building a product of your own?</span>
        </div>
        <h2 id="sdk-bridge-title">Put these agents inside it.</h2>
        <p>
          Mindwire SDK brings the same runtime to your app, with streaming,
          tools and supported approvals.
        </p>
        <Link href="/sdk" className={base.secondaryButton}>
          Meet Mindwire SDK
          <ArrowUpRight size={16} />
        </Link>
      </section>

      <section
        className={`${product.section} ${product.faqSection}`}
        aria-labelledby="dev-questions"
      >
        <div className={product.sectionCopy}>
          <span className={product.eyebrow}>Before you jump in</span>
          <h2 id="dev-questions">
            Make yourself
            <br />
            <em>at home.</em>
          </h2>
        </div>
        <Questions
          items={[
            {
              question: "Is Developer mode a different app?",
              answer:
                "No. General mode and Developer mode are two ways to use Mindwire. Developer mode puts projects, files, Git and terminals up front. Switch modes while keeping your conversations and saved context.",
            },
            {
              question: "Do I have to move my projects to the cloud?",
              answer: (
                <>
                  No. Connect your own Mac, Windows or Linux computer and work
                  where your files already live. It needs to stay awake and
                  online while you work remotely. A{" "}
                  <Link href="/cloud">Cloud Computer</Link> is optional.
                </>
              ),
            },
            {
              question: "Does a terminal close when I leave the app?",
              answer:
                "Leaving the terminal view or disconnecting the phone detaches the client; the shell stays on the running computer. Returning reconnects you. Explicitly closing the terminal tab, restarting the service or rebooting the host ends that shell.",
            },
            {
              question: "Can I bring my current agent account?",
              answer:
                "Use an account or API key supported by your selected agent. Mindwire works through the agent’s own authentication flow on the connected computer. Available models and usage depend on your provider.",
            },
          ]}
        />
      </section>
      <section className={styles.closing}>
        <img
          src="/images/framework/canvas-motif.svg"
          alt=""
          width={105}
          height={29}
        />
        <h2>
          A good place for
          <br />
          your next great idea.
        </h2>
        <div>
          <Link href="/app#download" className={base.primaryButton}>
            Get Mindwire
            <ArrowUpRight size={17} />
          </Link>
          <Link href="/get-started" className={product.textLink}>
            Walk through setup
            <ArrowRight size={16} />
          </Link>
        </div>
      </section>
    </div>
  );
}
