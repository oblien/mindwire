"use client";

import { useState } from "react";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import styles from "./cloud-capabilities.module.css";

const capabilities = [
  {
    id: "compute",
    name: "A whole computer",
    description: "Your stack, ready to work.",
    title: "Give the agent room to build.",
    body: "A Linux environment with the tools your product needs, behind a hardware isolation boundary.",
    features: [
      { title: "Full Linux + root access", docs: "/workspace" },
      { title: "A separate kernel for every VM", docs: "/workspace" },
      { title: "Ready-made or custom images", docs: "/concepts/images" },
      { title: "Fast startup with your environment", docs: "/api/workspaces" },
    ],
  },
  {
    id: "storage",
    name: "Work that stays",
    description: "Keep it. Copy it. Come back to it.",
    title: "The conversation ends. The project stays.",
    body: "Keep the files, dependencies, and working state that make the next session useful.",
    features: [
      { title: "Persistent project disks", docs: "/workspace/disks" },
      { title: "Snapshots, restore & copies", docs: "/api/snapshots" },
      { title: "Encrypted disks & secure erasure", docs: "" },
      { title: "Defined CPU, memory & disk capacity", docs: "/api/resources" },
    ],
  },
  {
    id: "network",
    name: "A way out to the world",
    description: "Private services. Public previews.",
    title: "From a running app to a URL.",
    body: "Keep internal services private. Give the work you want to share a route of its own.",
    features: [
      { title: "Private workspace connections", docs: "/concepts/networking" },
      {
        title: "Inbound & outbound network rules",
        docs: "/concepts/networking",
      },
      { title: "HTTPS previews & custom domains", docs: "/workspace/domains" },
      { title: "Routing to your app’s services", docs: "/api/routes" },
    ],
  },
  {
    id: "lifecycle",
    name: "Its own rhythm",
    description: "Run, pause, resume, repeat.",
    title: "A quick task or a long-running project.",
    body: "Manage the environment around the work, from a live shell to a service that keeps running.",
    features: [
      { title: "SSH & live terminal access", docs: "/concepts/networking" },
      { title: "Managed processes & logs", docs: "/api/workloads" },
      {
        title: "Pause, resume & supported auto-wake",
        docs: "/workspace/on-demand",
      },
      { title: "Expiry for temporary environments", docs: "/concepts/modes" },
    ],
  },
  {
    id: "scale",
    name: "One customer to many",
    description: "Separate workspaces. Shared control.",
    title: "Make it part of your platform.",
    body: "Create and manage workspaces from your application, with a clear view of each customer’s resources.",
    features: [
      { title: "CPU, memory, disk & network metrics", docs: "/api/metrics" },
      { title: "SDK & REST infrastructure control", docs: "/api" },
      {
        title: "Scoped access to workspace resources",
        docs: "/api/workspaces",
      },
      { title: "Namespaces & independent lifecycles", docs: "/api/namespaces" },
    ],
  },
];

export default function CloudCapabilities() {
  const [active, setActive] = useState(0);
  const current = capabilities[active];
  return (
    <section
      className={styles.capabilities}
      aria-labelledby="cloud-capabilities-title"
    >
      <div className={styles.heading}>
        <h2 id="cloud-capabilities-title">
          Build it. Run it.
          <br />
          Keep it working.
        </h2>
        <p>
          Provision workspaces, connect services, and manage their lifecycle.
          Explore what the Oblien platform gives your business.
        </p>
      </div>
      <SurfaceRoutes
        kind="cloudCapabilities"
        routeColor="var(--mw-diagram-surface)"
        className={styles.layout}
      >
        <div
          className={styles.selector}
          data-sprue="capability-menu"
          role="group"
          aria-label="Explore cloud capabilities"
        >
          {capabilities.map((item, index) => (
            <button
              type="button"
              key={item.id}
              aria-pressed={active === index}
              aria-controls="cloud-capability-detail"
              onClick={() => setActive(index)}
            >
              <span>
                <strong>{item.name}</strong>
                <small>{item.description}</small>
              </span>
              <span aria-hidden="true">↗</span>
            </button>
          ))}
        </div>
        <div
          className={styles.detail}
          data-sprue="capability-detail"
          id="cloud-capability-detail"
        >
          <WindowShape kind="diagram" color="var(--mw-diagram-surface)" />
          <div className={styles.detailHeading}>
            <span>Inside an Oblien workspace</span>
            <span>0{active + 1} / 05</span>
          </div>
          <div className={styles.visual} key={current.id}>
            {current.id === "compute" && (
              <div className={styles.compute}>
                <div>
                  <span>workspace / customer-01</span>
                  <strong>
                    A machine
                    <br />
                    of its own.
                  </strong>
                  <p>
                    Own kernel.
                    <br />
                    Your tools and dependencies.
                  </p>
                </div>
                <div className={styles.terminal}>
                  <span>Linux / shell</span>
                  <pre>
                    <code>
                      {
                        "$ node --version\nv22.x\n\n$ python --version\nPython 3.x\n\n$ npm run build\n✓ Ready to preview"
                      }
                    </code>
                  </pre>
                </div>
              </div>
            )}
            {current.id === "storage" && (
              <div className={styles.storage}>
                <div className={styles.folder}>
                  <span>project /</span>
                  <div>
                    <span>src</span>
                    <span>Application files</span>
                  </div>
                  <div>
                    <span>assets</span>
                    <span>Your images & data</span>
                  </div>
                  <div>
                    <span>reports</span>
                    <span>The work so far</span>
                  </div>
                  <footer>Persistent disk</footer>
                </div>
                <div className={styles.snapshot}>
                  <span>Saved state</span>
                  <strong>
                    Pick up
                    <br />
                    where you left off.
                  </strong>
                  <div>Snapshot → New workspace</div>
                </div>
              </div>
            )}
            {current.id === "network" && (
              <div className={styles.network}>
                <div className={styles.address}>
                  <span>HTTPS / Your preview</span>
                  <strong>preview.yourproduct.com</strong>
                </div>
                <div className={styles.networkRoute} aria-hidden="true">
                  <span />
                </div>
                <div className={styles.services}>
                  <div>
                    <span>Web app</span>
                    <strong>:3000</strong>
                    <small>Public route</small>
                  </div>
                  <div>
                    <span>Database</span>
                    <strong>:5432</strong>
                    <small>Private network</small>
                  </div>
                </div>
              </div>
            )}
            {current.id === "lifecycle" && (
              <div className={styles.lifecycle}>
                <div className={styles.process}>
                  <span>web-server</span>
                  <strong>Running</strong>
                  <p>
                    Managed process
                    <br />
                    Logs available
                  </p>
                  <div className={styles.log}>
                    GET /preview <span>200</span>
                    <br />
                    GET /assets/site.css <span>200</span>
                  </div>
                </div>
                <div className={styles.timeline}>
                  <span>When the work needs it</span>
                  <ol>
                    <li>Start the workspace</li>
                    <li>Run your tools</li>
                    <li>Pause between sessions</li>
                    <li>Resume the project</li>
                  </ol>
                </div>
              </div>
            )}
            {current.id === "scale" && (
              <div className={styles.scale}>
                <div className={styles.scaleHeader}>
                  <strong>Your application</strong>
                  <span>Workspace overview</span>
                </div>
                {["Customer A", "Customer B", "Customer C"].map(
                  (name, index) => (
                    <div className={styles.resourceRow} key={name}>
                      <span>{name}</span>
                      <div>
                        <span style={{ width: `${[36, 62, 24][index]}%` }} />
                      </div>
                      <span>{[36, 62, 24][index]}% CPU</span>
                    </div>
                  ),
                )}
                <p>Independent resources, access, and lifecycles.</p>
              </div>
            )}
          </div>
          <div className={styles.copy} aria-live="polite">
            <h3>{current.title}</h3>
            <p>{current.body}</p>
          </div>
          <div className={styles.features}>
            {current.features.map((feature) => (
              <a
                key={feature.title}
                href={`https://oblien.com${feature.docs ? `/docs${feature.docs}` : "/"}`}
              >
                {feature.title}
                <span aria-hidden="true">↗</span>
              </a>
            ))}
          </div>
          <p className={styles.illustrationNote}>
            Workspace illustration · Explore the linked platform documentation.
          </p>
        </div>
      </SurfaceRoutes>
    </section>
  );
}
