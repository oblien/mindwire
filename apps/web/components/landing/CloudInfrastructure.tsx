import {
  ArrowUpRight,
  Box,
  Boxes,
  Clock3,
  Copy,
  Cpu,
  Database,
  EthernetPort,
  Globe2,
  KeyRound,
  Layers3,
  LockKeyhole,
  Network,
  Play,
  Radio,
  Route,
  Settings2,
  ShieldCheck,
  SquareTerminal,
  Workflow,
  Zap,
} from "lucide-react";
import OblienLogo from "@/components/OblienLogo";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import HeatformArt from "./HeatformArt";
import styles from "./cloud-infrastructure.module.css";

// Oblien platform capabilities, with primary documentation for each feature.
// Mindwire plan prices and resource allowances remain in getComputerPricing().
const features = [
  {
    title: "An entire operating system",
    description:
      "Install packages, run browsers, compile code, and use root access in your Linux VM.",
    icon: SquareTerminal,
    docs: "/workspace",
  },
  {
    title: "Ready in seconds",
    description:
      "Start a fresh environment quickly, with the image and tools your work needs.",
    icon: Zap,
    docs: "/workspace",
  },
  {
    title: "A VM boundary",
    description:
      "Each Linux environment has its own kernel, filesystem, and network, isolated by virtualization.",
    icon: ShieldCheck,
    docs: "/workspace",
  },
  {
    title: "Encrypted storage",
    description:
      "AES-256 encryption protects disks at rest. Secure erasure handles data when resources are released.",
    icon: LockKeyhole,
    docs: "",
  },
  {
    title: "Files that stay with you",
    description:
      "Keep project files and installed tools on persistent disks, across sessions and restarts.",
    icon: Database,
    docs: "/workspace/disks",
  },
  {
    title: "Snapshots & copies",
    description:
      "Capture a workspace, restore a known state, or use a saved environment as a starting point.",
    icon: Copy,
    docs: "/api/snapshots",
  },
  {
    title: "Your choice of image",
    description:
      "Begin with a ready-made environment or prepare a custom image with your own dependencies.",
    icon: Box,
    docs: "/concepts/images",
  },
  {
    title: "CPU, memory, storage",
    description:
      "Choose the resources your workload needs, with explicit capacity limits for each workspace.",
    icon: Cpu,
    docs: "/api/resources",
  },
  {
    title: "Private connections",
    description:
      "Let selected workspaces communicate privately, without exposing their services to the internet.",
    icon: Network,
    docs: "/concepts/networking",
  },
  {
    title: "Network rules you control",
    description:
      "Decide which ports accept connections and where a workspace can send outbound traffic.",
    icon: EthernetPort,
    docs: "/concepts/networking",
  },
  {
    title: "A URL for your work",
    description:
      "Expose an app through an HTTPS preview, or connect your own domain with managed TLS.",
    icon: Globe2,
    docs: "/workspace/domains",
  },
  {
    title: "Traffic routing",
    description:
      "Route incoming requests to workspace services and manage where your application traffic goes.",
    icon: Route,
    docs: "/api/routes",
  },
  {
    title: "SSH & live terminals",
    description:
      "Open a shell, stream command output, and work with the tools you already know.",
    icon: KeyRound,
    docs: "/concepts/networking",
  },
  {
    title: "Long-running processes",
    description:
      "Keep servers and workers under managed process supervision, with lifecycle controls and logs.",
    icon: Workflow,
    docs: "/api/workloads",
  },
  {
    title: "Pause. Pick up again.",
    description:
      "Suspend a workspace and resume its work later, or let supported environments wake on a request.",
    icon: Play,
    docs: "/workspace/on-demand",
  },
  {
    title: "Temporary by design",
    description:
      "Give temporary environments an expiration time, so short-lived work has a defined end.",
    icon: Clock3,
    docs: "/concepts/modes",
  },
  {
    title: "See what’s happening",
    description:
      "Inspect CPU, memory, disk, and network activity with live workspace metrics.",
    icon: Radio,
    docs: "/api/metrics",
  },
  {
    title: "Start with your setup",
    description:
      "Supply environment variables and startup commands to prepare the workspace for its first task.",
    icon: Settings2,
    docs: "/api/workspaces",
  },
  {
    title: "Build it into your product",
    description:
      "Manage infrastructure through Oblien’s SDK and REST API, with scoped access to workspace resources.",
    icon: Layers3,
    docs: "/api",
  },
  {
    title: "One workspace to many",
    description:
      "Separate customers, teams, and environments with namespaces, resource limits, and independent lifecycles.",
    icon: Boxes,
    docs: "/api/namespaces",
  },
];

export default function CloudInfrastructure() {
  return (
    <section
      className={styles.infrastructure}
      id="infrastructure"
      aria-labelledby="infrastructure-title"
    >
      <WindowShape kind="diagram" color="#0a0a0a" />
      <div className={styles.intro}>
        <a href="https://oblien.com/" className={styles.provider}>
          <OblienLogo size={30} />
          <span>Infrastructure by Oblien</span>
          <ArrowUpRight size={18} />
        </a>
        <h2 id="infrastructure-title">
          A COMPUTER.
          <br />
          FOR EVERY AGENT.
        </h2>
        <p>
          Your product on the surface. A whole computer underneath. Give each
          customer an isolated place to build, run software, and keep working.
          Start with one. Add more as you grow.
        </p>
        <a href="/docs/guides/cloud-computers" className={styles.textLink}>
          How Cloud Computers work <ArrowUpRight size={18} />
        </a>
      </div>
      <SurfaceRoutes kind="cloudMachines" className={styles.machines}>
        <div className={styles.sourceColumn}>
          <div className={styles.product} data-sprue="cloud-product">
            <img
              src="/images/framework/api-icon.svg"
              alt=""
              width={34}
              height={34}
              loading="lazy"
            />
            <div>
              <strong>Your product</strong>
              <span>Your customers. Your projects.</span>
            </div>
          </div>
          <div className={styles.sourceArt} data-sprue="cloud-art">
            <HeatformArt variant="cobalt" />
          </div>
        </div>
        <div className={styles.machineBase} data-sprue="cloud-provider">
          <WindowShape kind="diagram" color="#d9d9d9" />
          <img
            src="/images/framework/hub-ticks.svg"
            alt=""
            width={83}
            height={7}
            loading="lazy"
          />
          <OblienLogo size={64} />
          <strong>Oblien</strong>
          <span>Hardware-isolated VMs</span>
        </div>
        <div className={styles.machineGrid}>
          {["Customer A", "Customer B", "Customer C"].map((customer, i) => (
            <div
              className={styles.machine}
              data-sprue={`cloud-machine-${i}`}
              key={customer}
            >
              <div className={styles.machineHeading}>
                <h3>{customer}</h3>
                <span>VM / 0{i + 1}</span>
              </div>
              <ul>
                <li>Own kernel</li>
                <li>Private files</li>
                <li>Own network</li>
              </ul>
            </div>
          ))}
        </div>
      </SurfaceRoutes>
      <p className={styles.diagramCaption}>
        Separate Linux workspaces. Each computer has its own subscription and
        resource allowance.
      </p>
      <div className={styles.featureHeading}>
        <h2>
          More than somewhere
          <br />
          to run your agent.
        </h2>
        <div>
          <p>
            Compute, storage, networking, and the controls around them. The
            Oblien infrastructure behind Mindwire Cloud.
          </p>
          <a
            href="https://oblien.com/docs/workspace"
            className={styles.textLink}
          >
            Explore the platform <ArrowUpRight size={18} />
          </a>
        </div>
      </div>
      <div className={styles.featureGrid}>
        {features.map(({ title, description, icon: Icon, docs }) => (
          <a
            className={styles.feature}
            key={title}
            href={`https://oblien.com${docs ? `/docs${docs}` : "/"}`}
          >
            <div className={styles.featureIcon}>
              <Icon size={28} strokeWidth={1.5} aria-hidden="true" />
              <ArrowUpRight size={18} aria-hidden="true" />
            </div>
            <h3>{title}</h3>
            <p>{description}</p>
          </a>
        ))}
      </div>
      <p className={styles.featureNote}>
        Platform capabilities vary by image and plan. Your Cloud subscription
        covers the resources listed above; additional storage, backups, and
        transfer follow checkout terms. Pausing a computer does not pause its
        monthly subscription.
      </p>
    </section>
  );
}
