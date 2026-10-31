import Link from "next/link";
import { ArrowUpRight, Check, FileText, PanelTop, Send } from "lucide-react";
import SurfaceRoutes from "./SurfaceRoutes";
import styles from "./product-customization.module.css";

const tools = [
  { icon: FileText, name: "read_brief", description: "Read project files", access: "Read" },
  { icon: PanelTop, name: "create_page", description: "Build and save a page", access: "Write" },
  { icon: Send, name: "publish_page", description: "Take the page live", access: "Approve" },
];

/** One illustrative product, showing the three things its developer owns. */
export default function ProductCustomization() {
  return (
    <SurfaceRoutes
      as="section"
      kind="customizationIntro"
      routeColor="var(--mw-surface)"
      className={styles.section}
      id="features"
      aria-labelledby="features-title"
    >
      <div className={styles.intro}>
        <div className={styles.introTitle} data-sprue="custom-heading">
          <span className={styles.eyebrow}>Make it yours</span>
          <h2 id="features-title">Their brains.<br />Your rules.</h2>
        </div>
        <div className={styles.introCopy}>
          <p>
            White-label leading agents. Give them the tools, context, and
            direction that make your product yours.
          </p>
          <Link href="/sdk">Explore the framework <ArrowUpRight size={18} /></Link>
        </div>
      </div>

      <SurfaceRoutes kind="customization" routeColor="var(--mw-surface)" className={styles.panels}>
        <article className={styles.feature} data-sprue="custom-brand">
          <div className={`${styles.preview} ${styles.product}`} aria-label="Example of an agent inside a product">
            <div className={styles.productBar}><strong>forma</strong><span>Your workspace</span></div>
            <p className={styles.request}>Turn this brief into a launch page.</p>
            <div className={styles.pagePreview}>
              <span>Fieldnotes</span>
              <strong>Room for<br />new ideas.</strong>
              <span className={styles.pageAction}>Join the waitlist <ArrowUpRight size={16} /></span>
            </div>
            <p className={styles.saved}><Check size={16} /> Page and signup form saved</p>
          </div>
          <div className={styles.caption}>
            <h3><Link href="/use-cases/cloud-site-builder">Your brand <ArrowUpRight size={20} aria-hidden /></Link></h3>
            <p>Every request and result in your interface, under your name.</p>
          </div>
        </article>

        <article className={styles.feature} data-sprue="custom-tools">
          <div className={`${styles.preview} ${styles.tools}`} aria-label="Example product tools and access">
            <div className={styles.previewHeading}><span>Product tools</span><span>Access</span></div>
            <ul>
              {tools.map(({ icon: Icon, name, description, access }) => (
                <li key={name}>
                  <Icon size={20} strokeWidth={1.5} aria-hidden />
                  <div><code>{name}</code><p>{description}</p></div>
                  <span>{access}</span>
                </li>
              ))}
            </ul>
            <div className={styles.knowledge}><FileText size={19} strokeWidth={1.5} /><span>brand-guide.md<small>Your product knowledge</small></span></div>
          </div>
          <div className={styles.caption}>
            <h3><Link href="/docs/guides/mcp">Your tools <ArrowUpRight size={20} aria-hidden /></Link></h3>
            <p>Let the agent work with your app’s actions, files, and knowledge.</p>
          </div>
        </article>

        <article className={styles.feature} data-sprue="custom-direction">
          <div className={`${styles.preview} ${styles.instructions}`} aria-label="Example agent instructions">
            <div className={styles.previewHeading}><span>Agent instructions</span><FileText size={18} strokeWidth={1.5} aria-hidden /></div>
            <dl>
              <div><dt>Role</dt><dd>You are our website designer.</dd></div>
              <div><dt>Context</dt><dd>Use the brand guide and project files.</dd></div>
              <div><dt>Goal</dt><dd>Create a working page for review.</dd></div>
            </dl>
            <p className={styles.rule}><Check size={17} /> Ask before publishing</p>
          </div>
          <div className={styles.caption}>
            <h3><Link href="/docs/guides/quickstart">Your direction <ArrowUpRight size={20} aria-hidden /></Link></h3>
            <p>Give it a role and a goal. Decide what needs your user’s approval.</p>
          </div>
        </article>
      </SurfaceRoutes>
    </SurfaceRoutes>
  );
}
