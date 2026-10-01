import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import {
  ArrowDown,
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronRight,
  Cloud,
  Code2,
  FileCode2,
  GitBranch,
  Laptop,
  LockKeyhole,
  Monitor,
  MoveRight,
  ScanLine,
  Server,
  ShieldCheck,
  Smartphone,
  Terminal,
} from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import WireDiagram from "@/components/WireDiagram";
import Corners from "@/components/Corners";
import ProductPreview from "@/components/marketing/ProductPreview";
import WorkflowShowcase from "@/components/marketing/WorkflowShowcase";
import InstallCommand from "@/components/marketing/InstallCommand";
import styles from "@/components/marketing/marketing.module.css";
import { consoleUrl } from "@/lib/console-url";
import { downloadUrl, githubUrl, productAgents } from "@/lib/product";

export const metadata: Metadata = {
  title: "Mindwire — Your coding agents, anywhere",
  description:
    "Take Codex, Claude Code, and your projects with you. Chat, review code, use your terminal, and switch workspaces from your iPhone. Built on open-source Mindwire.",
  alternates: { canonical: "/" },
};

const questions = [
  {
    question: "Does my phone need to be on the same Wi-Fi?",
    answer:
      "No. Connect from another Wi-Fi network or mobile data. The default computer setup uses a Cloudflare tunnel and Mindwire’s encrypted address directory, with no Mindwire or Oblien account required. You can also use your own tunnel or VPN.",
  },
  {
    question: "Can my agent keep working when I close the app?",
    answer:
      "Yes, while its workspace stays online and Mindwire is running. Closing the iPhone app doesn’t stop an agent. A sleeping laptop can’t keep working; switch the project to an online server or cloud workspace before closing it.",
  },
  {
    question: "What travels when I switch workspaces?",
    answer:
      "Project files, uncommitted changes, supported Git state, and selected Codex or Claude conversations travel together. Both copies remain. Compatible changes can come back on your next switch, and conflicts stop before applying. Finish active turns and terminals first. Agent logins stay local to each workspace.",
  },
  {
    question: "Do I need to buy another AI subscription?",
    answer:
      "Use the authentication supported by your agent, including ChatGPT or Claude subscription sign-in where available, or your own API credentials. Mindwire workspace pricing does not include model usage or change your provider’s limits.",
  },
  {
    question: "What is open source?",
    answer:
      "Mindwire’s runtime, TypeScript and Go SDKs, and web Console are Apache-2.0 software. You can run your own infrastructure, inspect the connection protocol, self-host the address directory, and build on the same APIs used by the app.",
  },
];

export default function Home() {
  return (
    <div className={styles.marketing}>
      <section
        className={`${styles.wrap} ${styles.heroSection}`}
        aria-labelledby="hero-title"
      >
        <div className={styles.hero}>
          <Corners />
          <div className={styles.heroCopy}>
            <span className={styles.kicker}>
              <Smartphone size={15} /> Your coding agents, on iPhone
            </span>
            <h1 id="hero-title" className={styles.heroTitle}>
              Keep building.
              <br />
              <span>From anywhere.</span>
            </h1>
            <p className={styles.heroDescription}>
              Your favorite agents. Your real projects. Pick up the
              conversation, review the code, and keep things moving—right from
              your phone.
            </p>
            <div className={styles.actions}>
              <Link href={downloadUrl} className={styles.primaryButton}>
                <Smartphone size={18} /> Download app <ArrowUpRight size={17} />
              </Link>
              <a href="#features" className={styles.textButton}>
                Explore the app <ArrowDown size={16} />
              </a>
            </div>
            <a href={githubUrl} className={styles.openSourceNote}>
              <Code2 size={15} />
              <span>Built in the open. Yours to make your own.</span>
              <ArrowUpRight size={13} />
            </a>
            <div className={styles.heroFootnote}>
              <span /> Your computer, your server, or the cloud.
            </div>
          </div>
          <ProductPreview />
        </div>
      </section>

      <section className={styles.wrap} aria-label="Supported coding agents">
        <div className={styles.agentStrip}>
          <Corners />
          {productAgents.map((agent) => (
            <Link key={agent.id} href={agent.href}>
              <AgentLogo agent={agent.id} size={24} />
              <span>{agent.name}</span>
            </Link>
          ))}
        </div>
      </section>

      <section
        id="features"
        className={`${styles.wrap} ${styles.section}`}
        aria-labelledby="features-title"
      >
        <div id="how" className={styles.sectionHeader}>
          <div>
            <span className={styles.kicker}>Made for the moments between</span>
            <h2 id="features-title" className={styles.sectionTitle}>
              The whole workflow.
              <br />
              <span>In one hand.</span>
            </h2>
          </div>
          <p>
            Catch a thought on the train. Unblock an agent over coffee. Review
            that last change before you get home.
          </p>
        </div>
        <WorkflowShowcase />
        <div className={styles.toolsRibbon}>
          <span>
            <FileCode2 size={18} /> Files & search
          </span>
          <span>
            <Monitor size={18} /> Desktop on supported workspaces
          </span>
          <span>
            <ArrowUpRight size={18} /> Ports & web previews
          </span>
        </div>
      </section>

      <section
        id="agents"
        className={`${styles.wrap} ${styles.section}`}
        aria-labelledby="agents-title"
      >
        <div className={styles.harnessSection}>
          <Corners />
          <div className={styles.harnessCopy}>
            <span className={styles.kicker}>Your agents, connected</span>
            <h2 id="agents-title" className={styles.sectionTitle}>
              Different agents.
              <br />
              <span>One familiar app.</span>
            </h2>
            <p className={styles.sectionDescription}>
              Mindwire runs alongside your agents, on the machine where your
              code lives. Pick up their conversations, tools, and approvals from
              your iPhone.
            </p>
            <a href="#workspaces" className={styles.textButton}>
              Choose where you work <ArrowRight size={16} />
            </a>
          </div>
          <div className={styles.harnessVisual}>
            <WireDiagram />
          </div>
        </div>
      </section>

      <section
        id="switching"
        className={`${styles.wrap} ${styles.section}`}
        aria-labelledby="switching-title"
      >
        <div className={styles.switchSection}>
          <Corners />
          <div className={styles.switchCopy}>
            <span className={styles.kicker}>Workspace switching</span>
            <h2 id="switching-title" className={styles.sectionTitle}>
              New workspace.
              <br />
              <span>Same train of thought.</span>
            </h2>
            <p>
              Start on your laptop. Continue in the cloud. Bring the work back
              when you’re ready.
            </p>
            <p>
              Your code, local changes, and supported agent history travel
              together. Both project copies stay yours, with conflicts checked
              before changes are applied.
            </p>
            <Link
              href="/docs/guides/project-sync"
              className={styles.textButton}
            >
              See how switching works <ArrowUpRight size={16} />
            </Link>
            <span className={styles.switchSupport}>
              Codex & Claude Code · macOS & Linux
            </span>
          </div>
          <div className={styles.switchVisual}>
            <div className={styles.switchVisualHeader}>
              <span>atlas</span>
              <span>
                <GitBranch size={13} /> feature/dark-mode
              </span>
            </div>
            <div className={styles.switchRoute}>
              <div className={styles.switchMachine}>
                <Laptop size={30} strokeWidth={1.4} />
                <strong>My MacBook</strong>
                <span>Original kept</span>
              </div>
              <div className={styles.switchConnector}>
                <MoveRight size={25} />
                <span>Pick up here</span>
              </div>
              <div
                className={`${styles.switchMachine} ${styles.switchDestination}`}
              >
                <Cloud size={31} strokeWidth={1.4} />
                <strong>Cloud workspace</strong>
                <span>
                  <span className={styles.smallDot} /> Ready to continue
                </span>
              </div>
            </div>
            <div className={styles.switchContents}>
              <div>
                <FileCode2 size={17} />
                <span>Files & local changes</span>
                <Check size={15} />
              </div>
              <div>
                <GitBranch size={17} />
                <span>Commits & staging</span>
                <Check size={15} />
              </div>
              <div>
                <AgentLogo agent="codex" size={17} />
                <span>Conversations & context</span>
                <Check size={15} />
              </div>
            </div>
            <div className={styles.switchVisualFooter}>
              <ShieldCheck size={15} /> Finish active work, then switch safely.
            </div>
          </div>
        </div>
      </section>

      <section
        id="workspaces"
        className={`${styles.wrap} ${styles.section}`}
        aria-labelledby="workspaces-title"
      >
        <div className={styles.sectionHeader}>
          <div>
            <span className={styles.kicker}>You choose where it runs</span>
            <h2 id="workspaces-title" className={styles.sectionTitle}>
              One app.
              <br />
              <span>All your workspaces.</span>
            </h2>
          </div>
          <p>
            Same chats, same tools, same flow. Connect a machine you own or give
            your agent a place in the cloud.
          </p>
        </div>
        <div className={styles.workspaceGrid}>
          <Corners />
          <article className={styles.workspaceCard}>
            <div className={styles.workspaceCardTop}>
              <Laptop size={30} strokeWidth={1.4} />
              <span>Your hardware</span>
            </div>
            <h3>Your computer</h3>
            <p>
              Scan a QR code and continue with the projects already on your Mac,
              Windows PC, or Linux computer.
            </p>
            <ul>
              <li>
                <Check size={15} /> Connect across Wi-Fi and mobile data
              </li>
              <li>
                <Check size={15} /> No Mindwire account required
              </li>
              <li>
                <Check size={15} /> Your agent accounts and tools
              </li>
            </ul>
            <Link href="/get-started#computer" className={styles.textButton}>
              Connect a computer <ArrowRight size={16} />
            </Link>
          </article>
          <article className={styles.workspaceCard}>
            <div className={styles.workspaceCardTop}>
              <Server size={30} strokeWidth={1.4} />
              <span>Your infrastructure</span>
            </div>
            <h3>Your server</h3>
            <p>
              Bring a server over SSH. Work directly on the host or create a
              separate Docker workspace.
            </p>
            <ul>
              <li>
                <Check size={15} /> Native SSH connection
              </li>
              <li>
                <Check size={15} /> Optional container isolation
              </li>
              <li>
                <Check size={15} /> Files, agents, and terminal together
              </li>
            </ul>
            <Link href="/get-started#server" className={styles.textButton}>
              Bring a server <ArrowRight size={16} />
            </Link>
          </article>
          <article className={`${styles.workspaceCard} ${styles.cloudCard}`}>
            <div className={styles.workspaceCardTop}>
              <Cloud size={30} strokeWidth={1.4} />
              <span>Powered by Oblien</span>
            </div>
            <h3>A cloud workspace</h3>
            <p>
              Give your agent its own machine. Keep working with your laptop
              closed and manage it from your phone.
            </p>
            <ul>
              <li>
                <Check size={15} /> Linux and available macOS images
              </li>
              <li>
                <Check size={15} /> Compute, disks, and network controls
              </li>
              <li>
                <Check size={15} /> Usage and lifecycle in the app
              </li>
            </ul>
            <Link href="/pricing" className={styles.textButton}>
              Explore workspace pricing <ArrowRight size={16} />
            </Link>
          </article>
        </div>
        <p className={styles.workspaceNote}>
          <LockKeyhole size={15} /> Computer connections use encrypted SSH and
          device approval. Your machine stays under your control.
        </p>
      </section>

      <section
        id="get-started"
        className={`${styles.wrap} ${styles.section}`}
        aria-labelledby="setup-title"
      >
        <div className={styles.setupSection}>
          <Corners />
          <div>
            <span className={styles.kicker}>From your desk to your pocket</span>
            <h2 id="setup-title" className={styles.sectionTitle}>
              A QR code.
              <br />
              <span>And you’re connected.</span>
            </h2>
            <p className={styles.sectionDescription}>
              Install Mindwire on your computer, scan with the iPhone app, and
              approve your phone. Open a project and pick your agent.
            </p>
            <Link href="/get-started" className={styles.textButton}>
              Get set up <ArrowRight size={16} />
            </Link>
          </div>
          <div>
            <InstallCommand />
            <div className={styles.setupSteps}>
              <span>
                <ScanLine size={18} /> Scan
              </span>
              <ChevronRight size={13} />
              <span>
                <ShieldCheck size={18} /> Approve
              </span>
              <ChevronRight size={13} />
              <span>
                <Code2 size={18} /> Build
              </span>
            </div>
            <p className={styles.setupNote}>
              Mac, Windows, or Linux · Keep your computer awake and online.
            </p>
          </div>
        </div>
      </section>

      <section
        id="open-source"
        className={`${styles.wrap} ${styles.section}`}
        aria-labelledby="source-title"
      >
        <div id="console" className={styles.sourceSection}>
          <Corners />
          <div className={styles.sourceCopy}>
            <span className={styles.kicker}>
              <Code2 size={15} /> Open source · Apache-2.0
            </span>
            <h2 id="source-title" className={styles.sectionTitle}>
              An app for you.
              <br />
              <span>A foundation for everyone.</span>
            </h2>
            <p>
              The runtime, SDKs, and web Console are open source. Inspect the
              code, host it yourself, or build something entirely your own.
            </p>
            <div className={styles.actions}>
              <a href={githubUrl} className={styles.primaryButton}>
                Explore the source <ArrowUpRight size={16} />
              </a>
              <Link href="/docs" className={styles.textButton}>
                Developer docs <ArrowRight size={16} />
              </Link>
            </div>
            <div className={styles.sourceTags}>
              <span>TypeScript</span>
              <span>Go</span>
              <span>HTTP + SSE</span>
              <span>Self-hostable</span>
            </div>
          </div>
          <div className={styles.consolePreview}>
            <a href={consoleUrl} aria-label="Open the Mindwire web Console">
              <div className={styles.consoleCaption}>
                <span>
                  <span /> Mindwire Console
                </span>
                <ArrowUpRight size={16} />
              </div>
              <Image
                src="/console-light.png"
                alt="Mindwire web Console showing workspace activity and agent sessions"
                width={2400}
                height={1050}
                sizes="(max-width: 767px) 90vw, 520px"
                className={`${styles.consoleImage} dark:hidden`}
              />
              <Image
                src="/console-dark.png"
                alt="Mindwire web Console showing workspace activity and agent sessions"
                width={2400}
                height={1050}
                sizes="(max-width: 767px) 90vw, 520px"
                className={`${styles.consoleImage} hidden dark:block`}
              />
            </a>
            <p>
              Building with agents? The same runtime powers the app, the
              Console, and your next project.
            </p>
          </div>
        </div>
      </section>

      <section
        className={`${styles.wrap} ${styles.section} ${styles.faqSection}`}
        aria-labelledby="faq-title"
      >
        <div>
          <span className={styles.kicker}>A few things to know</span>
          <h2 id="faq-title" className={styles.sectionTitle}>
            Before you
            <br />
            <span>head out.</span>
          </h2>
        </div>
        <div className={styles.faqList}>
          {questions.map((item) => (
            <details key={item.question}>
              <summary>
                {item.question}
                <ChevronRight size={18} />
              </summary>
              <p>{item.answer}</p>
            </details>
          ))}
        </div>
      </section>

      <section
        className={`${styles.wrap} ${styles.finalCta}`}
        aria-labelledby="cta-title"
      >
        <div className={styles.ctaPanel}>
          <Corners />
          <span className={styles.kicker}>Make room for your next idea</span>
          <h2 id="cta-title">
            Good work doesn’t
            <br />
            have to stay at your desk.
          </h2>
          <div className={styles.actions}>
            <Link href={downloadUrl} className={styles.primaryButton}>
              <Smartphone size={18} /> Download app <ArrowUpRight size={16} />
            </Link>
            <Link href="/pricing" className={styles.textButton}>
              Find your workspace <ArrowRight size={16} />
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}
