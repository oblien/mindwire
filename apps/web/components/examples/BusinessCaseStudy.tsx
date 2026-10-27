import Link from "next/link";
import { type UseCase, useCases } from "@/lib/use-cases";
import WindowShape from "@/components/landing/WindowShape";
import UseCaseCard from "@/components/landing/UseCaseCard";
import BusinessProductDemo from "./BusinessProductDemo";
import ConnectionPatterns from "./ConnectionPatterns";
import base from "@/components/landing/landing.module.css";
import styles from "./business-case-study.module.css";

const localConnection = `// Inside your desktop app’s Node/Bun process.
// These paths and environment belong to your app.
const mw = new Mindwire({
  agent: "codex",
  target: local({
    cwd: projectDirectory,
    statePath: privateStateFile,
    environment: privateEnvironment,
    authSource: "host",
    shared: false,
  }),
});

// Discover and verify the native connection.
const status = await mw.auth.status();
// If unconfigured, offer mw.auth.methods().`;

const cloudConnection = `// mw targets this user’s authenticated runtime.
// Render the methods this agent actually offers.
const methods = await mw.auth.methods();
const login = methods.find((m) => m.id === "login");

if (login) {
  const flow = await mw.auth.begin(login.id);
  // Open flow.url in the user’s browser.
  // Handle callback and poll flow.flowId to finish.
}

// Once connected, start work in your application.
const run = await mw.turn({
  chatId: projectChatId,
  message: userRequest,
});
// Stream the run into your own interface.`;

export default function BusinessCaseStudy({ item }: { item: UseCase }) {
  const related = useCases
    .filter(
      (other) => other.slug !== item.slug && other.platform !== item.platform,
    )
    .slice(0, 2);
  return (
    <div className={base.landing}>
      <div className={styles.page}>
        <Link href="/use-cases#use-case-library" className={styles.back}>
          ← All product examples
        </Link>
        <section className={styles.hero} aria-labelledby="example-title">
          <WindowShape kind="hero" />
          <div className={styles.heroMeta}>
            <span>
              {item.category} / {item.platform} integration
            </span>
            <span>Product blueprint</span>
          </div>
          <h1 id="example-title">{item.headline}</h1>
          <div className={styles.heroBottom}>
            <p>{item.opportunity}</p>
            <a
              href={
                item.kind === "learning" ? "#course-results" : "#product-demo"
              }
            >
              Try the product <span aria-hidden="true">↓</span>
            </a>
          </div>
        </section>
        <section
          className={styles.experience}
          aria-label={`${item.brand} product experience`}
        >
          <BusinessProductDemo key={item.slug} item={item} />
        </section>
      </div>
      <ConnectionPatterns
        key={item.slug}
        platform={item.platform}
        brand={item.brand}
      />
      <div className={styles.page}>
        <section
          className={styles.integration}
          aria-labelledby="integration-title"
        >
          <div className={styles.integrationIntro}>
            <span>Build this into your product</span>
            <h2 id="integration-title">
              You bring the tools.
              <br />
              Mindwire connects the agent.
            </h2>
            <p>{item.integration}</p>
            <Link href="/docs/guides/quickstart">
              Read the integration guide <span aria-hidden="true">↗</span>
            </Link>
          </div>
          <div className={styles.toolList}>
            <h3>What {item.brand} gives the agent</h3>
            <p>Example application tools you implement and authorize.</p>
            <dl>
              {item.tools.map((tool) => (
                <div key={tool.name}>
                  <dt>{tool.name}</dt>
                  <dd>{tool.purpose}</dd>
                </div>
              ))}
            </dl>
            <Link href="/docs/guides/mcp">
              Connect your application’s tools <span aria-hidden="true">↗</span>
            </Link>
          </div>
        </section>
        <section className={styles.implementation}>
          <div>
            <span>The connection, in code</span>
            <h2>
              {item.platform === "Desktop" ? (
                <>
                  Use the account
                  <br />
                  already on the computer.
                </>
              ) : (
                <>
                  Connect the account.
                  <br />
                  Keep the experience yours.
                </>
              )}
            </h2>
            <p>
              {item.platform === "Desktop"
                ? "A private app environment can reuse a supported native account without importing the user’s personal tools, hooks, or conversation history. Close the runtime when the app releases the workspace."
                : "Start the provider’s native login in the customer’s workspace. Return its sign-in URL, handle completion, and keep credentials in that workspace. An eligible ChatGPT connection uses the customer’s Codex access; an API key follows provider API billing."}
            </p>
            <p>
              These are integration excerpts. Your app owns workspace setup,
              tool permissions, connection errors, and the result UI.
            </p>
            <Link href="/docs/guides/authentication">
              Full authentication flow <span aria-hidden="true">↗</span>
            </Link>
          </div>
          <div className={styles.code}>
            <div>
              <span>
                {item.platform === "Desktop"
                  ? "Native account reuse"
                  : "User-owned cloud connection"}
              </span>
              <span>TypeScript</span>
            </div>
            <pre>
              <code>
                {item.platform === "Desktop"
                  ? localConnection
                  : cloudConnection}
              </code>
            </pre>
          </div>
        </section>
        <section className={styles.outcome}>
          <span>The result</span>
          <h2>{item.outcome}</h2>
          <p>
            Your app starts the work, gives the agent its context and tools, and
            turns the response into a useful product experience. Customers stay
            in {item.brand} from the first request to the finished work.
          </p>
          <Link href="/sdk">
            Make your product AI-powered <span aria-hidden="true">↗</span>
          </Link>
        </section>
        <section className={styles.related} aria-labelledby="related-title">
          <h2 id="related-title">Another way to build with it.</h2>
          <div>
            {related.map((example) => (
              <UseCaseCard item={example} key={example.slug} />
            ))}
          </div>
        </section>
        <p className={styles.note}>
          {item.brand} is an illustrative product, not a customer endorsement.
          The interactive preview uses sample data and runs in your browser.
        </p>
      </div>
    </div>
  );
}
