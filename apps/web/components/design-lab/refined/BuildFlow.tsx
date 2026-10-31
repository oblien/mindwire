"use client";

import { useId, useLayoutEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, FileCode2 } from "lucide-react";
import { githubUrl } from "@/lib/product";
import CopyButton from "@/components/landing/CopyButton";
import { navigateTabs } from "@/components/landing/tabs";
import { fluidJoin, type Anchor, type Edge } from "./fluid-join";
import AnimatedLinework from "./AnimatedLinework";
import BuildConnectorArt from "./BuildConnectorArt";
import { buildExamples } from "./build-examples";
import styles from "./build-flow.module.css";

function CodeLines({ code }: { code: string }) {
  return (
    <pre
      className={styles.code}
      tabIndex={0}
      role="region"
      aria-label="Integration code example"
    >
      <code>
        {code.split("\n").map((line, index) => (
          <span className={styles.codeLine} key={index}>
            <span className={styles.lineNumber} aria-hidden="true">
              {index + 1}
            </span>
            <span>
              {line
                .split(
                  /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\/\/.*$|#.*$|\b(?:import|from|const|new|await|for|of|defer|if|return|range|nil)\b)/g,
                )
                .map((token, tokenIndex) => (
                  <span
                    key={tokenIndex}
                    data-token={
                      /^(\/\/|#)/.test(token)
                        ? "comment"
                        : /^["']/.test(token)
                          ? "string"
                          : /^(import|from|const|new|await|for|of|defer|if|return|range|nil)$/.test(
                                token,
                              )
                            ? "keyword"
                            : undefined
                    }
                  >
                    {token}
                  </span>
                ))}
              {"\n"}
            </span>
          </span>
        ))}
      </code>
    </pre>
  );
}

export default function BuildFlow() {
  const board = useRef<HTMLDivElement>(null);
  const [joins, setJoins] = useState<string[]>([]);
  const [selected, setSelected] = useState(0);
  const id = useId();
  const current = buildExamples[selected];

  useLayoutEffect(() => {
    const element = board.current;
    if (!element) return;
    let frame = 0;
    const measure = () => {
      const bounds = element.getBoundingClientRect();
      const compact = window.innerWidth < 960;
      const anchor = (id: string, edge: Edge, fraction = 0.5): Anchor => {
        const node = element.querySelector<HTMLElement>(
          '[data-flow="' + id + '"]',
        )!;
        const rect = node.getBoundingClientRect();
        const directions = {
          top: [0, -1],
          right: [1, 0],
          bottom: [0, 1],
          left: [-1, 0],
        };
        const [dx, dy] = directions[edge];
        return {
          x:
            rect.left -
            bounds.left +
            (edge === "left"
              ? 1
              : edge === "right"
                ? rect.width - 1
                : rect.width * fraction),
          y:
            rect.top -
            bounds.top +
            (edge === "top"
              ? 1
              : edge === "bottom"
                ? rect.height - 1
                : rect.height * fraction),
          dx,
          dy,
        };
      };
      if (compact) {
        const entrance = anchor("intro", "top", 0.5);
        setJoins([
          fluidJoin({ x: entrance.x + 50, y: -2, dx: 0, dy: 1 }, entrance),
          fluidJoin(
            anchor("intro", "bottom", 0.55),
            anchor("connector", "top"),
          ),
          fluidJoin(anchor("connector", "bottom"), anchor("sdk", "top", 0.5)),
        ]);
      } else {
        const entrance = anchor("sdk", "top", 0.82);
        setJoins([
          fluidJoin({ x: entrance.x, y: -2, dx: 0, dy: 1 }, entrance),
          fluidJoin(
            anchor("intro", "bottom", 0.82),
            anchor("sdk", "top", 0.25),
            28,
          ),
          fluidJoin(
            anchor("intro", "bottom", 0.25),
            anchor("connector", "top", 0.5),
            26,
          ),
          fluidJoin(
            anchor("connector", "right", 0.6),
            anchor("sdk", "left", 0.55),
            22,
          ),
        ]);
      }
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    const observer = new ResizeObserver(schedule);
    observer.observe(element);
    element
      .querySelectorAll<HTMLElement>("[data-flow]")
      .forEach((node) => observer.observe(node));
    window.addEventListener("resize", schedule);
    measure();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", schedule);
    };
  }, []);

  return (
    <section
      className={styles.section}
      id="connect"
      aria-labelledby="connect-title"
    >
      <div className={styles.stage} ref={board}>
        <svg
          className={styles.joins}
          aria-hidden="true"
          width="100%"
          height="100%"
        >
          {joins.map((path, index) => (
            <path key={index} d={path} />
          ))}
        </svg>

        <div className={styles.intro} data-flow="intro">
          <div className={styles.introArt}>
            <BuildConnectorArt />
          </div>
          <h2 id="connect-title">Build with Mindwire.</h2>
          <p>
            Bring capable agents into your product.
            <br /> Your tools, your context, your interface.
          </p>
          <div className={styles.actions}>
            <Link href="/docs/guides/quickstart" className={styles.start}>
              Read the quickstart <ArrowUpRight size={16} />
            </Link>
            <Link href="/sdk" className={styles.secondary}>
              Explore the SDK <ArrowUpRight size={16} />
            </Link>
          </div>
        </div>

        <div
          className={styles.connector}
          data-flow="connector"
          aria-hidden="true"
        >
          <AnimatedLinework variant="handoff" />
        </div>

        <div className={styles.sdk} data-flow="sdk">
          <div
            className={styles.tabs}
            role="tablist"
            aria-label="Choose your integration"
            onKeyDown={navigateTabs}
          >
            {buildExamples.map((example, index) => (
              <button
                key={example.id}
                type="button"
                role="tab"
                id={id + "-tab-" + example.id}
                aria-controls={id + "-panel"}
                aria-selected={selected === index}
                tabIndex={selected === index ? 0 : -1}
                onClick={() => setSelected(index)}
              >
                <span
                  className={styles.languageMark}
                  aria-hidden="true"
                  data-language={example.id}
                >
                  {example.id === "go" ? <FileCode2 size={16} /> : example.mark}
                </span>
                {example.name}
              </button>
            ))}
            <span className={styles.tabNote}>One framework.</span>
          </div>

          <div
            className={styles.instructions}
            role="tabpanel"
            id={id + "-panel"}
            aria-labelledby={id + "-tab-" + current.id}
            tabIndex={0}
          >
            <div className={styles.setup}>
              <div className={styles.stepHeading}>
                <span className={styles.stepNumber} aria-hidden="true">
                  01
                </span>
                <h3>{current.setup}</h3>
              </div>
              <p className={styles.setupCopy}>{current.summary}</p>
              <div className={styles.install}>
                <span className={styles.prompt} aria-hidden="true">
                  $
                </span>
                <code tabIndex={0}>{current.command}</code>
                <CopyButton
                  value={current.command}
                  label="Copy install command"
                />
              </div>
              <Link
                className={styles.setupGuide}
                href="/docs/guides/authentication"
              >
                Connect your agent <ArrowUpRight size={14} />
              </Link>
            </div>

            <div className={styles.example}>
              <div className={styles.stepHeading}>
                <span className={styles.stepNumber} aria-hidden="true">
                  02
                </span>
                <h3>{current.caption}</h3>
              </div>
              <div className={styles.editor}>
                <div className={styles.editorHeader}>
                  <span>
                    <FileCode2 size={14} />
                    {current.filename}
                  </span>
                  <CopyButton value={current.code} label="Copy example code" />
                </div>
                <CodeLines code={current.code} />
              </div>
            </div>
            <div className={styles.codeFooter}>
              <p>{current.note}</p>
              <Link href="/docs/guides/quickstart">
                Full guide <ArrowUpRight size={15} />
              </Link>
            </div>
          </div>
        </div>
      </div>

      <div className={styles.links}>
        <Link href="/docs/concepts/architecture">
          <span>Understand the architecture</span>
          <ArrowUpRight size={18} />
        </Link>
        <Link href="/docs/reference/agents/codex">
          <span>Connect your agent</span>
          <ArrowUpRight size={18} />
        </Link>
        <a href={githubUrl}>
          <span>Build on the source</span>
          <ArrowUpRight size={18} />
        </a>
      </div>
    </section>
  );
}
